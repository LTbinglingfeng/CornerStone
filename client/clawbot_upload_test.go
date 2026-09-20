package client

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func clawBotUploadFixtureClient(server *httptest.Server) *http.Client {
	base, _ := url.Parse(server.URL)
	transport := server.Client().Transport
	return &http.Client{Transport: clawBotProtocolTransport(func(r *http.Request) (*http.Response, error) {
		clone := r.Clone(r.Context())
		clone.URL.Scheme, clone.URL.Host = base.Scheme, base.Host
		return transport.RoundTrip(clone)
	})}
}

func TestClawBotMediaUploadProtocol(t *testing.T) {
	for _, kind := range []int{1, 2, 3} {
		t.Run(string(rune('0'+kind)), func(t *testing.T) {
			plain := []byte("bounded test media payload")
			var key []byte
			var uploaded bool
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/ilink/bot/getuploadurl" {
					var body struct {
						MediaType   int             `json:"media_type"`
						RawSize     int             `json:"rawsize"`
						RawMD5      string          `json:"rawfilemd5"`
						FileSize    int             `json:"filesize"`
						AESKey      string          `json:"aeskey"`
						FileKey     string          `json:"filekey"`
						NoNeedThumb bool            `json:"no_need_thumb"`
						BaseInfo    ClawBotBaseInfo `json:"base_info"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					sum := md5.Sum(plain)
					if body.MediaType != kind || body.RawSize != len(plain) || body.RawMD5 != hex.EncodeToString(sum[:]) || body.FileSize != 32 || !body.NoNeedThumb || len(body.FileKey) != 32 || body.BaseInfo.BotAgent == "" {
						t.Errorf("bad upload request: %+v", body)
					}
					if r.Header.Get("Authorization") != "Bearer test-token" {
						t.Error("missing API authorization")
					}
					key, _ = hex.DecodeString(body.AESKey)
					_ = json.NewEncoder(w).Encode(map[string]any{"ret": 0, "upload_full_url": "https://media.example/cdn"})
					return
				}
				if r.URL.Path != "/cdn" || r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/octet-stream" || r.Header.Get("Authorization") != "" {
					t.Error("bad CDN request")
				}
				ciphertext, _ := io.ReadAll(r.Body)
				decoded, err := decryptClawBotAESECB(ciphertext, key)
				if err != nil || !bytes.Equal(decoded, plain) {
					t.Errorf("decrypt: %q %v", decoded, err)
				}
				uploaded = true
				w.Header().Set("x-encrypted-param", "download-param")
			}))
			defer server.Close()
			c := NewClawBotClient()
			c.httpClient = clawBotUploadFixtureClient(server)
			got, err := c.UploadMedia(context.Background(), server.URL, "test-token", "uin", "user", kind, plain)
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := parseClawBotMediaAESKey(got.Media.AESKey)
			if !uploaded || got.RawSize != len(plain) || got.CipherSize != 32 || got.Media.EncryptType != 1 || got.Media.EncryptQueryParam != "download-param" || err != nil || !bytes.Equal(key, parsed) {
				t.Fatalf("invalid upload receipt: %+v, %v", got, err)
			}
		})
	}
}

func TestClawBotUploadFailuresDoNotSend(t *testing.T) {
	for _, failure := range []string{"missing-url", "missing-receipt", "http-error", "redirect", "business-error"} {
		t.Run(failure, func(t *testing.T) {
			sendCalls := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/ilink/bot/getuploadurl":
					if failure == "business-error" {
						_, _ = w.Write([]byte(`{"ret":-1}`))
						return
					}
					if failure == "missing-url" {
						_, _ = w.Write([]byte(`{"ret":0}`))
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"upload_full_url": "https://media.example/cdn"})
				case "/cdn":
					if failure == "http-error" {
						w.WriteHeader(503)
					}
					if failure == "redirect" {
						w.Header().Set("Location", "/ilink/bot/sendmessage")
						w.WriteHeader(307)
					}
				default:
					sendCalls++
				}
			}))
			defer server.Close()
			c := NewClawBotClient()
			c.httpClient = clawBotUploadFixtureClient(server)
			if _, err := c.UploadMedia(context.Background(), server.URL, "test-token", "uin", "user", 1, []byte("test")); err == nil {
				t.Fatal("wanted failure")
			}
			if sendCalls != 0 {
				t.Fatal("upload attempted send or followed redirect")
			}
		})
	}
}

func TestClawBotUploadCipherPaddingAndURL(t *testing.T) {
	for _, n := range []int{0, 1, 15, 16, 17, 32} {
		key := bytes.Repeat([]byte{1}, 16)
		plain := bytes.Repeat([]byte{2}, n)
		encrypted, err := encryptClawBotAESECB(plain, key)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := decryptClawBotAESECB(encrypted, key)
		if err != nil || !bytes.Equal(decoded, plain) || len(encrypted) != (n/16+1)*16 {
			t.Fatalf("padding n=%d err=%v", n, err)
		}
	}
	u, err := url.Parse(buildClawBotCDNUploadURL("https://cdn.example/c2c/", "a&b /+", "key/?"))
	if err != nil || u.Query().Get("encrypted_query_param") != "a&b /+" || u.Query().Get("filekey") != "key/?" || strings.Contains(u.Path, "//upload") {
		t.Fatalf("url=%v err=%v", u, err)
	}
}
