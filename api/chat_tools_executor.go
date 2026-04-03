package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/logging"
	"cornerstone/storage"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type chatToolResult struct {
	OK    bool        `json:"ok"`
	Tool  string      `json:"tool"`
	Data  interface{} `json:"data"`
	Error string      `json:"error"`
}

func marshalChatToolResult(result chatToolResult) string {
	// Tool result must always be a structured JSON string.
	data, errMarshal := json.Marshal(result)
	if errMarshal != nil {
		fallback := fmt.Sprintf(`{"ok":false,"tool":%q,"data":null,"error":%q}`, result.Tool, "marshal tool result failed")
		return fallback
	}
	return string(data)
}

type chatToolContext struct {
	SessionID  string
	PromptID   string
	PromptName string
}

type chatToolHandler func(ctx context.Context, toolCall client.ToolCall, toolCtx chatToolContext) chatToolResult

type chatToolExecutor struct {
	handlers map[string]chatToolHandler

	momentManager   *storage.MomentManager
	momentGenerator *MomentGenerator
}

func newChatToolExecutor(momentManager *storage.MomentManager, momentGenerator *MomentGenerator) *chatToolExecutor {
	executor := &chatToolExecutor{
		handlers:        make(map[string]chatToolHandler),
		momentManager:   momentManager,
		momentGenerator: momentGenerator,
	}

	executor.handlers["send_red_packet"] = executor.handleSendRedPacket
	executor.handlers["red_packet_received"] = executor.handleRedPacketReceived
	executor.handlers["send_pat"] = executor.handleSendPat
	executor.handlers["generate_moment"] = executor.handleGenerateMoment

	return executor
}

func (e *chatToolExecutor) Execute(ctx context.Context, toolCall client.ToolCall, toolCtx chatToolContext) string {
	toolName := strings.TrimSpace(toolCall.Function.Name)
	if toolName == "" {
		return marshalChatToolResult(chatToolResult{
			OK:    false,
			Tool:  "",
			Data:  nil,
			Error: "missing tool name",
		})
	}

	handler, ok := e.handlers[toolName]
	if !ok {
		return marshalChatToolResult(chatToolResult{
			OK:    false,
			Tool:  toolName,
			Data:  nil,
			Error: "unsupported tool",
		})
	}

	result := handler(ctx, toolCall, toolCtx)
	// Ensure required fields always present.
	result.Tool = toolName
	if result.OK {
		result.Error = ""
	}
	return marshalChatToolResult(result)
}

type chatToolSendRedPacketArgs struct {
	Amount  float64 `json:"amount"`
	Message string  `json:"message"`
}

func (e *chatToolExecutor) handleSendRedPacket(ctx context.Context, toolCall client.ToolCall, toolCtx chatToolContext) chatToolResult {
	var args chatToolSendRedPacketArgs
	if errUnmarshal := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); errUnmarshal != nil {
		return chatToolResult{OK: false, Data: nil, Error: "invalid arguments"}
	}
	if args.Amount <= 0 {
		return chatToolResult{OK: false, Data: nil, Error: "amount must be > 0"}
	}
	if strings.TrimSpace(args.Message) == "" {
		return chatToolResult{OK: false, Data: nil, Error: "message is required"}
	}
	packetKey := normalizePacketKey(toolCall.ID)
	if packetKey == "" {
		packetKey = "unknown"
	}
	return chatToolResult{
		OK: true,
		Data: map[string]interface{}{
			"amount":     args.Amount,
			"message":    strings.TrimSpace(args.Message),
			"packet_key": packetKey,
		},
	}
}

type chatToolSendPatArgs struct {
	Name   string `json:"name"`
	Target string `json:"target"`
}

func (e *chatToolExecutor) handleSendPat(ctx context.Context, toolCall client.ToolCall, toolCtx chatToolContext) chatToolResult {
	var args chatToolSendPatArgs
	if errUnmarshal := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); errUnmarshal != nil {
		return chatToolResult{OK: false, Data: nil, Error: "invalid arguments"}
	}
	name := strings.TrimSpace(args.Name)
	target := strings.TrimSpace(args.Target)
	if name == "" {
		return chatToolResult{OK: false, Data: nil, Error: "name is required"}
	}
	if target == "" {
		return chatToolResult{OK: false, Data: nil, Error: "target is required"}
	}
	return chatToolResult{
		OK: true,
		Data: map[string]interface{}{
			"name":   name,
			"target": target,
		},
	}
}

