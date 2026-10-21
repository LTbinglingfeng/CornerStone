package client

import (
	"encoding/json"
	"fmt"
	"strings"
)

// validateResponsesStatus runs before a response can be converted into an
// assistant message or executable tool calls. Missing JSON status is tolerated
// for compatible gateways; an explicit non-success status never is.
func validateResponsesStatus(raw json.RawMessage, terminalStatus string) error {
	var status struct {
		Status            string                `json:"status"`
		Error             *openAIResponsesError `json:"error"`
		IncompleteDetails *struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &status); err != nil {
			return fmt.Errorf("decode response status: %w", err)
		}
	}
	if status.Error != nil {
		return fmt.Errorf("API error (%s): %s", status.Error.Code, status.Error.Message)
	}
	state := strings.TrimSpace(status.Status)
	if state == "" {
		state = terminalStatus
	}
	// The event type is authoritative even if a broken gateway attaches a
	// conflicting nested status to a failed/incomplete event.
	if terminalStatus == "failed" || terminalStatus == "incomplete" {
		state = terminalStatus
	}
	if state == "" || state == "completed" {
		return nil
	}
	if status.IncompleteDetails != nil && status.IncompleteDetails.Reason != "" {
		return fmt.Errorf("Responses response %s: %s", state, status.IncompleteDetails.Reason)
	}
	return fmt.Errorf("Responses response %s", state)
}

func validateResponsesTerminalEvent(data []byte, eventType string) error {
	var event struct {
		Response json.RawMessage `json:"response"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return fmt.Errorf("decode terminal response: %w", err)
	}
	return validateResponsesStatus(event.Response, strings.TrimPrefix(eventType, "response."))
}
