package storage

import (
	"errors"
)

var (
	ErrInvalidID       = errors.New("invalid id")
	ErrInvalidFileName = errors.New("invalid file name")
)

// ValidateID validates IDs that may later be used as path segments.
// It is intentionally strict to be cross-platform safe and prevent path traversal.
func ValidateID(id string) error {
	if id == "" || len(id) > 128 {
		return ErrInvalidID
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '-':
		default:
			return ErrInvalidID
		}
	}
	return nil
}

// ValidateFileName validates a single file name (no directory components).
func ValidateFileName(name string) error {
	if name == "" || len(name) > 255 {
		return ErrInvalidFileName
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '-' || r == '.':
		default:
			return ErrInvalidFileName
		}
	}
	if name == "." || name == ".." {
		return ErrInvalidFileName
	}
	return nil
}
