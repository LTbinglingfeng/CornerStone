package client

import (
	"encoding/json"
	"google.golang.org/genai"
)

// modelUsageError preserves reported billing metadata without exposing failed
// response output as an executable ChatResponse. Error text and unwrapping stay
// identical to the underlying provider failure.
type modelUsageError struct {
	err   error
	model string
	usage Usage
}

func (e *modelUsageError) Error() string { return e.err.Error() }
func (e *modelUsageError) Unwrap() error { return e.err }

func withModelUsage(err error, model string, usage Usage) error {
	if err == nil || !usage.Present {
		return err
	}
	return &modelUsageError{err: err, model: model, usage: usage}
}

// numericUsageFields rejects empty/null containers and absent counts rather
// than treating unreported usage as explicitly reported zero usage.
func numericUsageFields(data []byte, keys ...string) bool {
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil {
		return false
	}
	for _, key := range keys {
		var value *int
		if json.Unmarshal(raw[key], &value) != nil || value == nil {
			return false
		}
	}
	return true
}

// UnmarshalJSON distinguishes absent/null usage from an explicitly reported zero.
func (u *Usage) UnmarshalJSON(data []byte) error {
	type plain Usage
	var v plain
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	v.Present = numericUsageFields(data, "prompt_tokens", "completion_tokens", "total_tokens")
	var details struct {
		Prompt struct {
			Cached int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
		Completion struct {
			Reasoning int `json:"reasoning_tokens"`
		} `json:"completion_tokens_details"`
	}
	if err := json.Unmarshal(data, &details); err != nil {
		return err
	}
	if details.Prompt.Cached != 0 {
		v.CacheReadInputTokens = details.Prompt.Cached
	}
	if details.Completion.Reasoning != 0 {
		v.ReasoningTokens = details.Completion.Reasoning
	}
	*u = Usage(v)
	return nil
}

func (u *openAIResponsesUsage) UnmarshalJSON(data []byte) error {
	type plain openAIResponsesUsage
	var v plain
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	v.Present = numericUsageFields(data, "input_tokens", "output_tokens", "total_tokens")
	*u = openAIResponsesUsage(v)
	return nil
}

func usagePointer(u Usage) *Usage {
	if !u.Present {
		return nil
	}
	return &u
}

func responsesUsage(u openAIResponsesUsage) Usage {
	return Usage{Present: u.Present, PromptTokens: u.InputTokens, CompletionTokens: u.OutputTokens, TotalTokens: u.TotalTokens, CacheReadInputTokens: u.InputTokensDetails.CachedTokens, ReasoningTokens: u.OutputTokensDetails.ReasoningTokens}
}

// Anthropic deltas are cumulative snapshots with optional input fields. Merge
// counts and their presence so an output-only delta retains the initial input.
func (u *AnthropicUsage) UnmarshalJSON(data []byte) error {
	type plain AnthropicUsage
	v := plain(*u)
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	v.decoded = true
	v.inputReported = v.inputReported || numericUsageFields(data, "input_tokens")
	v.outputReported = v.outputReported || numericUsageFields(data, "output_tokens")
	*u = AnthropicUsage(v)
	return nil
}

func anthropicUsage(u *AnthropicUsage) Usage {
	if u == nil {
		return Usage{}
	}
	return Usage{Present: !u.decoded || (u.inputReported && u.outputReported), InputTokensExcludeCache: true, PromptTokens: u.InputTokens, CompletionTokens: u.OutputTokens, TotalTokens: u.InputTokens + u.OutputTokens, CacheCreationInputTokens: u.CacheCreationInputTokens, CacheReadInputTokens: u.CacheReadInputTokens}
}

// genai v1.75.0 continuation.go merges usage across hops, retaining the latest
// cumulative snapshot within each hop. Never sum streamed snapshots again.
func geminiUsage(u *genai.GenerateContentResponseUsageMetadata) Usage {
	if u == nil {
		return Usage{}
	}
	return Usage{Present: true, PromptTokens: int(u.PromptTokenCount), CompletionTokens: int(u.CandidatesTokenCount), TotalTokens: int(u.TotalTokenCount), CacheReadInputTokens: int(u.CachedContentTokenCount), ReasoningTokens: int(u.ThoughtsTokenCount)}
}
