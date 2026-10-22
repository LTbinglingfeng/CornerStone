package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGeminiImageInteractionsConfigCompatibility(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	legacy := `{"providers":[{"id":"image-fixture","type":"gemini_image","model":"configured-model","gemini_image_size":"4k","gemini_image_number_of_images":4,"gemini_image_output_mime_type":"image/png"}]}`
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(path)
	if err := manager.Update(manager.Get()); err != nil {
		t.Fatal(err)
	}
	provider := NewManager(path).GetProvider("image-fixture")
	if provider == nil || provider.GeminiImageSize == nil || *provider.GeminiImageSize != "4K" {
		t.Fatalf("Interactions 4K setting was lost: %+v", provider)
	}
	if provider.Model != "configured-model" || provider.GeminiImageNumberOfImages == nil || *provider.GeminiImageNumberOfImages != 4 || provider.GeminiImageOutputMIMEType == nil || *provider.GeminiImageOutputMIMEType != "image/png" {
		t.Fatal("legacy image options or model silently changed")
	}
}
