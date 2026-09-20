package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClawBotMediaSendRedactsRequestSecrets(t *testing.T) {
	const key = "a-secret-key-without-field-label"
	const param = "a-secret-download-query"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Msg struct {
				ItemList []ClawBotIncomingMessageItem `json:"item_list"`
			} `json:"msg"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Msg.ItemList) != 1 {
			t.Error("bad media request")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ret": 1, "errmsg": "invalid values: " + key + " " + param})
	}))
	defer server.Close()
	c := NewClawBotClient()
	_, err := c.SendItemMessageWithID(context.Background(), server.URL, "bot", "uin", "user", "context", ClawBotIncomingMessageItem{Type: 4, FileItem: &ClawBotFileItem{FileName: "test.txt", Len: "12", Media: &ClawBotCDNMedia{AESKey: key, EncryptQueryParam: param, EncryptType: 1}}}, "fixed-id")
	if err == nil || strings.Contains(err.Error(), key) || strings.Contains(err.Error(), param) {
		t.Fatalf("secret leak: %v", err)
	}
}

func TestClawBotUploadRejectsUnsafeCDNURL(t *testing.T) {
	for _, endpoint := range []string{"http://cdn.example/upload", "https://127.0.0.1/upload", "https://localhost/upload", "https://user:pass@cdn.example/upload", "https://cdn.example/upload#fragment"} {
		t.Run(endpoint, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"upload_full_url": endpoint})
			}))
			defer server.Close()
			if _, err := NewClawBotClient().UploadMedia(context.Background(), server.URL, "bot", "uin", "user", 3, []byte("file")); err == nil {
				t.Fatal("unsafe URL accepted")
			}
		})
	}
}
