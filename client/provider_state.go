package client

import "encoding/json"

// ProviderState is opaque, provider/model-scoped continuation data. Never include
// it in generic Chat Completions wire messages. Stream deltas carry snapshots.
type ProviderState struct {
	Provider string          `json:"provider"`
	Model    string          `json:"model,omitempty"`
	Payload  json.RawMessage `json:"payload"`
}

func CloneProviderState(state *ProviderState) *ProviderState {
	if state == nil {
		return nil
	}
	cloned := *state
	cloned.Payload = append(json.RawMessage(nil), state.Payload...)
	return &cloned
}

func (state *ProviderState) Matches(provider, model string) bool {
	return state != nil && state.Provider == provider && (model == "" || state.Model == model)
}

func CloneMessage(message Message) Message {
	message.ProviderState = CloneProviderState(message.ProviderState)
	message.ToolCalls = append([]ToolCall(nil), message.ToolCalls...)
	message.ImagePaths = append([]string(nil), message.ImagePaths...)
	message.TTSAudioPaths = append([]string(nil), message.TTSAudioPaths...)
	return message
}
