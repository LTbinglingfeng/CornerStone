package client

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	for _, encryptType := range []int{0, 1, 42} {
		for _, typ := range []struct{ wire, kind string }{
			{`{"type":3,"voice_item":{"text":"transcription","encode_type":6,"media":MEDIA}}`, "audio"},
			{`{"type":4,"file_item":{"file_name":"notes.txt","len":"20","media":MEDIA}}`, "file"},
			{`{"type":5,"video_item":{"video_size":32,"media":MEDIA}}`, "video"},
		} {
			media, _ := json.Marshal(ClawBotCDNMedia{FullURL: "https://cdn.example.test/media", AESKey: base64.StdEncoding.EncodeToString(key), EncryptType: encryptType})
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

func TestReceiveImageEncryptionDependsOnKey(t *testing.T) {
	key := []byte("0123456789abcdef")
	plain := []byte("original image bytes")
	encrypted := mustEncryptAESECBForTest(t, plain, key)
	mediaKey := base64.StdEncoding.EncodeToString(key)
	imageKey := hex.EncodeToString(key)
	for _, encryptType := range []int{0, 1, 42} {
		for _, tc := range []struct {
			name, mediaKey, imageKey string
			body                     []byte
			wantErr                  bool
			wantCalls                int
		}{
			{"plaintext", "", "", plain, false, 1},
			{"media key", mediaKey, "", encrypted, false, 1},
			{"image key", "", imageKey, encrypted, false, 1},
			{"image key takes priority", base64.StdEncoding.EncodeToString([]byte("fedcba9876543210")), imageKey, encrypted, false, 1},
			{"image key overrides malformed media key", "bad-key", imageKey, encrypted, false, 1},
			{"malformed media key", "bad-key", "", plain, true, 0},
			{"short media key", base64.StdEncoding.EncodeToString([]byte("short")), "", plain, true, 0},
			{"malformed image key does not fall back", mediaKey, "not-hex", encrypted, true, 0},
			{"short image key", mediaKey, "0123", encrypted, true, 0},
			{"wrong AES key", base64.StdEncoding.EncodeToString([]byte("fedcba9876543210")), "", encrypted, true, 1},
			{"invalid ciphertext length", mediaKey, "", []byte("broken"), true, 1},
			{"invalid ciphertext padding", mediaKey, "", make([]byte, 16), true, 1},
			{"plaintext with key does not fall back", mediaKey, "", plain, true, 1},
		} {
			t.Run(fmt.Sprintf("encrypt_type_%d/%s", encryptType, tc.name), func(t *testing.T) {
				c := NewClawBotClient()
				calls := 0
				c.httpClient.Transport = clawBotProtocolTransport(func(r *http.Request) (*http.Response, error) {
					calls++
					return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(tc.body)), Request: r}, nil
				})
				got, err := c.DownloadImageItem(context.Background(), &ClawBotImageItem{
					AESKey: tc.imageKey,
					Media:  &ClawBotCDNMedia{FullURL: "https://cdn.example.test/image", AESKey: tc.mediaKey, EncryptType: encryptType},
				})
				if (err != nil) != tc.wantErr {
					t.Fatalf("error = %v, wantErr = %v", err, tc.wantErr)
				}
				if tc.wantErr {
					if got != nil {
						t.Fatalf("returned bytes on error: %q", got)
					}
				} else if !bytes.Equal(got, plain) {
					t.Fatalf("got %q, want %q", got, plain)
				}
				if calls != tc.wantCalls {
					t.Fatalf("requests = %d, want %d", calls, tc.wantCalls)
				}
			})
		}
	}
}

func TestReceiveNonImageMediaRequiresKeyBeforeRequest(t *testing.T) {
	for _, encryptType := range []int{0, 1, 42} {
		for _, key := range []string{"", " \t"} {
			media := &ClawBotCDNMedia{FullURL: "https://cdn.example.test/media", AESKey: key, EncryptType: encryptType}
			for _, tc := range []struct {
				name string
				item ClawBotIncomingMessageItem
			}{
				{"voice", ClawBotIncomingMessageItem{Type: 3, VoiceItem: &ClawBotVoiceItem{Media: media}}},
				{"file", ClawBotIncomingMessageItem{Type: 4, FileItem: &ClawBotFileItem{Media: media}}},
				{"video", ClawBotIncomingMessageItem{Type: 5, VideoItem: &ClawBotVideoItem{Media: media}}},
			} {
				t.Run(fmt.Sprintf("encrypt_type_%d/key_%q/%s", encryptType, key, tc.name), func(t *testing.T) {
					c := NewClawBotClient()
					c.httpClient.Transport = clawBotProtocolTransport(func(r *http.Request) (*http.Response, error) {
						t.Fatal("missing key must be rejected before any network request")
						return nil, nil
					})
					got, _, _, err := c.DownloadMediaItem(context.Background(), tc.item)
					if err == nil || err.Error() != "missing media encryption key" || got != nil {
						t.Fatalf("got %q, error = %v; want missing key error", got, err)
					}
				})
			}
		}
	}
}
