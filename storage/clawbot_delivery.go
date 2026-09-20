package storage

import (
	"cornerstone/client"
	"encoding/json"
	"time"
)

// Delivery is kept in the same atomic snapshot as the receive cursor. Receipts
// are never evicted automatically: hitting the file limit applies backpressure
// instead of silently forgetting deduplication or unprocessed messages.
type ClawBotDeliveryState struct {
	Jobs           []ClawBotDeliveryJob `json:"jobs"`
	Receipts       map[string]string    `json:"receipts"`
	Origins        map[string]string    `json:"origins"`
	Finished       map[string]string    `json:"finished"`
	Actions        map[string]string    `json:"actions,omitempty"`
	ActiveSessions map[string]string    `json:"active_sessions,omitempty"`
}

// ClawBotDeliveryMedia separates immutable local bytes and the CDN upload
// receipt from acknowledgement of the message containing the media reference.
type ClawBotDeliveryMedia struct {
	Kind         string                       `json:"kind"`
	Name         string                       `json:"name"`
	MIMEType     string                       `json:"mime_type"`
	Size         int64                        `json:"size"`
	SourcePath   string                       `json:"source_path,omitempty"`
	HistoryPath  string                       `json:"history_path,omitempty"`
	BlobID       string                       `json:"blob_id,omitempty"`
	UploadStatus string                       `json:"upload_status,omitempty"`
	Uploaded     *client.ClawBotUploadedMedia `json:"uploaded,omitempty"`
}

type ClawBotDeliveryChunk struct {
	Media *ClawBotDeliveryMedia `json:"media,omitempty"`
	// MessageIndex identifies the persisted artifact source; absent in old jobs.
	MessageIndex       *int   `json:"message_index,omitempty"`
	ClientID           string `json:"client_id"`
	Text               string `json:"text"`
	Status             string `json:"status"`
	MessageID          string `json:"message_id,omitempty"`
	ManualConfirmation bool   `json:"manual_confirmation,omitempty"`
}

type ClawBotDeliveryJob struct {
	Revision         uint64                          `json:"revision"`
	MemoryPromptID   string                          `json:"memory_prompt_id,omitempty"`
	MemoryNotified   bool                            `json:"memory_notified,omitempty"`
	ID               string                          `json:"id"`
	Kind             string                          `json:"kind"`
	UserID           string                          `json:"user_id"`
	SessionID        string                          `json:"session_id,omitempty"`
	Status           string                          `json:"status"`
	Phase            string                          `json:"phase,omitempty"`
	CreatedAt        time.Time                       `json:"created_at"`
	UpdatedAt        time.Time                       `json:"updated_at"`
	ReadyAt          time.Time                       `json:"ready_at"`
	NextAttemptAt    time.Time                       `json:"next_attempt_at,omitempty"`
	Attempts         int                             `json:"attempts,omitempty"`
	LastError        string                          `json:"last_error,omitempty"`
	InputTimes       []time.Time                     `json:"input_times,omitempty"`
	Inputs           []client.ClawBotIncomingMessage `json:"inputs,omitempty"`
	InputMessages    []ChatMessage                   `json:"input_messages,omitempty"`
	Messages         []ChatMessage                   `json:"messages,omitempty"`
	Replace          bool                            `json:"replace,omitempty"`
	ExpectedTail     []ChatMessage                   `json:"expected_tail,omitempty"`
	Chunks           []ClawBotDeliveryChunk          `json:"chunks"`
	NextChunk        int                             `json:"next_chunk"`
	CancelRequested  bool                            `json:"cancel_requested,omitempty"`
	HistoryCommitted bool                            `json:"history_committed,omitempty"`
	HistoryChunks    int                             `json:"history_chunks,omitempty"`
	HistoryMessages  []ChatMessage                   `json:"history_messages,omitempty"`
	Reminder         *Reminder                       `json:"reminder,omitempty"`
	Greeting         *IdleGreetingTask               `json:"greeting,omitempty"`
}

func CloneClawBotState(state ClawBotState) (ClawBotState, error) {
	data, err := json.Marshal(state)
	if err != nil {
		return ClawBotState{}, err
	}
	var clone ClawBotState
	err = json.Unmarshal(data, &clone)
	return clone, err
}
