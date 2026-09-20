package client

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/md5" // Required by the iLink upload protocol, not used for security.
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ClawBotUploadedMedia is an upload receipt, NOT a message delivery receipt.
// Its credential-bearing media reference must never be exposed by queue APIs.
type ClawBotUploadedMedia struct {
	Media      ClawBotCDNMedia `json:"media"`
	RawSize    int             `json:"raw_size"`
	CipherSize int             `json:"cipher_size"`
}

// UploadMedia uploads one bounded plaintext payload. Retrying an interrupted
// upload cannot resend a chat message; SendItemMessageWithID is a separate step.
func (c *ClawBotClient) UploadMedia(ctx context.Context, baseURL, token, uin, user string, mediaType int, data []byte) (*ClawBotUploadedMedia, error) {
	if mediaType < 1 || mediaType > 3 || len(data) == 0 || len(data) > clawBotMediaMaxBytes {
		return nil, errors.New("clawbot: invalid upload type or size")
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	key := make([]byte, 16)
	fileKey := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		return nil, errors.New("clawbot: upload key generation failed")
	}
	if _, err := rand.Read(fileKey); err != nil {
		return nil, errors.New("clawbot: upload key generation failed")
	}
	ciphertext, err := encryptClawBotAESECB(data, key)
	if err != nil {
		return nil, err
	}
	sum := md5.Sum(data)
	keyHex := hex.EncodeToString(key)
	fileKeyHex := hex.EncodeToString(fileKey)
	payload, _ := json.Marshal(struct {
		FileKey     string          `json:"filekey"`
		MediaType   int             `json:"media_type"`
		ToUserID    string          `json:"to_user_id"`
		RawSize     int             `json:"rawsize"`
		RawMD5      string          `json:"rawfilemd5"`
		FileSize    int             `json:"filesize"`
		NoNeedThumb bool            `json:"no_need_thumb"`
		AESKey      string          `json:"aeskey"`
		BaseInfo    ClawBotBaseInfo `json:"base_info"`
	}{fileKeyHex, mediaType, user, len(data), hex.EncodeToString(sum[:]), len(ciphertext), true, keyHex, c.baseInfo()})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, buildClawBotURL(baseURL, "/ilink/bot/getuploadurl"), bytes.NewReader(payload))
	if err != nil {
		return nil, errors.New("clawbot: invalid upload request")
	}
	setClawBotHeaders(req, token, uin)
	var result struct {
		UploadParam   string `json:"upload_param"`
		UploadFullURL string `json:"upload_full_url"`
	}
	if err := c.doJSON(req, &result); err != nil {
		return nil, err
	}
	endpoint := strings.TrimSpace(result.UploadFullURL)
	if endpoint == "" {
		if result.UploadParam == "" {
			return nil, errors.New("clawbot: upload URL missing")
		}
		endpoint = buildClawBotCDNUploadURL(defaultClawBotCDNBaseURL, result.UploadParam, fileKeyHex)
	}
	if err := validateClawBotMediaURL(endpoint); err != nil {
		return nil, errors.New("clawbot: unsafe CDN upload URL")
	}
	req, err = http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(ciphertext))
	if err != nil {
		return nil, errors.New("clawbot: invalid CDN upload request")
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	hc, closeTransport := c.clawBotMediaHTTPClient()
	defer closeTransport()
	resp, err := hc.Do(req)
	if err != nil {
		return nil, clawBotSafeRequestError(req, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("clawbot CDN upload http %d", resp.StatusCode)
	}
	downloadParam := strings.TrimSpace(resp.Header.Get("x-encrypted-param"))
	if downloadParam == "" || len(downloadParam) > 16384 {
		return nil, errors.New("clawbot: invalid CDN upload receipt")
	}
	return &ClawBotUploadedMedia{Media: ClawBotCDNMedia{EncryptQueryParam: downloadParam, AESKey: base64.StdEncoding.EncodeToString([]byte(keyHex)), EncryptType: 1}, RawSize: len(data), CipherSize: len(ciphertext)}, nil
}

func buildClawBotCDNUploadURL(base, param, key string) string {
	q := url.Values{"encrypted_query_param": {param}, "filekey": {key}}
	return strings.TrimRight(base, "/") + "/upload?" + q.Encode()
}

func encryptClawBotAESECB(data, key []byte) ([]byte, error) {
	if len(key) != aes.BlockSize {
		return nil, errors.New("clawbot: invalid AES key length")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	pad := aes.BlockSize - len(data)%aes.BlockSize
	padded := make([]byte, len(data)+pad)
	copy(padded, data)
	for i := len(data); i < len(padded); i++ {
		padded[i] = byte(pad)
	}
	for i := 0; i < len(padded); i += aes.BlockSize {
		block.Encrypt(padded[i:i+aes.BlockSize], padded[i:i+aes.BlockSize])
	}
	return padded, nil
}
