package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeLog(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cornerstone.log")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseLogLineStructuredEvent(t *testing.T) {
	line := `2026/10/23 07:51:54.933258 handler.go:247: [WARN] event="http.request.completed" duration_ms=3 http_status=401 method="GET" path="/api/x \"y\"" request="abc"`
	entry := ParseLogLine(line)
	if entry.Level != "WARN" || entry.Source != "handler.go:247" || entry.Event != "http.request.completed" {
		t.Fatalf("unexpected header: %+v", entry)
	}
	if entry.Time == "" || !strings.HasPrefix(entry.Time, "2026-10-23T07:51:54.933258") {
		t.Fatalf("unexpected time: %q", entry.Time)
	}
	want := []LogField{{"duration_ms", "3"}, {"http_status", "401"}, {"method", "GET"}, {"path", `/api/x "y"`}, {"request", "abc"}}
	if fmt.Sprint(entry.Fields) != fmt.Sprint(want) {
		t.Fatalf("fields = %v, want %v", entry.Fields, want)
	}
	if entry.Message != "" {
		t.Fatalf("message = %q", entry.Message)
	}
}

func TestParseLogLineLegacy(t *testing.T) {
	line := `2026/10/22 22:27:25.622611 qqofficial.go:134: [WARN] qqofficial connection error: err=QQ HTTP request failed (delivery may be uncertain) category= ip=[::1]:62120`
	entry := ParseLogLine(line)
	if entry.Message != "qqofficial connection error" {
		t.Fatalf("message = %q", entry.Message)
	}
	want := []LogField{{"err", "QQ HTTP request failed (delivery may be uncertain)"}, {"category", ""}, {"ip", "[::1]:62120"}}
	if fmt.Sprint(entry.Fields) != fmt.Sprint(want) {
		t.Fatalf("fields = %v, want %v", entry.Fields, want)
	}
	// A legacy `event=` field is channel metadata, not a structured event name.
	legacy := ParseLogLine(`2026/10/22 22:04:18.186465 asm_arm64.s:1447: [ERROR] process failed: event=ROBOT1.0_x err=boom`)
	if legacy.Event != "" || legacy.Fields[0].Key != "event" {
		t.Fatalf("legacy event misparsed: %+v", legacy)
	}
	plain := ParseLogLine("goroutine 1 [running]:")
	if plain.Level != "" || plain.Message != "goroutine 1 [running]:" {
		t.Fatalf("plain line misparsed: %+v", plain)
	}
}

func buildLines(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		level := "INFO"
		if i%10 == 0 {
			level = "ERROR"
		}
		fmt.Fprintf(&b, "2026/10/23 08:00:00.000000 x.go:1: [%s] line %04d\n", level, i)
	}
	return b.String()
}

func TestReadLogBeforePaginatesWithoutOverlap(t *testing.T) {
	path := writeLog(t, buildLines(2500))
	seen := map[string]bool{}
	before := int64(-1)
	pages := 0
	for {
		page, err := ReadLogBefore(path, before, LogQuery{Limit: 333})
		if err != nil {
			t.Fatal(err)
		}
		pages++
		for i, entry := range page.Entries {
			if seen[entry.Message] {
				t.Fatalf("duplicate %q", entry.Message)
			}
			seen[entry.Message] = true
			if i > 0 && page.Entries[i-1].Offset >= entry.Offset {
				t.Fatal("entries not in ascending order")
			}
		}
		if !page.HasMore {
			break
		}
		before = page.NextBefore
	}
	if len(seen) != 2500 || pages != 8 {
		t.Fatalf("seen=%d pages=%d", len(seen), pages)
	}
}

