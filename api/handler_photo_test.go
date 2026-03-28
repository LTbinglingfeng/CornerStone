package api

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"
)

func TestSaveCachePhotoBytes(t *testing.T) {
	imageData, err := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO8B3ioAAAAASUVORK5CYII=",
	)
	if err != nil {
		t.Fatalf("decode png err = %v", err)
	}

	handler := &Handler{
		cachePhotoDir: t.TempDir(),
	}

	relPath, err := handler.saveCachePhotoBytes(imageData)
	if err != nil {
		t.Fatalf("saveCachePhotoBytes err = %v", err)
	}
	if !strings.HasPrefix(relPath, cachePhotoDirName+"/") {
		t.Fatalf("saveCachePhotoBytes relPath = %s, want prefix %s/", relPath, cachePhotoDirName)
	}

	absPath, err := handler.resolveCachePhotoPath(relPath)
	if err != nil {
		t.Fatalf("resolveCachePhotoPath err = %v", err)
	}
	if _, err := os.Stat(absPath); err != nil {
		t.Fatalf("saved image stat err = %v", err)
	}
}
