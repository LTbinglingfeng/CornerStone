package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// QQ's GROUP_AND_C2C_EVENT shares bit 25. Dispatch is nevertheless strictly C2C-only.
const qqOfficialIntent = 1 << 25
const qqOfficialQueueLimit = 64

type qqOfficialFrame struct {
	Op int             `json:"op"`
	S  *int64          `json:"s,omitempty"`
	T  string          `json:"t,omitempty"`
	D  json.RawMessage `json:"d"`
}
type qqOfficialSession struct {
	id             string
	seq            *int64
	botID, botName string
}
type qqOfficialJob struct {
	ctx   context.Context
	frame qqOfficialFrame
	done  chan qqOfficialAccepted
}
type qqOfficialAccepted struct {
	seq *int64
	err error
}

type qqOfficialCloseError struct{ code int }

func (e *qqOfficialCloseError) Error() string { return "QQ gateway closed" }

// Policy follows the official WebSocket error-code table:
// https://bot.q.qq.com/wiki/develop/api-v2/dev-prepare/error-trace/websocket.html
func (e *qqOfficialCloseError) canReconnect() bool {
	switch e.code {
	case 4001, 4002, 4010, 4011, 4012, 4013, 4014, 4914, 4915:
		return false
	}
	return true
}

func (e *qqOfficialCloseError) requiresIdentify() bool {
	return e.code == 4006 || e.code == 4007 || (e.code >= 4900 && e.code <= 4913)
}

func qqOfficialGatewayAllowed(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "wss" || u.User != nil || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	h := strings.ToLower(u.Hostname())
	return h == "api.bot.qq.com" || h == "api.sgroup.qq.com" || h == "api.sgroup.qq.com.cn"
}

