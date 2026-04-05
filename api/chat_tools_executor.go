package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/config"
	"cornerstone/internal/search"
	"cornerstone/storage"
	"encoding/json"
	"fmt"
	"strings"
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
	SessionID        string
	PromptID         string
	PromptName       string
	Channel          chatToolChannel
	Target           storage.ReminderTarget
	MemSession       *storage.MemorySession
	AllowedToolNames map[string]bool
}

type chatToolHandler func(ctx context.Context, toolCall client.ToolCall, toolCtx chatToolContext) chatToolResult

type chatToolExecutor struct {
	handlers map[string]chatToolHandler

	memoryManager    *storage.MemoryManager
	configManager    *config.Manager
	weatherService   weatherService
	exactTimeService exactTimeProvider
	webSearch        *search.Orchestrator
	reminderService  *ReminderService
	emitEvent        func(payload interface{})
}

func newChatToolExecutor() *chatToolExecutor {
	executor := &chatToolExecutor{
		handlers: make(map[string]chatToolHandler),
	}

	executor.handlers["send_red_packet"] = executor.handleSendRedPacket
	executor.handlers["red_packet_received"] = executor.handleRedPacketReceived
	executor.handlers["send_pat"] = executor.handleSendPat
	executor.handlers["no_reply"] = executor.handleNoReply
	executor.handlers["get_weather"] = executor.handleGetWeather
	executor.handlers["get_time"] = executor.handleGetTime
	executor.handlers["web_search"] = executor.handleWebSearch
	executor.handlers["write_memory"] = executor.handleWriteMemory
	executor.handlers["schedule_reminder"] = executor.handleScheduleReminder

	return executor
}

func (e *chatToolExecutor) ExecuteResult(ctx context.Context, toolCall client.ToolCall, toolCtx chatToolContext) chatToolResult {
	toolName := strings.TrimSpace(toolCall.Function.Name)
	if toolName == "" {
		return chatToolResult{
			OK:    false,
			Tool:  "",
			Data:  nil,
			Error: "missing tool name",
		}
	}

	handler, ok := e.handlers[toolName]
	if !ok {
		return chatToolResult{
			OK:    false,
			Tool:  toolName,
			Data:  nil,
			Error: "unsupported tool",
		}
	}

	result := handler(ctx, toolCall, toolCtx)
	result.Tool = toolName
	if result.OK {
		result.Error = ""
	}
	return result
}

func (e *chatToolExecutor) Execute(ctx context.Context, toolCall client.ToolCall, toolCtx chatToolContext) string {
	return marshalChatToolResult(e.ExecuteResult(ctx, toolCall, toolCtx))
}

func decodeToolArguments(arguments string, dst interface{}) error {
	trimmed := strings.TrimSpace(arguments)
	if trimmed == "" {
		trimmed = `{}`
	}
	return json.Unmarshal([]byte(trimmed), dst)
}

type chatToolSendRedPacketArgs struct {
	Amount  float64 `json:"amount"`
	Message string  `json:"message"`
}

func (e *chatToolExecutor) handleSendRedPacket(ctx context.Context, toolCall client.ToolCall, toolCtx chatToolContext) chatToolResult {
	var args chatToolSendRedPacketArgs
	if errUnmarshal := decodeToolArguments(toolCall.Function.Arguments, &args); errUnmarshal != nil {
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
	if errUnmarshal := decodeToolArguments(toolCall.Function.Arguments, &args); errUnmarshal != nil {
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

type chatToolNoReplyArgs struct {
	Reason          string   `json:"reason,omitempty"`
	CooldownSeconds *float64 `json:"cooldown_seconds,omitempty"`
}

func (e *chatToolExecutor) handleNoReply(ctx context.Context, toolCall client.ToolCall, toolCtx chatToolContext) chatToolResult {
	result := map[string]interface{}{}
	var args chatToolNoReplyArgs
	// no_reply is a "silent end" signal. It should be best-effort and never fail.
	if errUnmarshal := decodeToolArguments(toolCall.Function.Arguments, &args); errUnmarshal == nil {
		if reason := strings.TrimSpace(args.Reason); reason != "" {
			result["reason"] = reason
		}
		if args.CooldownSeconds != nil {
			cooldownSeconds := *args.CooldownSeconds
			if cooldownSeconds < 0 {
				cooldownSeconds = 0
			}
			result["cooldown_seconds"] = cooldownSeconds
		}
	}

	return chatToolResult{
		OK:   true,
		Data: result,
	}
}

type chatToolRedPacketReceivedArgs struct {
	PacketKey    string `json:"packet_key"`
	ReceiverName string `json:"receiver_name,omitempty"`
	SenderName   string `json:"sender_name,omitempty"`
}

func (e *chatToolExecutor) handleRedPacketReceived(ctx context.Context, toolCall client.ToolCall, toolCtx chatToolContext) chatToolResult {
	var args chatToolRedPacketReceivedArgs
	if errUnmarshal := decodeToolArguments(toolCall.Function.Arguments, &args); errUnmarshal != nil {
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
