package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

// GetConfig obtains a user-scoped typing ticket. Tickets are opaque credentials.
func (c *ClawBotClient) GetConfig(ctx context.Context, baseURL, botToken, wechatUIN, userID, contextToken string) (string, error) {
	var response struct {
		TypingTicket string `json:"typing_ticket"`
	}
	err := c.typingRequest(ctx, baseURL, botToken, wechatUIN, "getconfig", struct {
		UserID       string          `json:"ilink_user_id"`
		ContextToken string          `json:"context_token,omitempty"`
		BaseInfo     ClawBotBaseInfo `json:"base_info"`
	}{userID, contextToken, c.baseInfo()}, &response)
	return response.TypingTicket, err
}

// SendTyping sets (1) or clears (2) the transient input indicator.
func (c *ClawBotClient) SendTyping(ctx context.Context, baseURL, botToken, wechatUIN, userID, ticket string, status int) error {
	if ticket == "" || userID == "" || (status != 1 && status != 2) {
		return errors.New("clawbot: invalid typing request")
	}
	return c.typingRequest(ctx, baseURL, botToken, wechatUIN, "sendtyping", struct {
		UserID   string          `json:"ilink_user_id"`
		Ticket   string          `json:"typing_ticket"`
		Status   int             `json:"status"`
		BaseInfo ClawBotBaseInfo `json:"base_info"`
	}{userID, ticket, status, c.baseInfo()}, nil)
}

func (c *ClawBotClient) typingRequest(ctx context.Context, baseURL, token, uin, operation string, payload, response interface{}) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	body, err := json.Marshal(payload)
	if err != nil {
		return errors.New("clawbot: invalid typing payload")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, buildClawBotURL(baseURL, "/ilink/bot/"+operation), bytes.NewReader(body))
	if err != nil {
		return errors.New("clawbot: invalid typing URL")
	}
	setClawBotHeaders(req, token, uin)
	// Upstream may echo tickets in arbitrary error text. Keep only structured
	// business codes, never return its message from these credential APIs.
	err = c.doJSON(req, response)
	var apiErr *ClawBotAPIError
	if errors.As(err, &apiErr) {
		return &ClawBotAPIError{Ret: apiErr.Ret, ErrCode: apiErr.ErrCode, Message: "typing request rejected"}
	}
	return err
}
