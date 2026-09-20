package client

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

type ClawBotClient struct {
	httpClient *http.Client
	botAgent   string
}

type ClawBotQRCodeResponse struct {
	QRCode           string `json:"qrcode"`
	QRCodeImgContent string `json:"qrcode_img_content"`
}

type ClawBotQRCodeStatusResponse struct {
	Status       string `json:"status"`
	BotToken     string `json:"bot_token"`
	BaseURL      string `json:"baseurl"`
	ILinkUserID  string `json:"ilink_user_id"`
	ILinkBotID   string `json:"ilink_bot_id"`
	RedirectHost string `json:"redirect_host"`
}

type ClawBotGetUpdatesRequest struct {
	GetUpdatesBuf string          `json:"get_updates_buf"`
	BaseInfo      ClawBotBaseInfo `json:"base_info"`
}

type ClawBotItemText struct {
	Text string `json:"text"`
}

type ClawBotCDNMedia struct {
	EncryptQueryParam string `json:"encrypt_query_param,omitempty"`
	AESKey            string `json:"aes_key,omitempty"`
	EncryptType       int    `json:"encrypt_type,omitempty"`
	FullURL           string `json:"full_url,omitempty"`
}

type ClawBotImageItem struct {
	Media      *ClawBotCDNMedia `json:"media,omitempty"`
	ThumbMedia *ClawBotCDNMedia `json:"thumb_media,omitempty"`
	AESKey     string           `json:"aeskey,omitempty"`
	MidSize    int              `json:"mid_size,omitempty"`
	ThumbSize  int              `json:"thumb_size,omitempty"`
	HDSize     int              `json:"hd_size,omitempty"`
}

type ClawBotIncomingMessageItem struct {
	IsReference bool              `json:"-"`
	Raw         json.RawMessage   `json:"-"`
	RefMsg      *ClawBotReference `json:"ref_msg,omitempty"`
	MsgID       ClawBotMessageID  `json:"msg_id,omitempty"`
	Type        int               `json:"type"`
	TextItem    *ClawBotItemText  `json:"text_item,omitempty"`
	ImageItem   *ClawBotImageItem `json:"image_item,omitempty"`
	VoiceItem   *ClawBotVoiceItem `json:"voice_item,omitempty"`
	VideoItem   *ClawBotVideoItem `json:"video_item,omitempty"`
	FileItem    *ClawBotFileItem  `json:"file_item,omitempty"`
}

// ClawBotMessageID keeps uint64 identifiers lossless for replay detection.
type ClawBotMessageID string

func (id *ClawBotMessageID) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		*id = ""
		return nil
	}
	var value string
	if len(data) > 0 && data[0] == '"' {
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
	} else {
		value = string(data)
	}
	if value != "" {
		if _, err := strconv.ParseUint(value, 10, 64); err != nil {
			return fmt.Errorf("invalid ClawBot message ID")
		}
	}
	*id = ClawBotMessageID(value)
	return nil
}

type ClawBotIncomingMessage struct {
	SessionID    string                       `json:"session_id,omitempty"`
	ToUserID     string                       `json:"to_user_id,omitempty"`
	MessageID    ClawBotMessageID             `json:"message_id,omitempty"`
	MessageType  int                          `json:"message_type"`
	FromUserID   string                       `json:"from_user_id"`
	ContextToken string                       `json:"context_token"`
	ItemList     []ClawBotIncomingMessageItem `json:"item_list"`
}

type ClawBotGetUpdatesResponse struct {
	LongPollingTimeoutMS int                      `json:"longpolling_timeout_ms"`
	Ret                  int                      `json:"ret"`
	ErrMsg               string                   `json:"errmsg"`
	ErrCode              int                      `json:"errcode"`
	GetUpdatesBuf        string                   `json:"get_updates_buf"`
	Msgs                 []ClawBotIncomingMessage `json:"msgs"`
}

type ClawBotSendMessageRequest struct {
	Msg struct {
		FromUserID   string `json:"from_user_id"`
		ToUserID     string `json:"to_user_id"`
		ClientID     string `json:"client_id"`
		MessageType  int    `json:"message_type"`
		MessageState int    `json:"message_state"`
		ContextToken string `json:"context_token,omitempty"`
		ItemList     []struct {
			Type     int              `json:"type"`
			TextItem *ClawBotItemText `json:"text_item,omitempty"`
		} `json:"item_list"`
	} `json:"msg"`
	BaseInfo ClawBotBaseInfo `json:"base_info"`
}

