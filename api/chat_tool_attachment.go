package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/storage"
	"encoding/json"
	"mime"
	"path"
	"strings"
	"unicode"
)

const sendAttachmentToolName = "send_attachment"

// Paths are opaque local media references, not URLs or arbitrary filesystem paths.
// /api/attachments/<id> is the attachment store's canonical local reference.
func validToolAttachmentPath(value string) bool {
	if value == "" || strings.ContainsAny(value, "\\:%?#") || strings.IndexFunc(value, unicode.IsControl) >= 0 || path.Clean(value) != value {
		return false
	}
	for _, prefix := range []string{"cache_photo/", "tts_audio/", storage.AttachmentURLPrefix} {
		if strings.HasPrefix(value, prefix) {
			name := strings.TrimPrefix(value, prefix)
			return name != "" && name != "." && name != ".." && !strings.Contains(name, "/")
		}
	}
	return false
}

func addToolAttachment(allowed map[string]storage.ChatAttachment, a storage.ChatAttachment) {
	if !validToolAttachmentPath(a.Path) {
		return
	}
	switch a.Kind {
	case "image", "audio", "video", "file":
	default:
		return
	}
	if a.Name == "" {
		a.Name = path.Base(a.Path)
	}
	if a.MIMEType == "" {
		a.MIMEType = mime.TypeByExtension(path.Ext(a.Name))
		if a.MIMEType == "" {
			a.MIMEType = "application/octet-stream"
		}
	}
	if a.Size < 0 {
		return
	}
	allowed[a.Path] = a
}

// Explicit successful tool metadata is the only tool-output source; prose and URLs
// in tool output (including search results) never grant file access.
func attachmentFromToolResult(content string) (storage.ChatAttachment, bool) {
	var result struct {
		OK   bool `json:"ok"`
		Data struct {
			Attachment *storage.ChatAttachment `json:"attachment"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(content), &result) != nil || !result.OK || result.Data.Attachment == nil {
		return storage.ChatAttachment{}, false
	}
	return *result.Data.Attachment, true
}

func sessionToolAttachments(messages []storage.ChatMessage) map[string]storage.ChatAttachment {
	allowed := make(map[string]storage.ChatAttachment)
	for _, msg := range messages {
		for _, a := range msg.Attachments {
			addToolAttachment(allowed, a)
		}
		for _, p := range msg.ImagePaths {
			addToolAttachment(allowed, storage.ChatAttachment{Path: p, Kind: "image"})
		}
		for _, p := range msg.TTSAudioPaths {
			addToolAttachment(allowed, storage.ChatAttachment{Path: p, Kind: "audio"})
		}
		if msg.Role == "tool" && msg.ToolCallID != "" {
			if a, ok := attachmentFromToolResult(msg.Content); ok {
				addToolAttachment(allowed, a)
			}
		}
	}
	return allowed
}

func (e *chatToolExecutor) handleSendAttachment(ctx context.Context, call client.ToolCall, toolCtx chatToolContext) chatToolResult {
	if toolCtx.Channel != chatToolChannelClawBot || (toolCtx.AllowedToolNames != nil && !toolCtx.AllowedToolNames[sendAttachmentToolName]) {
		return chatToolResult{Error: "send_attachment is disabled or unavailable on this channel"}
	}
	var args struct {
		Path string `json:"path"`
	}
	if decodeToolArguments(call.Function.Arguments, &args) != nil || !validToolAttachmentPath(args.Path) {
		return chatToolResult{Error: "invalid attachment path"}
	}
	a, ok := toolCtx.Attachments[args.Path]
	if !ok {
		return chatToolResult{Error: "attachment is not registered in the current session"}
	}
	// No disk/network access and no delivery side effects: the delivery queue consumes
	// this intent after persistence, keyed by the tool call ID.
	return chatToolResult{OK: true, Data: map[string]interface{}{"attachment": a}}
}

func sendAttachmentTool() client.Tool {
	return client.Tool{Type: "function", Function: client.ToolFunction{Name: sendAttachmentToolName, Description: "将当前会话已登记的附件、图片或语音加入微信投递队列。仅使用附件清单或成功工具产物 metadata 中的原始 path，不支持任意磁盘路径或 URL。成功仅表示生成投递意图，不表示已送达；不要重复调用同一附件。", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"path": map[string]interface{}{"type": "string", "description": "当前会话附件的原始本地引用路径"}}, "required": []string{"path"}, "additionalProperties": false}}}
}
