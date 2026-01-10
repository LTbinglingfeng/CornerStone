package client

import (
	"encoding/base64"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type imagePayload struct {
	MimeType string
	Data     string
}

func loadImagePayload(path string) (imagePayload, error) {
	data, errReadFile := os.ReadFile(path)
	if errReadFile != nil {
		return imagePayload{}, fmt.Errorf("read image file: %w", errReadFile)
	}

	mimeType := http.DetectContentType(data)
	if mimeType == "application/octet-stream" {
		ext := strings.ToLower(filepath.Ext(path))
		if ext != "" {
			if extMime := mime.TypeByExtension(ext); extMime != "" {
				mimeType = extMime
			}
		}
	}
	if !strings.HasPrefix(mimeType, "image/") {
		return imagePayload{}, fmt.Errorf("unsupported image type: %s", mimeType)
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	return imagePayload{MimeType: mimeType, Data: encoded}, nil
}