type ClawBotSendMessageResponse struct {
	Ret       int              `json:"ret"`
	ErrCode   int              `json:"errcode"`
	ErrMsg    string           `json:"errmsg"`
	MessageID ClawBotMessageID `json:"message_id,omitempty"`
}

const ClawBotStaleTokenCode = -14

// ClawBotAPIError preserves business error codes without confusing them with HTTP errors.
type ClawBotAPIError struct {
	Ret     int
	ErrCode int
	Message string
}

func (e *ClawBotAPIError) Error() string {
	return fmt.Sprintf("clawbot API ret=%d errcode=%d: %s", e.Ret, e.ErrCode, e.Message)
}

func IsClawBotStaleToken(err error) bool {
	var apiErr *ClawBotAPIError
	return errors.As(err, &apiErr) && (apiErr.Ret == ClawBotStaleTokenCode || apiErr.ErrCode == ClawBotStaleTokenCode)
}

func (r *ClawBotGetUpdatesResponse) APIError() error {
	if r.Ret == 0 && r.ErrCode == 0 {
		return nil
	}
	return &ClawBotAPIError{Ret: r.Ret, ErrCode: r.ErrCode, Message: r.ErrMsg}
}

const (
	defaultClawBotCDNBaseURL = "https://novac2c.cdn.weixin.qq.com/c2c"
	clawBotMediaMaxBytes     = 100 << 20
)

// These protocol versions align with upstream reference 2.4.9, not CornerStone's build version.
const clawBotChannelVersion = "2.4.9"
const clawBotClientVersion = (2 << 16) | (4 << 8) | 9

var ErrClawBotPollTimeout = errors.New("clawbot poll timeout")

type ClawBotBaseInfo struct {
	ChannelVersion string `json:"channel_version"`
	BotAgent       string `json:"bot_agent"`
}

func NewClawBotClient(appVersion ...string) *ClawBotClient {
	version := "dev"
	if len(appVersion) > 0 {
		candidate := strings.TrimSpace(appVersion[0])
		// A build version is one UA-style ASCII version token (upstream limit: 32 bytes).
		valid := len(candidate) > 0 && len(candidate) <= 32
		for _, ch := range candidate {
			if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || strings.ContainsRune("_.+-", ch)) {
				valid = false
			}
		}
		if valid {
			version = candidate
		}
	}
	return &ClawBotClient{httpClient: newHTTPClient(), botAgent: "CornerStone/" + version}
}

func (c *ClawBotClient) baseInfo() ClawBotBaseInfo {
	agent := c.botAgent
	if agent == "" {
		agent = "CornerStone/dev"
	}
	return ClawBotBaseInfo{ChannelVersion: clawBotChannelVersion, BotAgent: agent}
}

