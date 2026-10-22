package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/config"
	"fmt"
	"strings"
	"time"
)

// timeToolSummary is what the model sees. Keep it minimal: NTP diagnostics
// (server, rtt, offset, sync status) are noise for the model.
type timeToolSummary struct {
	Now      string `json:"now"`
	Weekday  string `json:"weekday"`
	Timezone string `json:"timezone"`
	Note     string `json:"note,omitempty"`
}

var chineseWeekdays = [...]string{"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"}

func (e *chatToolExecutor) handleGetTime(ctx context.Context, toolCall client.ToolCall, toolCtx chatToolContext) chatToolResult {
	_ = ctx
	_ = toolCtx

	if e.exactTimeService == nil {
		return chatToolResult{OK: false, Data: nil, Error: "exact time service not configured"}
	}
	if e.configManager == nil {
		return chatToolResult{OK: false, Data: nil, Error: "config manager not configured"}
	}

	var args struct{}
	if err := decodeToolArguments(toolCall.Function.Arguments, &args); err != nil {
		return chatToolResult{OK: false, Data: nil, Error: "invalid arguments"}
	}

	cfg := e.configManager.Get()
	location, timeZoneName, timeZoneMessage := resolveConfiguredTimeLocation(cfg.TimeZone)
	now := e.exactTimeService.Now().In(location)

	return chatToolResult{
		OK: true,
		Data: timeToolSummary{
			Now:      now.Format(time.RFC3339),
			Weekday:  chineseWeekdays[now.Weekday()],
			Timezone: timeZoneName,
			Note:     timeZoneMessage,
		},
	}
}

func resolveConfiguredTimeLocation(timeZone string) (*time.Location, string, string) {
	trimmed := strings.TrimSpace(timeZone)
	if trimmed == "" {
		trimmed = config.DefaultTimeZone
	}

	location, err := time.LoadLocation(trimmed)
	if err == nil {
		return location, trimmed, ""
	}

	fallbackLocation, fallbackErr := time.LoadLocation(config.DefaultTimeZone)
	if fallbackErr == nil {
		return fallbackLocation, config.DefaultTimeZone, fmt.Sprintf("timezone %q is invalid, fallback to %s", trimmed, config.DefaultTimeZone)
	}

	return time.Local, time.Local.String(), fmt.Sprintf("timezone %q is invalid, fallback to local", trimmed)
}
