package client

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const qqOfficialImageLimit = 5 << 20

type qqOfficialHTTPError struct{ status int }

func (e *qqOfficialHTTPError) Error() string {
	return fmt.Sprintf("QQ HTTP status %d (delivery may be uncertain)", e.status)
}

// QQOfficialClient implements only the official QQ C2C API. Construct with NewQQOfficialClient.
// Its credentials and test seams are deliberately not exported.
type QQOfficialClient struct {
	appID, appSecret string
	httpClient       *http.Client
	tokenURL, apiURL string
	gatewayAllowed   func(string) bool
	tokenMu          sync.Mutex
	tokenGate        chan struct{}
	token            string
	tokenUntil       time.Time
	runGate          chan struct{}
}

type QQOfficialMessage struct {
	ID          string
	UserOpenID  string
	Content     string
	Attachments []QQOfficialAttachment
	Timestamp   time.Time
}
type QQOfficialAttachment struct{ URL, ContentType, Filename string }
type QQOfficialStatus struct{ Status, BotID, BotName, LastError string }

func qqOfficialHTTPClient() *http.Client {
	return &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
func NewQQOfficialClient(appID, appSecret string) *QQOfficialClient {
	return &QQOfficialClient{appID: appID, appSecret: appSecret, httpClient: qqOfficialHTTPClient(), tokenURL: "https://api.bot.qq.com/app/getAppAccessToken", apiURL: "https://api.bot.qq.com", gatewayAllowed: qqOfficialGatewayAllowed, runGate: make(chan struct{}, 1), tokenGate: make(chan struct{}, 1)}
}

// qqOfficialJSON never includes upstream bodies, URLs or transport errors in errors.
// A POST is issued exactly once; a failure may mean delivery was successful.
func qqOfficialJSON(ctx context.Context, hc *http.Client, method, endpoint, token string, payload, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return errors.New("QQ request encoding failed")
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return errors.New("QQ invalid request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "QQBot "+token)
	}
	safe := *hc
	safe.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := safe.Do(req)
	if err != nil {
		return errors.New("QQ HTTP request failed (delivery may be uncertain)")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &qqOfficialHTTPError{status: resp.StatusCode}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(b) > 1<<20 {
		return errors.New("QQ invalid response size")
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(b, &envelope) != nil || envelope == nil {
		return errors.New("QQ invalid JSON response")
	}
	for _, key := range []string{"code", "retcode"} {
		if raw, ok := envelope[key]; ok {
			v, e := qqOfficialScalar(raw)
			if e != nil || v != "0" {
				return errors.New("QQ API rejected request")
			}
		}
	}
	if json.Unmarshal(b, out) != nil {
		return errors.New("QQ invalid response fields")
	}
	return nil
}

// Decode decimal IDs without converting them through float64.
func qqOfficialScalar(raw json.RawMessage) (string, error) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, nil
	}
	s = string(raw)
	if s == "" {
		return "", errors.New("missing value")
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return "", errors.New("invalid decimal value")
		}
	}
	return s, nil
}
func (c *QQOfficialClient) accessToken(ctx context.Context) (string, error) {
	// Waiting for another refresh must remain cancellable.
	select {
	case c.tokenGate <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { <-c.tokenGate }()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	if c.token != "" && time.Now().Before(c.tokenUntil) {
		return c.token, nil
	}
	var r struct {
		Token   string          `json:"access_token"`
		Expires json.RawMessage `json:"expires_in"`
	}
	if err := qqOfficialJSON(ctx, c.httpClient, http.MethodPost, c.tokenURL, "", map[string]string{"appId": c.appID, "clientSecret": c.appSecret}, &r); err != nil {
		return "", err
	}
	seconds, e := qqOfficialScalar(r.Expires)
	n, err := strconv.ParseInt(seconds, 10, 64)
	if e != nil || err != nil || n <= 0 || n > 86400*30 || r.Token == "" {
		return "", errors.New("QQ invalid access token response")
	}
	ttl := time.Duration(n) * time.Second
	margin := time.Minute
	if ttl/10 < margin {
		margin = ttl / 10
	}
	c.token = r.Token
	c.tokenUntil = time.Now().Add(ttl - margin)
	return c.token, nil
}

// Invalidation only affects later operations. Never replay an uncertain POST.
func (c *QQOfficialClient) rejectToken(token string) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	if c.token == token {
		c.tokenUntil = time.Time{}
	}
}
func (c *QQOfficialClient) rejectUnauthorizedToken(token string, err error) {
	var status *qqOfficialHTTPError
	if errors.As(err, &status) && status.status == http.StatusUnauthorized {
		c.rejectToken(token)
	}
}
func (c *QQOfficialClient) send(ctx context.Context, openid string, payload any) (string, error) {
	if strings.TrimSpace(openid) == "" {
		return "", errors.New("QQ missing user openid")
	}
	token, err := c.accessToken(ctx)
	if err != nil {
		return "", err
	}
	var r struct {
		ID json.RawMessage `json:"id"`
	}
	err = qqOfficialJSON(ctx, c.httpClient, http.MethodPost, c.apiURL+"/v2/users/"+url.PathEscape(openid)+"/messages", token, payload, &r)
	if err != nil {
		c.rejectUnauthorizedToken(token, err)
		return "", err
	}
	id, err := qqOfficialScalar(r.ID)
	if err != nil || id == "" || id == "0" {
		return "", errors.New("QQ missing sent message ID (delivery may be uncertain)")
	}
	return id, nil
}
func (c *QQOfficialClient) SendText(ctx context.Context, openid, replyID string, seq int, text string) (string, error) {
	if strings.TrimSpace(text) == "" || seq < 1 {
		return "", errors.New("QQ invalid text or message sequence")
	}
	return c.send(ctx, openid, map[string]any{"msg_type": 0, "content": text, "msg_id": replyID, "msg_seq": seq})
}
func qqOfficialImage(data []byte) bool {
	if len(data) == 0 || len(data) > qqOfficialImageLimit {
		return false
	}
	switch http.DetectContentType(data) {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return true
	}
	return false
}
func (c *QQOfficialClient) SendImage(ctx context.Context, openid, replyID string, seq int, data []byte, name string) (string, error) {
	if !qqOfficialImage(data) || strings.TrimSpace(openid) == "" || seq < 1 {
		return "", errors.New("QQ invalid image, openid or message sequence")
	}
	token, err := c.accessToken(ctx)
	if err != nil {
		return "", err
	}
	var r struct {
		FileInfo string `json:"file_info"`
	}
	// Binary uploads require file_name on the current official API.
	if name == "" {
		name = "image.png"
	}
	if len(name) > 255 || name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00\r\n") {
		return "", errors.New("QQ invalid image filename")
	}
	err = qqOfficialJSON(ctx, c.httpClient, http.MethodPost, c.apiURL+"/v2/users/"+url.PathEscape(openid)+"/files", token, map[string]any{"file_type": 1, "file_name": name, "file_data": base64.StdEncoding.EncodeToString(data), "srv_send_msg": false}, &r)
	if err != nil {
		c.rejectUnauthorizedToken(token, err)
		return "", err
	}
	if r.FileInfo == "" {
		return "", errors.New("QQ missing uploaded file info")
	}
	return c.send(ctx, openid, map[string]any{"msg_type": 7, "media": map[string]string{"file_info": r.FileInfo}, "msg_id": replyID, "msg_seq": seq})
}

// DownloadQQOfficialImage uses a separate unauthenticated, DNS-pinned public HTTPS transport.
// Redirects are rejected, including redirects to private addresses.
func DownloadQQOfficialImage(ctx context.Context, rawURL string) ([]byte, error) {
	if strings.HasPrefix(rawURL, "//") {
		rawURL = "https:" + rawURL
	}
	if validateClawBotMediaURL(rawURL) != nil {
		return nil, errors.New("QQ invalid public image URL")
	}
	c := NewClawBotClient()
	hc, closeTransport := c.clawBotMediaHTTPClient()
	defer closeTransport()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, errors.New("QQ invalid image request")
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, errors.New("QQ image download failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("QQ image download rejected")
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, qqOfficialImageLimit+1))
	if err != nil || !qqOfficialImage(b) {
		return nil, errors.New("QQ invalid or oversized image")
	}
	return b, nil
}