func setClawBotHeaders(req *http.Request, token, uin string) {
	req.Header.Set("iLink-App-Id", "bot")
	req.Header.Set("iLink-App-ClientVersion", strconv.Itoa(clawBotClientVersion))
	if req.Method == http.MethodGet {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("AuthorizationType", "ilink_bot_token")
	req.Header.Set("X-WECHAT-UIN", uin)
	if token = strings.TrimSpace(token); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

func GenerateClawBotWechatUIN() (string, error) {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	value := binary.BigEndian.Uint32(buf)
	return base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%d", value))), nil
}

func (c *ClawBotClient) GetBotQRCode(ctx context.Context, baseURL string, localTokens ...string) (*ClawBotQRCodeResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	endpoint := buildClawBotURL(baseURL, "/ilink/bot/get_bot_qrcode?bot_type=3")
	if localTokens == nil {
		localTokens = []string{}
	}
	payload, err := json.Marshal(struct {
		LocalTokens []string `json:"local_token_list"`
	}{localTokens})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, errors.New("clawbot: invalid request")
	}
	uin, err := GenerateClawBotWechatUIN()
	if err != nil {
		return nil, errors.New("clawbot: invalid request")
	}
	setClawBotHeaders(req, "", uin)
	var resp ClawBotQRCodeResponse
	if err := c.doJSON(req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *ClawBotClient) GetQRCodeStatus(ctx context.Context, baseURL, qrcode string, verifyCode ...string) (*ClawBotQRCodeStatusResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	query := url.Values{}
	query.Set("qrcode", qrcode)
	if len(verifyCode) > 0 && verifyCode[0] != "" {
		query.Set("verify_code", verifyCode[0])
	}
	endpoint := buildClawBotURL(baseURL, "/ilink/bot/get_qrcode_status?"+query.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, errors.New("clawbot: invalid request")
	}
	setClawBotHeaders(req, "", "")

	var resp ClawBotQRCodeStatusResponse
	if err := c.doJSON(req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *ClawBotClient) GetUpdates(ctx context.Context, baseURL, botToken, getUpdatesBuf, wechatUIN string, pollTimeout ...time.Duration) (*ClawBotGetUpdatesResponse, error) {
	parent := ctx
	timeout := 35 * time.Second
	if len(pollTimeout) > 0 && pollTimeout[0] > 0 {
		timeout = pollTimeout[0]
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	body := ClawBotGetUpdatesRequest{
		GetUpdatesBuf: strings.TrimSpace(getUpdatesBuf),
	}
	body.BaseInfo = c.baseInfo()

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, buildClawBotURL(baseURL, "/ilink/bot/getupdates"), bytes.NewReader(payload))
	if err != nil {
		return nil, errors.New("clawbot: invalid request")
	}
	setClawBotHeaders(req, botToken, wechatUIN)

	var resp ClawBotGetUpdatesResponse
	if err := c.doJSON(req, &resp); err != nil {
		if parent.Err() != nil {
			return nil, parent.Err()
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, ErrClawBotPollTimeout
		}
		return nil, err
	}
	return &resp, nil
}

func (c *ClawBotClient) SendTextMessage(ctx context.Context, baseURL, botToken, wechatUIN, toUserID, contextToken, text string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	body := ClawBotSendMessageRequest{}
	body.Msg.FromUserID = ""
	body.Msg.ToUserID = strings.TrimSpace(toUserID)
	body.Msg.ClientID = uuid.NewString()
	body.Msg.MessageType = 2
	body.Msg.MessageState = 2
	body.Msg.ContextToken = strings.TrimSpace(contextToken)
	body.Msg.ItemList = []struct {
		Type     int              `json:"type"`
		TextItem *ClawBotItemText `json:"text_item,omitempty"`
	}{
		{
			Type: 1,
			TextItem: &ClawBotItemText{
				Text: text,
			},
		},
	}
	body.BaseInfo = c.baseInfo()

	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, buildClawBotURL(baseURL, "/ilink/bot/sendmessage"), bytes.NewReader(payload))
	if err != nil {
		return errors.New("clawbot: invalid request")
	}
	setClawBotHeaders(req, botToken, wechatUIN)

	var resp ClawBotSendMessageResponse
	if err := c.doJSON(req, &resp); err != nil {
		return err
	}

	return nil
}

func ExtractTextFromClawBotMessage(msg ClawBotIncomingMessage) string {
	for _, item := range msg.ItemList {
		switch item.Type {
		case 1:
			if item.TextItem != nil {
				if text := strings.TrimSpace(item.TextItem.Text); text != "" {
					return text
				}
			}
		case 3:
			if item.VoiceItem != nil {
				if text := strings.TrimSpace(item.VoiceItem.Text); text != "" {
					return text
				}
			}
		}
	}
	return ""
}

func ExtractImageItemsFromClawBotMessage(msg ClawBotIncomingMessage) []*ClawBotImageItem {
	items := make([]*ClawBotImageItem, 0, len(msg.ItemList))
	for _, item := range msg.ItemList {
		if item.Type != 2 || item.ImageItem == nil {
			continue
		}
		items = append(items, item.ImageItem)
	}
	return items
}

func (c *ClawBotClient) DownloadImageItem(ctx context.Context, imageItem *ClawBotImageItem) ([]byte, error) {
	if imageItem == nil {
		return nil, errors.New("missing image")
	}
	return c.downloadClawBotMedia(ctx, imageItem.Media, imageItem.AESKey)
}

func buildClawBotURL(baseURL, endpoint string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	return baseURL + endpoint
}

func buildClawBotCDNDownloadURL(baseURL, encryptedQueryParam string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	return baseURL + "/download?encrypted_query_param=" + url.QueryEscape(strings.TrimSpace(encryptedQueryParam))
}

// NotifyStart and NotifyStop use the caller's context; shutdown callers should
// supply a fresh context if their polling context has already been canceled.
func (c *ClawBotClient) NotifyStart(ctx context.Context, baseURL, botToken, wechatUIN string) error {
	return c.notify(ctx, baseURL, botToken, wechatUIN, "notifystart")
}

func (c *ClawBotClient) NotifyStop(ctx context.Context, baseURL, botToken, wechatUIN string) error {
	return c.notify(ctx, baseURL, botToken, wechatUIN, "notifystop")
}

func (c *ClawBotClient) notify(ctx context.Context, baseURL, botToken, wechatUIN, operation string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	payload, err := json.Marshal(struct {
		BaseInfo ClawBotBaseInfo `json:"base_info"`
	}{c.baseInfo()})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, buildClawBotURL(baseURL, "/ilink/bot/msg/"+operation), bytes.NewReader(payload))
	if err != nil {
		return errors.New("clawbot: invalid notify request")
	}
	setClawBotHeaders(req, botToken, wechatUIN)
	var resp ClawBotSendMessageResponse
	return c.doJSON(req, &resp)
}

func clawBotSafeRequestError(req *http.Request, err error) error {
	// Never wrap net/http's URL errors: they include QR and verification queries.
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		if errors.Is(err, cause) {
			return fmt.Errorf("clawbot request failed: %w", cause)
		}
	}
	if req.Context().Err() != nil {
		return fmt.Errorf("clawbot request failed: %w", req.Context().Err())
	}
	var dnsErr *net.DNSError
	var tlsErr *tls.CertificateVerificationError
	var certificateErr x509.UnknownAuthorityError
	var hostnameErr x509.HostnameError
	var opErr *net.OpError
	var netErr net.Error
	switch {
	case errors.As(err, &dnsErr):
		return errors.New("clawbot request failed (dns)")
	case errors.As(err, &tlsErr), errors.As(err, &certificateErr), errors.As(err, &hostnameErr):
		return errors.New("clawbot request failed (tls)")
	case errors.As(err, &netErr) && netErr.Timeout():
		return errors.New("clawbot request failed (timeout)")
	case errors.As(err, &opErr):
		return errors.New("clawbot request failed (tcp)")
	default:
		return errors.New("clawbot request failed (network or invalid response)")
	}
}