// Run serializes concurrent calls on this client; a waiting call can be cancelled.
// Callbacks should honor their context. onMessage success means durable acceptance:
// its dispatch sequence is acknowledged only after it returns nil. Replays are possible.
// onStatus is called serially by Run and must return promptly.
func (c *QQOfficialClient) Run(ctx context.Context, onMessage func(context.Context, QQOfficialMessage) error, onStatus func(QQOfficialStatus)) {
	select {
	case c.runGate <- struct{}{}:
	case <-ctx.Done():
		return
	}
	defer func() { <-c.runGate }()
	if ctx.Err() != nil {
		return
	}
	jobs := make(chan qqOfficialJob, qqOfficialQueueLimit)
	workerCtx, stopWorker := context.WithCancel(ctx)
	defer stopWorker()
	go func() {
		for {
			select {
			case <-workerCtx.Done():
				return
			case j := <-jobs:
				if j.ctx.Err() != nil {
					continue
				}
				var err error
				if j.frame.T == "C2C_MESSAGE_CREATE" {
					var m QQOfficialMessage
					m, err = qqOfficialDecodeMessage(j.frame.D)
					if err == nil {
						if onMessage == nil {
							err = errors.New("QQ missing message handler")
						} else {
							err = onMessage(j.ctx, m)
						}
					}
				}
				// Never expose callback errors (which may include secrets) through status.
				if err != nil {
					err = errors.New("QQ message was not accepted")
				}
				select {
				case j.done <- qqOfficialAccepted{j.frame.S, err}:
				case <-j.ctx.Done():
				case <-workerCtx.Done():
					return
				}
			}
		}
	}()
	session := qqOfficialSession{}
	emit := func(status string, err error) {
		if onStatus != nil {
			s := QQOfficialStatus{Status: status, BotID: session.botID, BotName: session.botName}
			if err != nil {
				s.LastError = err.Error()
			}
			onStatus(s)
		}
	}
	defer emit("disconnected", nil)
	delay := time.Second
	first := true
	for ctx.Err() == nil {
		if first {
			emit("connecting", nil)
			first = false
		} else {
			emit("reconnecting", nil)
		}
		started := time.Now()
		err := c.connection(ctx, &session, jobs, func() { emit("connected", nil) })
		if ctx.Err() != nil {
			return
		}
		emit("error", err)
		var closed *qqOfficialCloseError
		if errors.As(err, &closed) && !closed.canReconnect() {
			return
		}
		if time.Since(started) > time.Minute {
			delay = time.Second
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if delay < 30*time.Second {
			delay *= 2
			if delay > 30*time.Second {
				delay = 30 * time.Second
			}
		}
	}
}

func qqOfficialDecodeMessage(raw json.RawMessage) (QQOfficialMessage, error) {
	var d struct {
		ID        string    `json:"id"`
		Content   string    `json:"content"`
		Timestamp time.Time `json:"timestamp"`
		Author    struct {
			OpenID string `json:"user_openid"`
		} `json:"author"`
		Attachments []struct {
			URL         string `json:"url"`
			ContentType string `json:"content_type"`
			Filename    string `json:"filename"`
		} `json:"attachments"`
	}
	if json.Unmarshal(raw, &d) != nil || d.ID == "" || d.Author.OpenID == "" {
		return QQOfficialMessage{}, errors.New("QQ malformed C2C event")
	}
	m := QQOfficialMessage{ID: d.ID, UserOpenID: d.Author.OpenID, Content: d.Content, Timestamp: d.Timestamp}
	for _, a := range d.Attachments {
		m.Attachments = append(m.Attachments, QQOfficialAttachment{a.URL, a.ContentType, a.Filename})
	}
	return m, nil
}

func (c *QQOfficialClient) connection(parent context.Context, s *qqOfficialSession, jobs chan<- qqOfficialJob, connected func()) (result error) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	token, err := c.accessToken(ctx)
	if err != nil {
		return err
	}
	var gateway struct {
		URL string `json:"url"`
	}
	if err = qqOfficialJSON(ctx, c.httpClient, http.MethodGet, c.apiURL+"/gateway", token, nil, &gateway); err != nil {
		c.rejectUnauthorizedToken(token, err)
		return err
	}
	if !c.gatewayAllowed(gateway.URL) {
		return errors.New("QQ untrusted gateway")
	}
	dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second}
	conn, resp, err := dialer.DialContext(ctx, gateway.URL, nil)
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		return errors.New("QQ gateway connection failed")
	}
	defer conn.Close()
	conn.SetReadLimit(1 << 20)
	go func() { <-ctx.Done(); conn.Close() }()
	frames := make(chan qqOfficialFrame, qqOfficialQueueLimit)
	readErr := make(chan error, 1)
	go func() {
		defer conn.Close()
		for {
			var f qqOfficialFrame
			if err := conn.ReadJSON(&f); err != nil {
				var safeErr error = errors.New("QQ gateway read failed")
				var closed *websocket.CloseError
				if errors.As(err, &closed) {
					safeErr = &qqOfficialCloseError{code: closed.Code}
				}
				select {
				case readErr <- safeErr:
				default:
				}
				return
			}
			select {
			case frames <- f:
			case <-ctx.Done():
				return
			default:
				select {
				case readErr <- errors.New("QQ gateway receive queue full"):
				default:
				}
				return
			}
		}
	}()
	write := func(op int, d any) error {
		conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if conn.WriteJSON(map[string]any{"op": op, "d": d}) != nil {
			return errors.New("QQ gateway write failed")
		}
		return nil
	}
	handshake := time.NewTimer(20 * time.Second)
	defer handshake.Stop()
	var ticker *time.Ticker
	defer func() {
		if ticker != nil {
			ticker.Stop()
		}
	}()
	var ticks <-chan time.Time
	hello, ready, awaitingAck := false, false, false
	resuming := false
	accepted := make(chan qqOfficialAccepted, qqOfficialQueueLimit)
	var terminalReadError error
	defer func() {
		var closed *qqOfficialCloseError
		if errors.As(terminalReadError, &closed) {
			// Apply after draining buffered frames: a queued READY must not
			// restore a session that the close code has invalidated.
			if closed.requiresIdentify() {
				s.id = ""
				s.seq = nil
			}
			if closed.code == 4004 {
				c.rejectToken(token)
			}
			result = closed
		}
	}()
	for {
		var f qqOfficialFrame
		if terminalReadError != nil {
			// Drain already received control frames before EOF; in particular an
			// INVALID_SESSION immediately followed by close must not be lost.
			select {
			case f = <-frames:
			default:
				return terminalReadError
			}
		} else {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case err := <-readErr:
				terminalReadError = err
				continue
			case <-handshake.C:
				return errors.New("QQ gateway handshake timeout")
			case <-ticks:
				if awaitingAck {
					return errors.New("QQ heartbeat acknowledgement timeout")
				}
				if err := write(1, s.seq); err != nil {
					return err
				}
				awaitingAck = true
				continue
			case a := <-accepted:
				if a.err != nil {
					return a.err
				}
				if a.seq != nil {
					s.seq = a.seq
				}
				continue
			case f = <-frames:
			}
		}
		switch f.Op {
		case 10:
			if hello {
				return errors.New("QQ duplicate hello")
			}
			hello = true
			var h struct {
				Interval int64 `json:"heartbeat_interval"`
			}
			if json.Unmarshal(f.D, &h) != nil || h.Interval < 10 || h.Interval > 300000 {
				return errors.New("QQ invalid heartbeat interval")
			}
			ticker = time.NewTicker(time.Duration(h.Interval) * time.Millisecond)
			ticks = ticker.C
			if s.id != "" && s.seq != nil {
				resuming = true
				err = write(6, map[string]any{"token": "QQBot " + token, "session_id": s.id, "seq": *s.seq})
			} else {
				err = write(2, map[string]any{"token": "QQBot " + token, "intents": qqOfficialIntent, "shard": []int{0, 1}, "properties": map[string]string{}})
			}
			if err != nil {
				return err
			}
		case 11:
			awaitingAck = false
		case 1:
			if !hello {
				return errors.New("QQ heartbeat before hello")
			}
			if err := write(1, s.seq); err != nil {
				return err
			}
			awaitingAck = true
		case 7:
			return errors.New("QQ gateway requested reconnect")
		case 9:
			var resumable bool
			_ = json.Unmarshal(f.D, &resumable)
			if !resumable {
				s.id = ""
				s.seq = nil
			}
			return errors.New("QQ invalid gateway session")
		case 0:
			if !hello {
				return errors.New("QQ dispatch before hello")
			}
			if f.T == "READY" {
				if ready {
					return errors.New("QQ duplicate ready")
				}
				var d struct {
					SessionID string `json:"session_id"`
					User      struct {
						ID       json.RawMessage `json:"id"`
						Username string          `json:"username"`
					} `json:"user"`
				}
				if json.Unmarshal(f.D, &d) != nil || d.SessionID == "" || f.S == nil {
					return errors.New("QQ invalid ready event")
				}
				s.id = d.SessionID
				s.seq = f.S
				s.botID, _ = qqOfficialScalar(d.User.ID)
				s.botName = d.User.Username
				ready = true
				handshake.Stop()
				connected()
				continue
			}
			if f.T == "RESUMED" {
				if ready || s.id == "" {
					return errors.New("QQ invalid resumed event")
				}
				ready = true
				handshake.Stop()
				connected()
			}
			// Resume replays missed dispatches before the final RESUMED event.
			// Only a connection on which we sent Resume may accept these;
			// a new Identify connection still requires READY first.
			if (!ready && !resuming) || f.S == nil {
				return errors.New("QQ dispatch before ready or missing sequence")
			}
			// All dispatches pass through the ordered acceptance queue, including ignored
			// group/channel events, so they cannot advance past an unaccepted C2C event.
			select {
			case jobs <- qqOfficialJob{ctx, f, accepted}:
			default:
				return errors.New("QQ event queue full")
			}
		}
	}
}