type chatToolRedPacketReceivedArgs struct {
	PacketKey    string `json:"packet_key"`
	ReceiverName string `json:"receiver_name,omitempty"`
	SenderName   string `json:"sender_name,omitempty"`
}

func (e *chatToolExecutor) handleRedPacketReceived(ctx context.Context, toolCall client.ToolCall, toolCtx chatToolContext) chatToolResult {
	var args chatToolRedPacketReceivedArgs
	if errUnmarshal := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); errUnmarshal != nil {
		return chatToolResult{OK: false, Data: nil, Error: "invalid arguments"}
	}
	packetKey := strings.TrimSpace(args.PacketKey)
	if packetKey == "" {
		return chatToolResult{OK: false, Data: nil, Error: "packet_key is required"}
	}
	result := map[string]interface{}{
		"packet_key": packetKey,
	}
	if strings.TrimSpace(args.ReceiverName) != "" {
		result["receiver_name"] = strings.TrimSpace(args.ReceiverName)
	}
	if strings.TrimSpace(args.SenderName) != "" {
		result["sender_name"] = strings.TrimSpace(args.SenderName)
	}
	return chatToolResult{
		OK:   true,
		Data: result,
	}
}

type chatToolGenerateMomentArgs struct {
	Content       string `json:"content"`
	ImagePrompt   string `json:"image_prompt"`
	Prompt        string `json:"prompt"`
	ImagePromptV2 string `json:"imagePrompt"`
}

func (e *chatToolExecutor) handleGenerateMoment(ctx context.Context, toolCall client.ToolCall, toolCtx chatToolContext) chatToolResult {
	if e.momentManager == nil || e.momentGenerator == nil {
		return chatToolResult{OK: false, Data: nil, Error: "moment service not configured"}
	}
	promptID := strings.TrimSpace(toolCtx.PromptID)
	promptName := strings.TrimSpace(toolCtx.PromptName)
	if promptID == "" || promptName == "" {
		return chatToolResult{OK: false, Data: nil, Error: "missing prompt context"}
	}

	var args chatToolGenerateMomentArgs
	if errUnmarshal := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); errUnmarshal != nil {
		return chatToolResult{OK: false, Data: nil, Error: "invalid arguments"}
	}

	content := strings.TrimSpace(args.Content)
	imagePrompt := strings.TrimSpace(args.ImagePrompt)
	if imagePrompt == "" {
		imagePrompt = strings.TrimSpace(args.Prompt)
	}
	if imagePrompt == "" {
		imagePrompt = strings.TrimSpace(args.ImagePromptV2)
	}
	if content == "" || imagePrompt == "" {
		return chatToolResult{OK: false, Data: nil, Error: "missing fields"}
	}

	now := time.Now()
	moment := storage.Moment{
		ID:          uuid.NewString(),
		PromptID:    promptID,
		PromptName:  promptName,
		Content:     content,
		ImagePrompt: imagePrompt,
		Status:      storage.MomentStatusPending,
		CreatedAt:   now,
		UpdatedAt:   now,
		Likes:       []storage.Like{},
		Comments:    []storage.Comment{},
	}

	created, errCreate := e.momentManager.Create(moment)
	if errCreate != nil {
		logging.Errorf("generate_moment create moment failed: prompt_id=%s err=%v", promptID, errCreate)
		return chatToolResult{OK: false, Data: nil, Error: "create moment failed"}
	}

	e.momentGenerator.StartGeneration(created.ID)
	logging.Infof("moment created via tool: id=%s prompt_id=%s", created.ID, promptID)

	return chatToolResult{
		OK: true,
		Data: map[string]interface{}{
			"moment_id":   created.ID,
			"prompt_id":   promptID,
			"prompt_name": promptName,
			"status":      created.Status,
		},
	}
}
