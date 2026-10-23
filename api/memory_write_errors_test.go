package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cornerstone/client"
	"cornerstone/storage"
)

// Prime the cache, then replace the prompt directory with a regular file.
// This forces saves to fail deterministically, even when tests run as root.
func blockedMemoryWrites(t *testing.T, seed bool) (*storage.MemoryManager, string) {
	t.Helper()
	root := t.TempDir()
	id := "test-prompt"
	manager := storage.NewMemoryManager(root)
	if seed {
		if err := manager.Add(id, storage.Memory{Subject: "user", Category: "preference", Content: "喜欢茶"}); err != nil {
			t.Fatal(err)
		}
	} else if _, err := manager.Load(id); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, id)
	if seed {
		if err := os.Rename(dir, dir+"-saved"); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(dir, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	return manager, id
}

func TestWriteMemorySaveErrors(t *testing.T) {
	for _, seed := range []bool{false, true} {
		name := "add"
		if seed {
			name = "update"
		}
		t.Run(name, func(t *testing.T) {
			manager, id := blockedMemoryWrites(t, seed)
			executor := &chatToolExecutor{memoryManager: manager}
			args, err := json.Marshal(chatToolWriteMemoryArgs{Items: []chatToolWriteMemoryItem{
				{Subject: "user", Category: "preference", Content: "喜欢茶"},
				{Subject: "user", Category: "preference", Content: "喜欢茶"},
				{Subject: "user", Category: "preference", Content: "密码是示例"},
			}})
			if err != nil {
				t.Fatal(err)
			}
			result := executor.handleWriteMemory(context.Background(), client.ToolCall{Function: client.ToolCallFunction{Arguments: string(args)}}, chatToolContext{
				PromptID: id, MemSession: storage.NewMemorySession(id, "test-session", manager, nil),
			})
			if result.OK || !strings.Contains(result.Error, "不要盲目整批重试") {
				t.Fatalf("unexpected result: %+v", result)
			}
			data, ok := result.Data.(writeMemoryToolData)
			if !ok {
				t.Fatalf("unexpected data: %#v", result.Data)
			}
			if data.Added != 0 || data.Updated != 0 || data.Failed != 1 || data.Skipped != 2 || data.Blocked != 1 {
				t.Fatalf("unexpected counts: %+v", data)
			}
			if len(data.Items) != 3 || !strings.Contains(data.Items[0].Reason, "保存失败") {
				t.Fatalf("missing item results: %+v", data)
			}
		})
	}
}

func TestMemoryImportSaveErrors(t *testing.T) {
	for _, mode := range []string{"merge", "replace"} {
		t.Run(mode, func(t *testing.T) {
			manager, id := blockedMemoryWrites(t, mode == "replace")
			h := &Handler{memoryManager: manager}
			body, err := json.Marshal(MemoryImportRequest{Mode: mode, Memories: []MemoryExportItem{
				{Subject: "user", Category: "preference", Content: "喜欢咖啡"},
				{Subject: "invalid", Category: "preference", Content: "无效条目"},
				{Subject: "user", Category: "preference", Content: "喜欢阅读"},
			}})
			if err != nil {
				t.Fatal(err)
			}
			recorder := httptest.NewRecorder()
			h.handleMemoryImport(recorder, httptest.NewRequest(http.MethodPost, "/api/memory/"+id+"/import", bytes.NewReader(body)), id)
			var response struct {
				Success bool   `json:"success"`
				Error   string `json:"error"`
				Data    struct {
					Added        int    `json:"added"`
					Invalid      int    `json:"invalid"`
					Failed       int    `json:"failed"`
					Deleted      int    `json:"deleted"`
					DeleteFailed int    `json:"delete_failed"`
					Unprocessed  int    `json:"unprocessed"`
					Mode         string `json:"mode"`
				} `json:"data"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if recorder.Code != http.StatusInternalServerError || response.Success || !strings.Contains(response.Error, "not a batch transaction") {
				t.Fatalf("unexpected response: %d %s", recorder.Code, recorder.Body.String())
			}
			data := response.Data
			if data.Added != 0 || data.Deleted != 0 || data.Mode != mode {
				t.Fatalf("unexpected counts: %+v", data)
			}
			if mode == "merge" {
				if data.Failed != 2 || data.Invalid != 1 || data.Unprocessed != 0 || data.DeleteFailed != 0 {
					t.Fatalf("unexpected merge counts: %+v", data)
				}
			} else {
				// No input items may be processed after the first deletion fails.
				if data.Failed != 0 || data.Invalid != 0 || data.Unprocessed != 3 || data.DeleteFailed != 1 {
					t.Fatalf("replace continued after delete failure: %+v", data)
				}
			}
		})
	}
}
