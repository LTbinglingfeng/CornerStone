package client

import (
	"context"
	"cornerstone/logging"
	"errors"
	"sync"
	"time"
)

// CallStats describes completed calls, including failures. UsageCalls counts
// reported usage (including explicit zero). 0 < UsageCalls < ModelCalls means
// partial coverage; Usage is the sum of only the reported calls.
type CallStats struct {
	ModelCalls int
	UsageCalls int
	Usage      Usage
	DurationMS int64
}

type ObservedClient struct {
	upstream AIClient
	mu       sync.Mutex
	stats    CallStats
	nextCall int
}

var _ AIClient = (*ObservedClient)(nil)

// Observe is opt-in and does not alter requests or install provider context.
func Observe(upstream AIClient) *ObservedClient { return &ObservedClient{upstream: upstream} }

func (c *ObservedClient) callContext(ctx context.Context) context.Context {
	c.mu.Lock()
	c.nextCall++
	call := c.nextCall
	c.mu.Unlock()
	return logging.WithFields(ctx, "model_call", call)
}

func (c *ObservedClient) Snapshot() CallStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stats
}

func (c *ObservedClient) complete(ctx context.Context, req ChatRequest, model string, stream bool, start time.Time, firstText *int64, usage Usage, err error) {
	duration := time.Since(start).Milliseconds()
	// Anthropic input_tokens excludes both cache categories. Other providers
	// include cache reads in input_tokens; normalize only this provider's logs.
	if usage.InputTokensExcludeCache {
		cached := usage.CacheCreationInputTokens + usage.CacheReadInputTokens
		usage.PromptTokens += cached
		usage.TotalTokens += cached
		usage.InputTokensExcludeCache = false
	}
	c.mu.Lock()
	c.stats.ModelCalls++
	c.stats.DurationMS += duration
	if usage.Present {
		c.stats.UsageCalls++
		c.stats.Usage.Present = true
		c.stats.Usage.PromptTokens += usage.PromptTokens
		c.stats.Usage.CompletionTokens += usage.CompletionTokens
		c.stats.Usage.TotalTokens += usage.TotalTokens
		c.stats.Usage.CacheCreationInputTokens += usage.CacheCreationInputTokens
		c.stats.Usage.CacheReadInputTokens += usage.CacheReadInputTokens
		c.stats.Usage.ReasoningTokens += usage.ReasoningTokens
	}
	c.mu.Unlock()
	status := "success"
	if err != nil {
		status = "error"
	}
	availability := "unavailable"
	if usage.Present {
		availability = "reported"
	}
	fields := []any{"model", model, "requested_model", req.Model, "stream", stream, "duration_ms", duration, "status", status, "error_kind", logging.ErrorKind(err), "usage", availability}
	if usage.Present {
		fields = append(fields, "input_tokens", usage.PromptTokens, "output_tokens", usage.CompletionTokens, "total_tokens", usage.TotalTokens,
			"cache_creation_input_tokens", usage.CacheCreationInputTokens, "cache_read_input_tokens", usage.CacheReadInputTokens, "reasoning_tokens", usage.ReasoningTokens)
	}
	if firstText != nil {
		fields = append(fields, "first_text_ms", *firstText)
	}
	logging.Event(ctx, "model.completed", fields...)
}

func (c *ObservedClient) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	ctx = c.callContext(ctx)
	start := time.Now()
	logging.Event(ctx, "model.started", "model", req.Model, "requested_model", req.Model, "stream", false)
	resp, err := c.upstream.Chat(ctx, req)
	model := req.Model
	var usage Usage
	var failure *modelUsageError
	if errors.As(err, &failure) {
		usage = failure.usage
		if failure.model != "" {
			model = failure.model
		}
	}
	if resp != nil {
		usage = resp.Usage
		if resp.Model != "" {
			model = resp.Model
		}
	}
	c.complete(ctx, req, model, false, start, nil, usage, err)
	return resp, err
}

func (c *ObservedClient) ChatStream(ctx context.Context, req ChatRequest, callback func(StreamChunk) error) error {
	ctx = c.callContext(ctx)
	start := time.Now()
	logging.Event(ctx, "model.started", "model", req.Model, "requested_model", req.Model, "stream", true)
	model := req.Model
	var usage Usage
	var firstText *int64
	// Protect observation state even for clients that invoke callbacks concurrently.
	var mu sync.Mutex
	err := c.upstream.ChatStream(ctx, req, func(chunk StreamChunk) error {
		mu.Lock()
		if chunk.Model != "" {
			model = chunk.Model
		}
		if chunk.Usage != nil && chunk.Usage.Present {
			usage = *chunk.Usage
		}
		if firstText == nil {
			for _, choice := range chunk.Choices {
				if choice.Delta.Content != "" {
					elapsed := time.Since(start).Milliseconds()
					firstText = &elapsed
					break
				}
			}
		}
		mu.Unlock()
		return callback(chunk)
	})
	mu.Lock()
	c.complete(ctx, req, model, true, start, firstText, usage, err)
	mu.Unlock()
	return err
}
