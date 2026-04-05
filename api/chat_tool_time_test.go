package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/config"
	"cornerstone/exacttime"
	"encoding/json"
	"testing"
	"time"
)

type stubExactTimeService struct {
	now    time.Time
	status exacttime.Status
}

func (s *stubExactTimeService) Now() time.Time {
	return s.now
}

func (s *stubExactTimeService) Status() exacttime.Status {
	return s.status
}

func decodeTimeToolResult(t *testing.T, raw string) (chatToolResult, timeToolSummary) {
	t.Helper()

	var result chatToolResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("Unmarshal chatToolResult failed: %v", err)
	}

	dataBytes, err := json.Marshal(result.Data)
	if err != nil {
		t.Fatalf("Marshal result.Data failed: %v", err)
	}

	var summary timeToolSummary
	if err := json.Unmarshal(dataBytes, &summary); err != nil {
		t.Fatalf("Unmarshal timeToolSummary failed: %v", err)
	}

	return result, summary
}

func TestGetChatTools_IncludesTimeAndClawBotKeepsReadOnlyTools(t *testing.T) {
	tools := getChatTools()
	foundTime := false
	for _, tool := range tools {
		if tool.Function.Name == "get_time" {
			foundTime = true
			break
		}
	}
	if !foundTime {
		t.Fatal("get_time tool not registered")
	}

	clawBotTools := getChatTools(chatToolOptions{Channel: chatToolChannelClawBot})
	if len(clawBotTools) != 4 {
		t.Fatalf("clawbot tools len = %d, want 4", len(clawBotTools))
	}

	names := make(map[string]struct{}, len(clawBotTools))
	for _, tool := range clawBotTools {
		names[tool.Function.Name] = struct{}{}
	}
	if _, ok := names["get_time"]; !ok {
		t.Fatalf("clawbot tools = %#v, want get_time", clawBotTools)
	}
	if _, ok := names["get_weather"]; !ok {
		t.Fatalf("clawbot tools = %#v, want get_weather", clawBotTools)
	}
	if _, ok := names["schedule_reminder"]; !ok {
		t.Fatalf("clawbot tools = %#v, want schedule_reminder", clawBotTools)
	}
	if _, ok := names["no_reply"]; !ok {
		t.Fatalf("clawbot tools = %#v, want no_reply", clawBotTools)
	}

	clawBotTools = getChatTools(chatToolOptions{
		Channel:          chatToolChannelClawBot,
		WebSearchEnabled: true,
	})
	if len(clawBotTools) != 5 {
		t.Fatalf("clawbot tools len with web search = %d, want 5", len(clawBotTools))
	}

	names = make(map[string]struct{}, len(clawBotTools))
	for _, tool := range clawBotTools {
		names[tool.Function.Name] = struct{}{}
	}
	if _, ok := names["web_search"]; !ok {
		t.Fatalf("clawbot tools = %#v, want web_search when enabled", clawBotTools)
	}

	disabledTimeTools := getChatTools(chatToolOptions{
		ToolToggles: map[string]bool{
			"get_time": false,
		},
	})
	foundTime = false
	for _, tool := range disabledTimeTools {
		if tool.Function.Name == "get_time" {
			foundTime = true
			break
		}
	}
	if !foundTime {
		t.Fatalf("get_time should remain exposed even when toggle is off: %#v", disabledTimeTools)
	}

	clawBotTools = getChatTools(chatToolOptions{
		Channel: chatToolChannelClawBot,
		ToolToggles: map[string]bool{
			"get_weather": false,
		},
	})
	if len(clawBotTools) != 4 {
		t.Fatalf("clawbot tools len with get_weather toggle disabled = %d, want 4", len(clawBotTools))
	}
	names = make(map[string]struct{}, len(clawBotTools))
	for _, tool := range clawBotTools {
		names[tool.Function.Name] = struct{}{}
	}
	if _, ok := names["get_time"]; !ok {
		t.Fatalf("clawbot tools = %#v, want get_time", clawBotTools)
	}
	if _, ok := names["get_weather"]; !ok {
		t.Fatalf("clawbot tools = %#v, want get_weather even when toggle is off", clawBotTools)
	}
	if _, ok := names["schedule_reminder"]; !ok {
		t.Fatalf("clawbot tools = %#v, want schedule_reminder", clawBotTools)
	}
	if _, ok := names["no_reply"]; !ok {
		t.Fatalf("clawbot tools = %#v, want no_reply", clawBotTools)
	}
}

