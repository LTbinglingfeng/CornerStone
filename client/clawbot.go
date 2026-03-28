package client

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
)

type ClawBotClient struct {
	httpClient *http.Client
}

type ClawBotQRCodeResponse struct {
	QRCode           string `json:"qrcode"`
	QRCodeImgContent string `json:"qrcode_img_content"`
}

type ClawBotQRCodeStatusResponse struct {
	Status      string `json:"status"`
	BotToken    string `json:"bot_token"`
	BaseURL     string `json:"baseurl"`
	ILinkUserID string `json:"ilink_user_id"`
}

type ClawBotGetUpdatesRequest struct {
	GetUpdatesBuf string `json:"get_updates_buf"`
	BaseInfo      struct {
		ChannelVersion string `json:"channel_version"`
	} `json:"base_info"`
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
	Type      int               `json:"type"`
	TextItem  *ClawBotItemText  `json:"text_item,omitempty"`
	ImageItem *ClawBotImageItem `json:"image_item,omitempty"`
	VoiceItem *ClawBotItemText  `json:"voice_item,omitempty"`
}

type ClawBotIncomingMessage struct {
	MessageType  int                          `json:"message_type"`
	FromUserID   string                       `json:"from_user_id"`
	ContextToken string                       `json:"context_token"`
	ItemList     []ClawBotIncomingMessageItem `json:"item_list"`
}

type ClawBotGetUpdatesResponse struct {
	ErrCode       int                      `json:"errcode"`
	GetUpdatesBuf string                   `json:"get_updates_buf"`
	Msgs          []ClawBotIncomingMessage `json:"msgs"`
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
	BaseInfo struct {
		ChannelVersion string `json:"channel_version"`
	} `json:"base_info"`
}

type ClawBotSendMessageResponse struct {
	Ret int `json:"ret"`
}

const (
	defaultClawBotCDNBaseURL = "https://novac2c.cdn.weixin.qq.com/c2c"
	clawBotMediaMaxBytes     = 100 << 20
)

func NewClawBotClient() *ClawBotClient {
	return &ClawBotClient{
		httpClient: newHTTPClient(),
	}
}

func GenerateClawBotWechatUIN() (string, error) {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	value := int32(binary.BigEndian.Uint32(buf))
	if value < 0 {
		value = -value
	}
	return base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%d", value))), nil
}

func (c *ClawBotClient) GetBotQRCode(ctx context.Context, baseURL string) (*ClawBotQRCodeResponse, error) {
	endpoint := buildClawBotURL(baseURL, "/ilink/bot/get_bot_qrcode?bot_type=3")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	var resp ClawBotQRCodeResponse
	if err := c.doJSON(req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *ClawBotClient) GetQRCodeStatus(ctx context.Context, baseURL, qrcode string) (*ClawBotQRCodeStatusResponse, error) {
	query := url.Values{}
	query.Set("qrcode", qrcode)
	endpoint := buildClawBotURL(baseURL, "/ilink/bot/get_qrcode_status?"+query.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("iLink-App-ClientVersion", "1")

	var resp ClawBotQRCodeStatusResponse
	if err := c.doJSON(req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *ClawBotClient) GetUpdates(ctx context.Context, baseURL, botToken, getUpdatesBuf, wechatUIN string) (*ClawBotGetUpdatesResponse, error) {
	body := ClawBotGetUpdatesRequest{
		GetUpdatesBuf: strings.TrimSpace(getUpdatesBuf),
	}
	body.BaseInfo.ChannelVersion = "1.0.0"

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, buildClawBotURL(baseURL, "/ilink/bot/getupdates"), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(botToken))
	req.Header.Set("AuthorizationType", "ilink_bot_token")
	req.Header.Set("X-WECHAT-UIN", wechatUIN)

	var resp ClawBotGetUpdatesResponse
	if err := c.doJSON(req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *ClawBotClient) SendTextMessage(ctx context.Context, baseURL, botToken, wechatUIN, toUserID, contextToken, text string) error {
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
	body.BaseInfo.ChannelVersion = "1.0.0"

	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, buildClawBotURL(baseURL, "/ilink/bot/sendmessage"), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(botToken))
	req.Header.Set("AuthorizationType", "ilink_bot_token")
	req.Header.Set("X-WECHAT-UIN", wechatUIN)

	var resp ClawBotSendMessageResponse
	if err := c.doJSON(req, &resp); err != nil {
		return err
	}
	if resp.Ret != 0 {
		return fmt.Errorf("clawbot sendmessage ret=%d", resp.Ret)
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
	if imageItem == nil || imageItem.Media == nil {
		return nil, fmt.Errorf("clawbot image item is empty")
	}

	media := imageItem.Media
	imageURL := strings.TrimSpace(media.FullURL)
	if imageURL == "" {
		encryptQueryParam := strings.TrimSpace(media.EncryptQueryParam)
		if encryptQueryParam == "" {
			return nil, fmt.Errorf("clawbot image media has no download url")
		}
		imageURL = buildClawBotCDNDownloadURL(defaultClawBotCDNBaseURL, encryptQueryParam)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = resp.Status
		}
		return nil, fmt.Errorf("clawbot image download http %d: %s", resp.StatusCode, message)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, clawBotMediaMaxBytes))
	if err != nil {
		return nil, err
	}

	key, err := parseClawBotImageAESKey(imageItem)
	if err != nil {
		return nil, err
	}
	if len(key) == 0 {
		return data, nil
	}
	return decryptClawBotAESECB(data, key)
}

func buildClawBotURL(baseURL, endpoint string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	return baseURL + endpoint
}

func buildClawBotCDNDownloadURL(baseURL, encryptedQueryParam string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	return baseURL + "/download?encrypted_query_param=" + url.QueryEscape(strings.TrimSpace(encryptedQueryParam))
}

func (c *ClawBotClient) doJSON(req *http.Request, dst interface{}) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = resp.Status
		}
		return fmt.Errorf("clawbot http %d: %s", resp.StatusCode, message)
	}

	if dst == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(dst)
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
