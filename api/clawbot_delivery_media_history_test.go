package api

import (
	"cornerstone/client"
	"cornerstone/storage"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func TestConfirmedClawBotMediaMessagesPreservesToolGroups(t *testing.T) {
	for _, finalText := range []string{"done", ""} {
		t.Run(fmt.Sprintf("final=%q", finalText), func(t *testing.T) {
			messages := []storage.ChatMessage{
				{Role: "assistant", ToolCalls: []client.ToolCall{toolCall("a", "send_attachment", `{}`), toolCall("b", "get_time", `{}`)}},
				{Role: "tool", ToolCallID: "a", Content: `{"ok":true,"tool":"send_attachment"}`, ImagePaths: []string{"cache_photo/first.png", "cache_photo/second.png"}},
				{Role: "tool", ToolCallID: "b", Content: `{"ok":true,"tool":"get_time"}`},
				{Role: "assistant", ToolCalls: []client.ToolCall{toolCall("c", "send_attachment", `{}`), toolCall("d", "send_attachment", `{}`)}},
				{Role: "tool", ToolCallID: "c", Content: `{"ok":true,"tool":"send_attachment"}`, Attachments: []storage.ChatAttachment{{Path: "attachments/document.txt", Name: "document.txt", Kind: "file", Size: 12}}},
				{Role: "tool", ToolCallID: "d", Content: `{"ok":true,"tool":"send_attachment"}`, ImagePaths: []string{"cache_photo/third.png"}},
			}
			if finalText != "" {
				messages = append(messages, storage.ChatMessage{Role: "assistant", Content: finalText})
			}
			job := storage.ClawBotDeliveryJob{
				Messages:  messages,
				Chunks:    buildClawBotDeliveryChunks("receipt-test", finalText, "", messages),
				CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			}
			for confirmed := 0; confirmed <= len(job.Chunks); confirmed++ {
				t.Run(fmt.Sprintf("confirmed=%d", confirmed), func(t *testing.T) {
					job.NextChunk = confirmed
					before, _ := json.Marshal(job)
					got := confirmedDeliveryMessages(job)
					after, _ := json.Marshal(job)
					if string(before) != string(after) {
						t.Fatal("history reconstruction mutated the delivery snapshot")
					}

					// Every tool-call group must remain directly followed by all of
					// its original results, even with several receipts per tool.
					for i, message := range got {
						for j, call := range message.ToolCalls {
							index := i + j + 1
							if index >= len(got) || got[index].Role != "tool" || got[index].ToolCallID != call.ID {
								t.Fatalf("receipt split tool group at call %s: %+v", call.ID, got)
							}
						}
					}

					var gotPaths, wantPaths []string
					for _, message := range got {
						if len(message.ImagePaths)+len(message.Attachments) > 0 && message.Role != "assistant" {
							t.Fatal("visible media must be represented by assistant receipts")
						}
						gotPaths = append(gotPaths, message.ImagePaths...)
						for _, attachment := range message.Attachments {
							gotPaths = append(gotPaths, attachment.Path)
						}
					}
					for _, chunk := range job.Chunks[:confirmed] {
						if chunk.Media != nil {
							wantPaths = append(wantPaths, chunk.Media.SourcePath)
						}
					}
					if !reflect.DeepEqual(gotPaths, wantPaths) {
						t.Fatalf("confirmed media order = %v, want %v", gotPaths, wantPaths)
					}

					// Exercise the next turn's actual replay repair: no duplicate
					// or synthetic result should be introduced for any tool.
					replay := ensureToolResultMessagesForReplay(convertChatMessages(got))
					for _, original := range messages {
						if original.Role != "tool" {
							continue
						}
						count := 0
						for _, message := range replay {
							if message.Role == "tool" && message.ToolCallID == original.ToolCallID {
								count++
								if message.Content != original.Content {
									t.Fatalf("tool %s result changed: %s", original.ToolCallID, message.Content)
								}
							}
						}
						if count != 1 {
							t.Fatalf("tool %s has %d replay results, want 1", original.ToolCallID, count)
						}
					}
				})
			}
		})
	}
}