func TestChatToolExecutor_GetTimeUsesConfiguredTimeZone(t *testing.T) {
	nowUTC := time.Date(2026, 4, 4, 23, 30, 45, 0, time.UTC)
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("LoadLocation failed: %v", err)
	}
	expectedLocal := nowUTC.In(location)

	configManager := newTestProviderConfigManager(t, newTestProvider("provider-1"))
	cfg := configManager.Get()
	cfg.TimeZone = "America/New_York"
	if err := configManager.Update(cfg); err != nil {
		t.Fatalf("Update config failed: %v", err)
	}

	executor := newChatToolExecutor()
	executor.configManager = configManager
	executor.exactTimeService = &stubExactTimeService{
		now: nowUTC,
		status: exacttime.Status{
			Server:      "ntp.aliyun.com",
			Enabled:     true,
			LastSyncAt:  time.Date(2026, 4, 4, 23, 25, 0, 0, time.UTC),
			LastSuccess: true,
			Message:     "ntp sync succeeded",
			RTT:         85 * time.Millisecond,
			Offset:      1500 * time.Millisecond,
		},
	}

	raw := executor.Execute(context.Background(), client.ToolCall{
		ID:   "call-time-1",
		Type: "function",
		Function: client.ToolCallFunction{
			Name:      "get_time",
			Arguments: `{}`,
		},
	}, chatToolContext{})

	result, summary := decodeTimeToolResult(t, raw)
	if !result.OK {
		t.Fatalf("result.OK = false, error=%q", result.Error)
	}
	if summary.Timezone != "America/New_York" {
		t.Fatalf("timezone = %q, want %q", summary.Timezone, "America/New_York")
	}
	expectedOffset := formatTimeZoneOffset(expectedLocal)
	if summary.TimezoneOffset != expectedOffset {
		t.Fatalf("timezone_offset = %q, want %q", summary.TimezoneOffset, expectedOffset)
	}
	if summary.NowRFC3339 != expectedLocal.Format(time.RFC3339) {
		t.Fatalf("now_rfc3339 = %q, want %q", summary.NowRFC3339, expectedLocal.Format(time.RFC3339))
	}
	if summary.NowUnix != nowUTC.Unix() {
		t.Fatalf("now_unix = %d, want %d", summary.NowUnix, nowUTC.Unix())
	}
	if summary.Date != expectedLocal.Format("2006-01-02") {
		t.Fatalf("date = %q, want %q", summary.Date, expectedLocal.Format("2006-01-02"))
	}
	if summary.Time != expectedLocal.Format("15:04:05") {
		t.Fatalf("time = %q, want %q", summary.Time, expectedLocal.Format("15:04:05"))
	}
	if summary.Weekday != expectedLocal.Weekday().String() {
		t.Fatalf("weekday = %q, want %q", summary.Weekday, expectedLocal.Weekday().String())
	}
	if summary.Source != "app_ntp_time_service" {
		t.Fatalf("source = %q, want %q", summary.Source, "app_ntp_time_service")
	}
	if summary.NTPServer != "ntp.aliyun.com" {
		t.Fatalf("ntp_server = %q, want %q", summary.NTPServer, "ntp.aliyun.com")
	}
	if summary.LastSyncAt == "" {
		t.Fatal("last_sync_at is empty")
	}
	if !summary.LastSyncSuccess {
		t.Fatal("last_sync_success = false, want true")
	}
	if summary.SyncMessage != "ntp sync succeeded" {
		t.Fatalf("sync_message = %q, want %q", summary.SyncMessage, "ntp sync succeeded")
	}
	if summary.RTTMilliseconds != 85 {
		t.Fatalf("rtt_ms = %d, want %d", summary.RTTMilliseconds, 85)
	}
	if summary.OffsetMilliseconds != 1500 {
		t.Fatalf("offset_ms = %d, want %d", summary.OffsetMilliseconds, 1500)
	}
}

func TestChatToolExecutor_GetTimeAcceptsEmptyArguments(t *testing.T) {
	configManager := newTestProviderConfigManager(t, newTestProvider("provider-1"))
	cfg := configManager.Get()
	cfg.TimeZone = config.DefaultTimeZone
	if err := configManager.Update(cfg); err != nil {
		t.Fatalf("Update config failed: %v", err)
	}

	executor := newChatToolExecutor()
	executor.configManager = configManager
	executor.exactTimeService = &stubExactTimeService{
		now: time.Date(2026, 4, 4, 10, 0, 0, 0, time.UTC),
		status: exacttime.Status{
			Server:      "ntp.aliyun.com",
			Enabled:     true,
			LastSuccess: false,
			Message:     "ntp sync not completed yet",
		},
	}

	raw := executor.Execute(context.Background(), client.ToolCall{
		ID:   "call-time-2",
		Type: "function",
		Function: client.ToolCallFunction{
			Name:      "get_time",
			Arguments: "",
		},
	}, chatToolContext{})

	result, _ := decodeTimeToolResult(t, raw)
	if !result.OK {
		t.Fatalf("result.OK = false, error=%q", result.Error)
	}
}
