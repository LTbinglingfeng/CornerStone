package logging

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/google/uuid"
)

type fieldsKey struct{}

// WithFields adds metadata to a context without mutating the parent's fields.
// Callers must pass metadata only: never credentials, message bodies or tool arguments.
func WithFields(ctx context.Context, fields ...any) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	values := make(map[string]any)
	if inherited, ok := ctx.Value(fieldsKey{}).(map[string]any); ok {
		for key, value := range inherited {
			values[key] = value
		}
	}
	addFields(values, fields)
	return context.WithValue(ctx, fieldsKey{}, values)
}

func addFields(values map[string]any, fields []any) {
	for i := 0; i+1 < len(fields); i += 2 {
		key, ok := fields[i].(string)
		if !ok || key == "" || strings.ContainsAny(key, " \t\r\n=\"") {
			continue
		}
		values[key] = fields[i+1]
	}
}

// EnsureRequest creates a correlation ID only at the first request boundary.
// Internal generation, model and tool operations retain the original source/trigger.
func EnsureRequest(ctx context.Context, source, trigger string) context.Context {
	if RequestID(ctx) != "" {
		return ctx
	}
	return WithFields(ctx, "request", uuid.NewString(), "source", source, "trigger", trigger)
}

func RequestID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	values, _ := ctx.Value(fieldsKey{}).(map[string]any)
	id, _ := values["request"].(string)
	return id
}

// Event emits a single-line, deterministic key=value event using the existing
// text logger. String quoting prevents untrusted metadata from forging log lines.
func Event(ctx context.Context, event string, fields ...any) {
	values := make(map[string]any)
	if ctx != nil {
		if inherited, ok := ctx.Value(fieldsKey{}).(map[string]any); ok {
			for key, value := range inherited {
				values[key] = value
			}
		}
	}
	addFields(values, fields)
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	fmt.Fprintf(&b, "[%s] event=%q", eventLevel(values), event)
	for _, key := range keys {
		fmt.Fprintf(&b, " %s=", key)
		switch value := values[key].(type) {
		case string:
			fmt.Fprintf(&b, "%q", Truncate(value, 256))
		case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
			fmt.Fprint(&b, value)
		default:
			fmt.Fprintf(&b, "%q", Truncate(fmt.Sprint(value), 256))
		}
	}
	_ = logger.Output(2, b.String())
}

// Derive severity from safe outcome metadata, not from provider error text.
// Cancellation is a normal lifecycle outcome rather than a server error.
func eventLevel(fields map[string]any) string {
	kind, _ := fields["error_kind"].(string)
	if kind == "canceled" {
		return "INFO"
	}
	status, _ := fields["status"].(string)
	if status == "uncertain" || status == "unconfirmed" || status == "partial" {
		return "WARN"
	}
	if status == "error" || status == "failed" || status == "partial_failed" || status == "write_failed" || status == "aborted" || kind == "failed" || kind == "timeout" {
		return "ERROR"
	}
	if status, ok := fields["http_status"].(int); ok {
		if status >= 500 {
			return "ERROR"
		}
		if status >= 400 {
			return "WARN"
		}
	}
	return "INFO"
}

// ErrorKind deliberately excludes the original error text, which can contain
// upstream credentials, URLs, request/response bodies or user content.
func ErrorKind(err error) string {
	switch {
	case err == nil:
		return "none"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	default:
		var timeout net.Error
		if errors.As(err, &timeout) && timeout.Timeout() {
			return "timeout"
		}
		return "failed"
	}
}