func clawBotCollectSecrets(value interface{}, secrets *[]string) {
	switch v := value.(type) {
	case map[string]interface{}:
		for key, child := range v {
			switch strings.ToLower(key) {
			case "bot_token", "context_token", "verify_code", "qrcode", "qrcode_img_content":
				if text, ok := child.(string); ok && text != "" {
					*secrets = append(*secrets, text)
				}
			case "local_token_list":
				if tokens, ok := child.([]interface{}); ok {
					for _, token := range tokens {
						if text, ok := token.(string); ok && text != "" {
							*secrets = append(*secrets, text)
						}
					}
				}
			}
			clawBotCollectSecrets(child, secrets)
		}
	case []interface{}:
		for _, child := range v {
			clawBotCollectSecrets(child, secrets)
		}
	}
}

func clawBotSanitizeMessage(message string, secrets []string) string {
	// Unknown credentials labelled by the upstream are also suppressed, even if
	// they were not sent by this request. Do not attempt to parse arbitrary prose.
	lower := strings.ToLower(message)
	for _, key := range []string{"bot_token", "context_token", "verify_code", "qrcode"} {
		if strings.Contains(lower, key) {
			return "[redacted]"
		}
	}
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		for _, form := range []string{secret, url.QueryEscape(secret), url.PathEscape(secret)} {
			message = strings.ReplaceAll(message, form, "[redacted]")
		}
	}
	return message
}