func TestReadLogBeforeScanBudgetKeepsValidCursor(t *testing.T) {
	// A sparse filter must be able to page past an entire scan window. Use
	// variable-length lines so the scan boundary falls inside an unread line.
	var content strings.Builder
	content.WriteString("2026/10/23 08:00:00 x.go:1: [ERROR] oldest target\n")
	for i := 0; content.Len() < readerMaxScanBytes+readerChunkBytes; i++ {
		fmt.Fprintf(&content, "2026/10/23 08:00:00 x.go:1: [INFO] ordinary line %d\n", i)
	}
	path := writeLog(t, content.String())
	q := LogQuery{Levels: map[string]bool{"ERROR": true}}
	page, err := ReadLogBefore(path, -1, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 0 || !page.HasMore || page.NextBefore >= page.Cursor {
		t.Fatalf("expected an empty page with progress: %+v", page)
	}
	boundary := page.NextBefore
	if content.String()[boundary-1] != '\n' {
		t.Fatalf("cursor %d is not at a line boundary", boundary)
	}

	// The line straddling the scan boundary must not be dropped either.
	prefix := content.String()[:boundary-1]
	lineStart := strings.LastIndexByte(prefix, '\n') + 1
	boundaryLine := prefix[lineStart:]
	boundaryPage, err := ReadLogBefore(path, boundary, LogQuery{Limit: 1})
	if err != nil || boundaryPage.Reset || len(boundaryPage.Entries) != 1 ||
		boundaryPage.Entries[0].Raw != boundaryLine {
		t.Fatalf("lost boundary line: page=%+v err=%v", boundaryPage, err)
	}

	page, err = ReadLogBefore(path, boundary, q)
	if err != nil || page.Reset || page.HasMore || len(page.Entries) != 1 ||
		page.Entries[0].Message != "oldest target" {
		t.Fatalf("failed to reach old match: page=%+v err=%v", page, err)
	}
}

func TestReadLogFilters(t *testing.T) {
	path := writeLog(t, buildLines(100)+"2026/10/23 08:00:01 h.go:2: [INFO] event=\"http.request.started\" path=\"/x\"\n")
	page, err := ReadLogBefore(path, -1, LogQuery{Levels: map[string]bool{"ERROR": true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 10 || page.Entries[9].Message != "line 0090" {
		t.Fatalf("level filter: %d entries", len(page.Entries))
	}
	page, _ = ReadLogBefore(path, -1, LogQuery{Search: "LINE 004"})
	if len(page.Entries) != 10 {
		t.Fatalf("search: %d entries", len(page.Entries))
	}
	page, _ = ReadLogBefore(path, -1, LogQuery{ExcludeEventPrefixes: []string{"http.request."}, Limit: 1})
	if len(page.Entries) != 1 || page.Entries[0].Message != "line 0099" {
		t.Fatalf("exclude: %+v", page.Entries)
	}
}

func TestReadLogFollowSkipsPartialLineAndDetectsReset(t *testing.T) {
	path := writeLog(t, buildLines(3)+"2026/10/23 08:00:00 x.go:1: [INFO] partial")
	page, err := ReadLogBefore(path, -1, LogQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 3 {
		t.Fatalf("partial line should be excluded, got %d", len(page.Entries))
	}
	cursor := page.Cursor

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(" done\n2026/10/23 08:00:02 x.go:1: [WARN] next\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	page, err = ReadLogAfter(path, cursor, LogQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 2 || page.Entries[0].Message != "partial done" || page.Entries[1].Level != "WARN" {
		t.Fatalf("follow entries: %+v", page.Entries)
	}
	if page.Reset {
		t.Fatal("unexpected reset")
	}
	page, _ = ReadLogAfter(path, page.Cursor, LogQuery{})
	if len(page.Entries) != 0 {
		t.Fatalf("expected no new entries, got %d", len(page.Entries))
	}

	if err := os.WriteFile(path, []byte(buildLines(1)), 0o644); err != nil {
		t.Fatal(err)
	}
	page, err = ReadLogAfter(path, cursor, LogQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if !page.Reset || len(page.Entries) != 1 {
		t.Fatalf("expected reset to tail: %+v", page)
	}
	// A cursor that is not on a line boundary is also stale.
	page, _ = ReadLogAfter(path, 5, LogQuery{})
	if !page.Reset {
		t.Fatal("mid-line cursor should reset")
	}
}

func TestReadLogMissingFileIsEmpty(t *testing.T) {
	page, err := ReadLogBefore(filepath.Join(t.TempDir(), "none.log"), -1, LogQuery{})
	if err != nil || len(page.Entries) != 0 || page.HasMore {
		t.Fatalf("page=%+v err=%v", page, err)
	}
}

func TestReadLogTruncatesHugeLines(t *testing.T) {
	path := writeLog(t, "2026/10/23 08:00:00 x.go:1: [INFO] "+strings.Repeat("界", readerMaxLineBytes)+"\n")
	page, err := ReadLogBefore(path, -1, LogQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 1 || !page.Entries[0].Truncated || len(page.Entries[0].Raw) > readerMaxLineBytes {
		t.Fatalf("huge line not truncated: %d", len(page.Entries))
	}
}
