package client

import (
	"bytes"
	"context"
	"crypto/aes"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExtractImageItemsFromClawBotMessage(t *testing.T) {
	msg := ClawBotIncomingMessage{
		ItemList: []ClawBotIncomingMessageItem{
			{Type: 1, TextItem: &ClawBotItemText{Text: "hello"}},
			{Type: 2, ImageItem: &ClawBotImageItem{}},
			{Type: 2, ImageItem: &ClawBotImageItem{}},
		},
	}

	items := ExtractImageItemsFromClawBotMessage(msg)
	if len(items) != 2 {
		t.Fatalf("ExtractImageItemsFromClawBotMessage len = %d, want 2", len(items))
	}
}

func TestParseClawBotMediaAESKey(t *testing.T) {
	rawKey := []byte("0123456789abcdef")

	key, err := parseClawBotMediaAESKey(base64.StdEncoding.EncodeToString(rawKey))
	if err != nil {
		t.Fatalf("parseClawBotMediaAESKey raw err = %v", err)
	}
	if !bytes.Equal(key, rawKey) {
		t.Fatalf("parseClawBotMediaAESKey raw = %x, want %x", key, rawKey)
	}

	hexPayload := []byte(hex.EncodeToString(rawKey))
	key, err = parseClawBotMediaAESKey(base64.StdEncoding.EncodeToString(hexPayload))
	if err != nil {
		t.Fatalf("parseClawBotMediaAESKey hex err = %v", err)
	}
	if !bytes.Equal(key, rawKey) {
		t.Fatalf("parseClawBotMediaAESKey hex = %x, want %x", key, rawKey)
	}
}

func TestDownloadImageItem(t *testing.T) {
	rawKey := []byte("0123456789abcdef")
	plaintext := []byte("test image payload")
	ciphertext := mustEncryptAESECBForTest(t, plaintext, rawKey)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(ciphertext)
	}))
	defer server.Close()

	client := NewClawBotClient()
	data, err := client.DownloadImageItem(context.Background(), &ClawBotImageItem{
		Media: &ClawBotCDNMedia{
			FullURL: server.URL,
		},
		AESKey: hex.EncodeToString(rawKey),
	})
	if err != nil {
		t.Fatalf("DownloadImageItem err = %v", err)
	}
	if !bytes.Equal(data, plaintext) {
		t.Fatalf("DownloadImageItem data = %q, want %q", data, plaintext)
	}
}

func mustEncryptAESECBForTest(t *testing.T, plaintext, key []byte) []byte {
	t.Helper()

	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("aes.NewCipher err = %v", err)
	}

	padding := aes.BlockSize - (len(plaintext) % aes.BlockSize)
	if padding == 0 {
		padding = aes.BlockSize
	}
	padded := append(append([]byte(nil), plaintext...), bytes.Repeat([]byte{byte(padding)}, padding)...)

	ciphertext := make([]byte, len(padded))
	for offset := 0; offset < len(padded); offset += aes.BlockSize {
		block.Encrypt(ciphertext[offset:offset+aes.BlockSize], padded[offset:offset+aes.BlockSize])
	}
	return ciphertext
}
