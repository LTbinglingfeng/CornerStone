package client

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReceiveMediaEncryptedTypes(t *testing.T) {
	key := []byte("0123456789abcdef")
	plain := []byte("original media bytes")
	encrypted := mustEncryptAESECBForTest(t, plain, key)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(encrypted) }))
	defer server.Close()
	c := NewClawBotClient()
	c.httpClient.Transport = clawBotProtocolTransport(func(r *http.Request) (*http.Response, error) {
		req, _ := http.NewRequestWithContext(r.Context(), "GET", server.URL, nil)
		return http.DefaultTransport.RoundTrip(req)
	})
	for _, typ := range []struct{ wire, kind string }{
		{`{"type":3,"voice_item":{"text":"transcription","encode_type":6,"media":MEDIA}}`, "audio"},
		{`{"type":4,"file_item":{"file_name":"notes.txt","len":"20","media":MEDIA}}`, "file"},
		{`{"type":5,"video_item":{"video_size":32,"media":MEDIA}}`, "video"},
	} {
		media, _ := json.Marshal(ClawBotCDNMedia{FullURL: "https://cdn.example.test/media", AESKey: base64.StdEncoding.EncodeToString(key), EncryptType: 1})
		var item ClawBotIncomingMessageItem
		if err := json.Unmarshal([]byte(strings.ReplaceAll(typ.wire, "MEDIA", string(media))), &item); err != nil {
			t.Fatal(err)
		}
		got, kind, _, err := c.DownloadMediaItem(context.Background(), item)
		if err != nil || kind != typ.kind || !bytes.Equal(got, plain) {
			t.Fatalf("%s: %q %s %v", typ.kind, got, kind, err)
		}
		if item.Type == 3 && ExtractTextFromClawBotMessage(ClawBotIncomingMessage{ItemList: []ClawBotIncomingMessageItem{item}}) != "transcription" {
			t.Fatal("lost transcript")
		}
	}
}
func TestReceiveMediaRejectsInvalidAndPrivateDownloads(t *testing.T) {
	c := NewClawBotClient()
	for _, raw := range []string{"http://example.com/x", "https://127.0.0.1/x", "https://[::1]/x", "https://169.254.169.254/latest", "https://user:secret@example.com/x", "file:///etc/passwd"} {
		if _, err := c.DownloadImageItem(context.Background(), &ClawBotImageItem{Media: &ClawBotCDNMedia{FullURL: raw}}); err == nil {
			t.Fatal(raw)
		}
	}
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))
	item := ClawBotIncomingMessageItem{Type: 4, FileItem: &ClawBotFileItem{FileName: "file.txt", Media: &ClawBotCDNMedia{FullURL: "https://cdn.example.test/secret", AESKey: key}}}
	for _, body := range [][]byte{nil, []byte("broken"), make([]byte, 16)} {
		c.httpClient.Transport = clawBotProtocolTransport(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body))}, nil
		})
		if _, _, _, err := c.DownloadMediaItem(context.Background(), item); err == nil {
			t.Fatal("accepted corrupt payload")
		}
	}
	c.httpClient.Transport = clawBotProtocolTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, ContentLength: clawBotMediaMaxBytes + 17, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("x"))}, nil
	})
	if _, _, _, err := c.DownloadMediaItem(context.Background(), item); err == nil {
		t.Fatal("accepted oversized payload")
	}
	item.FileItem.Len = "-1"
	if _, _, _, err := c.DownloadMediaItem(context.Background(), item); err == nil {
		t.Fatal("accepted negative size")
	}
}

func TestReceiveMediaIntegrityAndRedirects(t *testing.T) {
	key := []byte("0123456789abcdef")
	plain := []byte("original bytes")
	encrypted := mustEncryptAESECBForTest(t, plain, key)
	c := NewClawBotClient()
	c.httpClient.Transport = clawBotProtocolTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(encrypted))}, nil
	})
	item := ClawBotIncomingMessageItem{Type: 4, FileItem: &ClawBotFileItem{FileName: "original.bin", Media: &ClawBotCDNMedia{FullURL: "https://cdn.example.test/x", AESKey: base64.StdEncoding.EncodeToString(key)}}}
	item.FileItem.Len = "1"
	if _, _, _, err := c.DownloadMediaItem(context.Background(), item); err == nil {
		t.Fatal("size mismatch accepted")
	}
	item.FileItem.Len = ""
	item.FileItem.MD5 = strings.Repeat("0", 32)
	if _, _, _, err := c.DownloadMediaItem(context.Background(), item); err == nil {
		t.Fatal("checksum mismatch accepted")
	}
	item.FileItem.MD5 = ""
	calls := 0
	c.httpClient.Transport = clawBotProtocolTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://127.0.0.1/private"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	if _, _, _, err := c.DownloadMediaItem(context.Background(), item); err == nil || calls != 1 {
		t.Fatalf("redirect followed: %d %v", calls, err)
	}
}

func TestReceiveMediaNilTransportStillRejectsPrivateDNS(t *testing.T) {
	c := NewClawBotClient()
	c.httpClient.Transport = nil
	// localhost. bypasses literal localhost checking but must fail at dial time.
	_, err := c.DownloadImageItem(context.Background(), &ClawBotImageItem{Media: &ClawBotCDNMedia{FullURL: "https://localhost./private"}})
	if err == nil {
		t.Fatal("nil transport bypassed DNS address checking")
	}
}
