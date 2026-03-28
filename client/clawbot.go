package client

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
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

type ClawBotIncomingMessageItem struct {
	Type      int              `json:"type"`
	TextItem  *ClawBotItemText `json:"text_item,omitempty"`
	VoiceItem *ClawBotItemText `json:"voice_item,omitempty"`
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

func buildClawBotURL(baseURL, endpoint string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	return baseURL + endpoint
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
