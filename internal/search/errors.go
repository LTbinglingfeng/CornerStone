package search

import (
	"errors"
	"fmt"
)

type ErrorKind string

const (
	ErrKindInvalidRequest        ErrorKind = "invalid_request"
	ErrKindProviderNotFound      ErrorKind = "provider_not_found"
	ErrKindProviderNotConfigured ErrorKind = "provider_not_configured"
	ErrKindTimeout               ErrorKind = "timeout"
	ErrKindUpstream              ErrorKind = "upstream"
	ErrKindBadResponse           ErrorKind = "bad_response"
)

type Error struct {
	Kind       ErrorKind
	ProviderID string
	Message    string
	StatusCode int
	Cause      error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	providerPart := ""
	if e.ProviderID != "" {
		providerPart = fmt.Sprintf(" provider=%s", e.ProviderID)
	}
	statusPart := ""
	if e.StatusCode > 0 {
		statusPart = fmt.Sprintf(" status=%d", e.StatusCode)
	}
	msg := e.Message
	if msg == "" && e.Cause != nil {
		msg = e.Cause.Error()
	}
	if msg == "" {
		msg = "search failed"
	}
	return fmt.Sprintf("cornerstone_web_search error kind=%s%s%s: %s", e.Kind, providerPart, statusPart, msg)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func IsKind(err error, kind ErrorKind) bool {
	var target *Error
	if !errors.As(err, &target) {
		return false
	}
	return target.Kind == kind
}

// PublicMessage returns a user/model-facing message that avoids leaking provider details.
func PublicMessage(err error) string {
	if err == nil {
		return ""
	}
	var target *Error
	if errors.As(err, &target) {
		msg := target.Message
		if msg == "" {
			msg = "search failed"
		}
		return msg
	}
	return err.Error()
}
