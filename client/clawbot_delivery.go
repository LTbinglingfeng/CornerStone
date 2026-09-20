package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync"
	"time"
)

const (
	ClawBotDeliveryNotSent   = "not_sent"
	ClawBotDeliveryRejected  = "rejected"
	ClawBotDeliveryUncertain = "uncertain"
)

// ClawBotDeliveryError distinguishes safe retries from potentially delivered messages.
type ClawBotDeliveryError struct {
	Outcome   string
	Retryable bool
	Err       error
}

func (e *ClawBotDeliveryError) Error() string {
	return fmt.Sprintf("clawbot delivery %s: %v", e.Outcome, e.Err)
}
func (e *ClawBotDeliveryError) Unwrap() error { return e.Err }

// ClawBotHTTPError never contains an upstream body or a credential-bearing URL.
type ClawBotHTTPError struct{ StatusCode int }

func (e *ClawBotHTTPError) Error() string { return fmt.Sprintf("clawbot http %d", e.StatusCode) }

// SendTextMessageWithID makes exactly one delivery attempt. Callers must persist and
// reuse clientID across retries; it is a correlation ID, not an idempotency guarantee.
func (c *ClawBotClient) SendTextMessageWithID(ctx context.Context, baseURL, botToken, wechatUIN, toUserID, contextToken, text, clientID string) (*ClawBotSendMessageResponse, error) {
	return c.SendItemMessageWithID(ctx, baseURL, botToken, wechatUIN, toUserID, contextToken, ClawBotIncomingMessageItem{Type: 1, TextItem: &ClawBotItemText{Text: text}}, clientID)
}

