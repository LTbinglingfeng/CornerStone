package storage

import (
	"cornerstone/client"
	"encoding/json"
	"testing"
)

func TestProviderStateDurableAndEditInvalidation(t *testing.T) {
	dir := t.TempDir()
	cm := NewChatManager(dir)
	if _, err := cm.CreateSession("state", "title", "prompt", "name"); err != nil {
		t.Fatal(err)
	}
	state := &client.ProviderState{Provider: "gemini", Model: "model", Payload: json.RawMessage(`[{"text":"answer","thoughtSignature":"c2ln"}]`)}
	if err := cm.AddMessages("state", []ChatMessage{{Role: "assistant", Content: "answer", ProviderState: state}}); err != nil {
		t.Fatal(err)
	}
	state.Payload[0] = 'x'
	cm = NewChatManager(dir)
	record, ok := cm.GetSession("state")
	if !ok || record.Messages[0].ProviderState == nil || record.Messages[0].ProviderState.Payload[0] != '[' {
		t.Fatal("state not durably preserved")
	}
	record.Messages[0].ProviderState.Payload[0] = 'x'
	again, _ := cm.GetSession("state")
	if again.Messages[0].ProviderState.Payload[0] != '[' {
		t.Fatal("snapshot aliases storage")
	}
	if err := cm.UpdateMessageContentByIndex("state", 0, "edited"); err != nil {
		t.Fatal(err)
	}
	cm = NewChatManager(dir)
	record, _ = cm.GetSession("state")
	if record.Messages[0].ProviderState != nil {
		t.Fatal("edited message kept stale state")
	}
}
