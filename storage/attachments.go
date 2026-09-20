package storage

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/google/uuid"
)

const MaxAttachmentBytes = 100 << 20
const AttachmentURLPrefix = "/api/attachments/"

type ChatAttachment struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	MIMEType string `json:"mime_type"`
	Kind     string `json:"kind"`
	Size     int64  `json:"size"`
}

// AttachmentStore stores opaque bytes under generated IDs, never sender filenames.
// dir must be the trusted data directory's attachments subdirectory.
type AttachmentStore struct{ Dir string }

func attachmentID(path string) (string, error) {
	if !strings.HasPrefix(path, AttachmentURLPrefix) {
		return "", errors.New("invalid attachment path")
	}
	id := strings.TrimPrefix(path, AttachmentURLPrefix)
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.String() != id {
		return "", errors.New("invalid attachment ID")
	}
	return id, nil
}
func validAttachmentName(name string) bool {
	if name == "" || name == "." || name == ".." || len(name) > 240 || strings.ContainsAny(name, "/\\:") {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}
func (s AttachmentStore) Save(data []byte, name, kind string) (ChatAttachment, error) {
	var a ChatAttachment
	if len(data) == 0 || len(data) > MaxAttachmentBytes || !validAttachmentName(name) {
		return a, errors.New("invalid attachment size or filename")
	}
	mimeType := strings.Split(http.DetectContentType(data), ";")[0]
	switch kind {
	case "audio":
		if strings.HasPrefix(string(data[:min(len(data), 16)]), "#!SILK_V3") || strings.HasPrefix(string(data[:min(len(data), 16)]), "\x02#!SILK_V3") {
			mimeType = "audio/silk"
		}
		// Unknown codecs are preserved as opaque downloads, never advertised as playable audio.
		if !strings.HasPrefix(mimeType, "audio/") {
			mimeType = "application/octet-stream"
		}
	case "video":
		if mimeType == "application/octet-stream" && len(data) >= 12 && string(data[4:8]) == "ftyp" {
			mimeType = "video/mp4"
		}
		if !strings.HasPrefix(mimeType, "video/") {
			// Preserve unknown original codecs as downloads, just like voice.
			mimeType = "application/octet-stream"
		}
	case "file":
		// Active documents are always downloaded, never rendered inline.
	default:
		return a, errors.New("invalid attachment kind")
	}
	if err := os.MkdirAll(s.Dir, 0700); err != nil {
		return a, err
	}
	f, err := os.CreateTemp(s.Dir, ".incoming-")
	if err != nil {
		return a, err
	}
	temp := f.Name()
	defer os.Remove(temp)
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return a, err
	}
	if closeErr != nil {
		return a, closeErr
	}
	id := uuid.NewString()
	if err := os.Rename(temp, filepath.Join(s.Dir, id)); err != nil {
		return a, err
	}
	return ChatAttachment{Path: AttachmentURLPrefix + id, Name: name, MIMEType: mimeType, Kind: kind, Size: int64(len(data))}, nil
}
func (s AttachmentStore) Open(path string) (*os.File, error) {
	id, err := attachmentID(path)
	if err != nil {
		return nil, err
	}
	// os.Root confines access even if an attachment has been replaced with a symlink.
	root, err := os.OpenRoot(s.Dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.Open(id)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > MaxAttachmentBytes {
		f.Close()
		return nil, errors.New("invalid stored attachment")
	}
	return f, nil
}
func (s AttachmentStore) Read(path string) ([]byte, error) {
	f, err := s.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxAttachmentBytes+1))
	if len(data) > MaxAttachmentBytes {
		return nil, errors.New("attachment exceeds limit")
	}
	return data, err
}
func (s AttachmentStore) Remove(path string) error {
	id, err := attachmentID(path)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(s.Dir)
	if err != nil {
		return err
	}
	defer root.Close()
	return root.Remove(id)
}
