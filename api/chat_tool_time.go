package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/config"
	"fmt"
	"strings"
	"time"
)

type timeToolSummary struct {
	Timezone           string `json:"timezone"`
	TimezoneOffset     string `json:"timezone_offset"`
	NowRFC3339         string `json:"now_rfc3339"`
	NowUnix            int64  `json:"now_unix"`
	Date               string `json:"date"`
	Time               string `json:"time"`
	Weekday            string `json:"weekday"`
	Source             string `json:"source"`
	NTPServer          string `json:"ntp_server"`
	LastSyncAt         string `json:"last_sync_at"`
	LastSyncSuccess    bool   `json:"last_sync_success"`
	SyncMessage        string `json:"sync_message"`
	RTTMilliseconds    int64  `json:"rtt_ms"`
	OffsetMilliseconds int64  `json:"offset_ms"`
}

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
	status := e.exactTimeService.Status()

	syncMessage := status.Message
	if syncMessage == "" {
		syncMessage = "status unavailable"
	}
	if timeZoneMessage != "" {
		syncMessage = strings.TrimSpace(syncMessage + "; " + timeZoneMessage)
	}

	lastSyncAt := ""
	if !status.LastSyncAt.IsZero() {
		lastSyncAt = status.LastSyncAt.In(location).Format(time.RFC3339)
	}

	return chatToolResult{
		OK: true,
		Data: timeToolSummary{
			Timezone:           timeZoneName,
			TimezoneOffset:     formatTimeZoneOffset(now),
			NowRFC3339:         now.Format(time.RFC3339),
			NowUnix:            now.Unix(),
			Date:               now.Format("2006-01-02"),
			Time:               now.Format("15:04:05"),
			Weekday:            now.Weekday().String(),
			Source:             "app_ntp_time_service",
			NTPServer:          status.Server,
			LastSyncAt:         lastSyncAt,
			LastSyncSuccess:    status.LastSuccess,
			SyncMessage:        syncMessage,
			RTTMilliseconds:    status.RTT.Milliseconds(),
			OffsetMilliseconds: status.Offset.Milliseconds(),
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

func formatTimeZoneOffset(now time.Time) string {
	_, offsetSeconds := now.Zone()
	sign := "+"
	if offsetSeconds < 0 {
		sign = "-"
		offsetSeconds = -offsetSeconds
	}

	hours := offsetSeconds / 3600
	minutes := (offsetSeconds % 3600) / 60
	return fmt.Sprintf("%s%02d:%02d", sign, hours, minutes)
}
