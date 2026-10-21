package client

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Key is server-only and excluded from JSON. Never log binding objects.
type QQOfficialBinding struct {
	TaskID    string    `json:"task_id"`
	Key       []byte    `json:"-"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Credentials are for server-side use, not frontend serialization.
type QQOfficialBindResult struct {
	Status    string `json:"status"`
	AppID     string `json:"-"`
	AppSecret string `json:"-"`
}

func StartQQOfficialBinding(ctx context.Context) (*QQOfficialBinding, error) {
	return qqOfficialStartBinding(ctx, qqOfficialHTTPClient(), "https://q.qq.com")
}
func PollQQOfficialBinding(ctx context.Context, binding *QQOfficialBinding) (*QQOfficialBindResult, error) {
	return qqOfficialPollBinding(ctx, qqOfficialHTTPClient(), "https://q.qq.com", binding)
}
func qqOfficialStartBinding(ctx context.Context, hc *http.Client, endpoint string) (*QQOfficialBinding, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, errors.New("QQ binding key generation failed")
	}
	var r struct {
		Data struct {
			TaskID  string          `json:"task_id"`
			Expires json.RawMessage `json:"expires_in"`
		} `json:"data"`
	}
	if err := qqOfficialJSON(ctx, hc, http.MethodPost, endpoint+"/lite/create_bind_task", "", map[string]string{"key": base64.StdEncoding.EncodeToString(key)}, &r); err != nil {
		return nil, err
	}
	if strings.TrimSpace(r.Data.TaskID) == "" {
		return nil, errors.New("QQ missing binding task ID")
	}
	// The lite API does not guarantee an expiry field. Apply a conservative local
	// five-minute lifetime, shortened by any valid server-provided expires_in.
	ttl := 5 * time.Minute
	if len(r.Data.Expires) > 0 {
		v, e := qqOfficialScalar(r.Data.Expires)
		n, err := strconv.ParseInt(v, 10, 64)
		if e != nil || err != nil || n <= 0 {
			return nil, errors.New("QQ invalid binding expiry")
		}
		if n < 300 {
			ttl = time.Duration(n) * time.Second
		}
	}
	return &QQOfficialBinding{TaskID: r.Data.TaskID, Key: key, URL: "https://q.qq.com/qqbot/openclaw/connect.html?task_id=" + url.QueryEscape(r.Data.TaskID) + "&_wv=2", ExpiresAt: time.Now().Add(ttl)}, nil
}
func qqOfficialPollBinding(ctx context.Context, hc *http.Client, endpoint string, b *QQOfficialBinding) (*QQOfficialBindResult, error) {
	if b == nil || b.TaskID == "" || len(b.Key) != 32 {
		return nil, errors.New("QQ invalid binding")
	}
	if b.ExpiresAt.IsZero() || !time.Now().Before(b.ExpiresAt) {
		return &QQOfficialBindResult{Status: "expired"}, nil
	}
	var r struct {
		Data struct {
			Status json.RawMessage `json:"status"`
			AppID  json.RawMessage `json:"bot_appid"`
			Secret string          `json:"bot_encrypt_secret"`
		} `json:"data"`
	}
	if err := qqOfficialJSON(ctx, hc, http.MethodPost, endpoint+"/lite/poll_bind_result", "", map[string]string{"task_id": b.TaskID}, &r); err != nil {
		return nil, err
	}
	status, err := qqOfficialScalar(r.Data.Status)
	if err != nil {
		return nil, errors.New("QQ invalid binding status")
	}
	switch status {
	case "0", "1":
		return &QQOfficialBindResult{Status: "pending"}, nil
	case "3":
		return &QQOfficialBindResult{Status: "expired"}, nil
	case "2":
		id, err := qqOfficialScalar(r.Data.AppID)
		if err != nil || id == "" || id == "0" {
			return nil, errors.New("QQ missing binding app ID")
		}
		for _, c := range id {
			if c < '0' || c > '9' {
				return nil, errors.New("QQ invalid binding app ID")
			}
		}
		secret, err := qqOfficialDecryptSecret(r.Data.Secret, b.Key)
		if err != nil {
			return nil, err
		}
		return &QQOfficialBindResult{Status: "created", AppID: id, AppSecret: secret}, nil
	default:
		return nil, errors.New("QQ unknown binding status")
	}
}
func qqOfficialDecryptSecret(encoded string, key []byte) (string, error) {
	failure := errors.New("QQ binding secret authentication failed")
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(raw) <= 28 || len(key) != 32 {
		return "", failure
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", failure
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", failure
	}
	plain, err := gcm.Open(nil, raw[:12], raw[12:], nil)
	if err != nil || len(plain) == 0 || !utf8.Valid(plain) {
		return "", failure
	}
	return string(plain), nil
}