// SendItemMessageWithID sends one ordered text or media item, with the same
// conservative acknowledgement rules as text. Upload is deliberately separate.
func (c *ClawBotClient) SendItemMessageWithID(ctx context.Context, baseURL, botToken, wechatUIN, toUserID, contextToken string, item ClawBotIncomingMessageItem, clientID string) (*ClawBotSendMessageResponse, error) {
	fail := func(outcome string, retryable bool, err error) (*ClawBotSendMessageResponse, error) {
		return nil, &ClawBotDeliveryError{Outcome: outcome, Retryable: retryable, Err: err}
	}
	if strings.TrimSpace(clientID) == "" {
		return fail(ClawBotDeliveryNotSent, false, errors.New("clawbot: client ID required"))
	}
	if err := ctx.Err(); err != nil {
		return fail(ClawBotDeliveryNotSent, false, err)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var body struct {
		Msg struct {
			FromUserID   string                       `json:"from_user_id"`
			ToUserID     string                       `json:"to_user_id"`
			ClientID     string                       `json:"client_id"`
			MessageType  int                          `json:"message_type"`
			MessageState int                          `json:"message_state"`
			ContextToken string                       `json:"context_token,omitempty"`
			ItemList     []ClawBotIncomingMessageItem `json:"item_list"`
		} `json:"msg"`
		BaseInfo ClawBotBaseInfo `json:"base_info"`
	}
	body.Msg.ToUserID = strings.TrimSpace(toUserID)
	body.Msg.ClientID = clientID
	body.Msg.MessageType = 2
	body.Msg.MessageState = 2
	body.Msg.ContextToken = strings.TrimSpace(contextToken)
	body.Msg.ItemList = []ClawBotIncomingMessageItem{item}
	body.BaseInfo = c.baseInfo()
	payload, err := json.Marshal(body)
	if err != nil {
		return fail(ClawBotDeliveryNotSent, false, errors.New("clawbot: invalid request"))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, buildClawBotURL(baseURL, "/ilink/bot/sendmessage"), bytes.NewReader(payload))
	if err != nil {
		return fail(ClawBotDeliveryNotSent, false, errors.New("clawbot: invalid request"))
	}
	setClawBotHeaders(req, botToken, wechatUIN)
	// GotConn is deliberately earlier than WroteHeaders: a failed write may have
	// transmitted a partial request without ever invoking WroteHeaders. Missing
	// trace evidence (including custom transports) is never proof of non-delivery.
	var mu sync.Mutex
	var possibleSend, connectionFailed bool
	markPossible := func() { mu.Lock(); possibleSend = true; mu.Unlock() }
	markFailure := func(err error) {
		if err != nil {
			mu.Lock()
			connectionFailed = true
			mu.Unlock()
		}
	}
	trace := &httptrace.ClientTrace{
		GotConn:          func(httptrace.GotConnInfo) { markPossible() },
		WroteHeaders:     markPossible,
		WroteRequest:     func(httptrace.WroteRequestInfo) { markPossible() },
		DNSDone:          func(info httptrace.DNSDoneInfo) { markFailure(info.Err) },
		ConnectDone:      func(_, _ string, err error) { markFailure(err) },
		TLSHandshakeDone: func(_ tls.ConnectionState, err error) { markFailure(err) },
	}
	req = req.WithContext(httptrace.WithClientTrace(ctx, trace))
	hc := *c.httpClient
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := hc.Do(req)
	if err != nil {
		mu.Lock()
		notSent := connectionFailed && !possibleSend
		mu.Unlock()
		if notSent {
			return fail(ClawBotDeliveryNotSent, !errors.Is(err, context.Canceled), clawBotSafeRequestError(req, err))
		}
		return fail(ClawBotDeliveryUncertain, false, clawBotSafeRequestError(req, err))
	}
	defer resp.Body.Close()
	// Gateways and server errors cannot establish whether the application sent it.
	if resp.StatusCode >= 500 || resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusProxyAuthRequired {
		return fail(ClawBotDeliveryUncertain, false, &ClawBotHTTPError{StatusCode: resp.StatusCode})
	}
	if resp.StatusCode >= 300 && resp.StatusCode < 500 {
		return fail(ClawBotDeliveryRejected, resp.StatusCode == http.StatusTooManyRequests, &ClawBotHTTPError{StatusCode: resp.StatusCode})
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fail(ClawBotDeliveryUncertain, false, &ClawBotHTTPError{StatusCode: resp.StatusCode})
	}
	// Read through EOF: a valid JSON prefix in a truncated response is not a receipt.
	const maxReceiptBytes = 1 << 20
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxReceiptBytes+1))
	if err != nil {
		return fail(ClawBotDeliveryUncertain, false, clawBotSafeRequestError(req, err))
	}
	var envelope struct {
		Ret       *int             `json:"ret"`
		ErrCode   int              `json:"errcode"`
		ErrMsg    string           `json:"errmsg"`
		MessageID ClawBotMessageID `json:"message_id"`
	}
	if len(raw) > maxReceiptBytes || json.Unmarshal(raw, &envelope) != nil {
		return fail(ClawBotDeliveryUncertain, false, errors.New("clawbot: invalid delivery response"))
	}
	secrets := []string{botToken, strings.TrimSpace(botToken), contextToken, strings.TrimSpace(contextToken)}
	var requestValue interface{}
	if json.Unmarshal(payload, &requestValue) == nil {
		clawBotCollectSecrets(requestValue, &secrets)
	}
	var value interface{}
	if json.Unmarshal(raw, &value) == nil {
		clawBotCollectSecrets(value, &secrets)
	}
	message := clawBotSanitizeMessage(envelope.ErrMsg, secrets)
	ret := 0
	if envelope.Ret != nil {
		ret = *envelope.Ret
	}
	if ret != 0 || envelope.ErrCode != 0 {
		return fail(ClawBotDeliveryRejected, false, &ClawBotAPIError{Ret: ret, ErrCode: envelope.ErrCode, Message: message})
	}
	if envelope.Ret == nil {
		return fail(ClawBotDeliveryUncertain, false, errors.New("clawbot: missing delivery result"))
	}
	return &ClawBotSendMessageResponse{Ret: ret, ErrCode: envelope.ErrCode, ErrMsg: message, MessageID: envelope.MessageID}, nil
}