func (c *ClawBotClient) doJSON(req *http.Request, dst interface{}) error {
	// Copy rather than mutate the shared client: no credential-bearing JSON API
	// request may follow even a same-origin redirect (including 307/308 POSTs).
	hc := *c.httpClient
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := hc.Do(req)
	if err != nil {
		return clawBotSafeRequestError(req, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Untrusted error bodies may echo arbitrary credentials; omit them entirely.
		return fmt.Errorf("clawbot http %d", resp.StatusCode)
	}
	secrets := []string{strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")}
	for _, key := range []string{"qrcode", "verify_code"} {
		secrets = append(secrets, req.URL.Query().Get(key))
	}
	if req.GetBody != nil {
		body, err := req.GetBody()
		if err == nil {
			var value interface{}
			if json.NewDecoder(body).Decode(&value) == nil {
				clawBotCollectSecrets(value, &secrets)
			}
			body.Close()
		}
	}
	var raw json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return clawBotSafeRequestError(req, err)
	}
	var value interface{}
	if err := json.Unmarshal(raw, &value); err != nil {
		return errors.New("clawbot: invalid JSON response")
	}
	if _, ok := value.(map[string]interface{}); !ok {
		return errors.New("clawbot: invalid JSON response")
	}
	clawBotCollectSecrets(value, &secrets)
	var envelope ClawBotSendMessageResponse
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return errors.New("clawbot: invalid JSON response")
	}
	envelope.ErrMsg = clawBotSanitizeMessage(envelope.ErrMsg, secrets)
	if dst != nil {
		if err := json.Unmarshal(raw, dst); err != nil {
			return errors.New("clawbot: invalid JSON response")
		}
	}
	// Preserve GetUpdates' existing response + APIError contract.
	if updates, ok := dst.(*ClawBotGetUpdatesResponse); ok {
		updates.ErrMsg = envelope.ErrMsg
		return nil
	}
	if envelope.Ret != 0 || envelope.ErrCode != 0 {
		return &ClawBotAPIError{Ret: envelope.Ret, ErrCode: envelope.ErrCode, Message: envelope.ErrMsg}
	}
	return nil
}

func parseClawBotImageAESKey(imageItem *ClawBotImageItem) ([]byte, error) {
	if imageItem == nil {
		return nil, fmt.Errorf("clawbot image item is nil")
	}

	if rawHex := strings.TrimSpace(imageItem.AESKey); rawHex != "" {
		key, err := hex.DecodeString(rawHex)
		if err != nil {
			return nil, fmt.Errorf("decode clawbot image aeskey hex: %w", err)
		}
		if len(key) != 16 {
			return nil, fmt.Errorf("clawbot image aeskey hex must be 16 bytes, got %d", len(key))
		}
		return key, nil
	}

	if imageItem.Media == nil {
		return nil, nil
	}
	return parseClawBotMediaAESKey(imageItem.Media.AESKey)
}

func parseClawBotMediaAESKey(aesKeyBase64 string) ([]byte, error) {
	aesKeyBase64 = strings.TrimSpace(aesKeyBase64)
	if aesKeyBase64 == "" {
		return nil, nil
	}

	decoded, err := base64.StdEncoding.DecodeString(aesKeyBase64)
	if err != nil {
		return nil, fmt.Errorf("decode clawbot media aes key: %w", err)
	}

	switch {
	case len(decoded) == 16:
		return decoded, nil
	case len(decoded) == 32 && isASCIIHex(decoded):
		key, err := hex.DecodeString(string(decoded))
		if err != nil {
			return nil, fmt.Errorf("decode clawbot media aes key hex payload: %w", err)
		}
		if len(key) != 16 {
			return nil, fmt.Errorf("clawbot media aes key hex payload must be 16 bytes, got %d", len(key))
		}
		return key, nil
	default:
		return nil, fmt.Errorf("clawbot media aes key must decode to 16 bytes or 32-char hex, got %d bytes", len(decoded))
	}
}

func decryptClawBotAESECB(ciphertext, key []byte) ([]byte, error) {
	if len(ciphertext) == 0 {
		return nil, nil
	}
	if len(ciphertext)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("clawbot media ciphertext is not a multiple of block size")
	}
	if len(key) != 16 {
		return nil, fmt.Errorf("clawbot media aes key must be 16 bytes, got %d", len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	plaintext := make([]byte, len(ciphertext))
	for offset := 0; offset < len(ciphertext); offset += aes.BlockSize {
		block.Decrypt(plaintext[offset:offset+aes.BlockSize], ciphertext[offset:offset+aes.BlockSize])
	}
	return stripPKCS7Padding(plaintext, aes.BlockSize)
}

func stripPKCS7Padding(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, fmt.Errorf("invalid pkcs7 padded data length")
	}

	padding := int(data[len(data)-1])
	if padding <= 0 || padding > blockSize || padding > len(data) {
		return nil, fmt.Errorf("invalid pkcs7 padding size")
	}

	for _, b := range data[len(data)-padding:] {
		if int(b) != padding {
			return nil, fmt.Errorf("invalid pkcs7 padding bytes")
		}
	}
	return data[:len(data)-padding], nil
}

func isASCIIHex(data []byte) bool {
	for _, b := range data {
		switch {
		case b >= '0' && b <= '9':
		case b >= 'a' && b <= 'f':
		case b >= 'A' && b <= 'F':
		default:
			return false
		}
	}
	return true
}
