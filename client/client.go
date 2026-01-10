package client

import "context"

// AIClient 通用AI客户端接口
type AIClient interface {
	// Chat 发送聊天请求（非流式）
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
	// ChatStream 发送聊天请求（流式）
	ChatStream(ctx context.Context, req ChatRequest, callback func(chunk StreamChunk) error) error
}
