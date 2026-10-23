package logging

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// The log file is append-only and unrotated, so every read is bounded: a page
// never scans more than readerMaxScanBytes and never returns oversized lines.
const (
	readerChunkBytes      = 64 << 10
	readerMaxScanBytes    = 8 << 20
	readerMaxFollowBytes  = 4 << 20
	readerMaxLineBytes    = 16 << 10
	ReaderDefaultLimit    = 200
	ReaderMaxLimit        = 1000
	readerTimestampLayout = "2006/01/02 15:04:05"
)

// LogField is one key=value pair parsed from a log line, in source order.
type LogField struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// LogEntry is a parsed log line. Offset is the byte offset of the line start
// and doubles as a stable identifier and pagination cursor.
type LogEntry struct {
	Offset    int64      `json:"offset"`
	Time      string     `json:"time,omitempty"`
	Level     string     `json:"level,omitempty"`
	Source    string     `json:"source,omitempty"`
	Event     string     `json:"event,omitempty"`
	Message   string     `json:"message,omitempty"`
	Fields    []LogField `json:"fields,omitempty"`
	Raw       string     `json:"raw"`
	Truncated bool       `json:"truncated,omitempty"`
}

// LogQuery filters lines. Empty Levels means every level, including lines
// without a recognisable header (e.g. multi-line continuations).
type LogQuery struct {
	Levels               map[string]bool
	Search               string
	ExcludeEventPrefixes []string
	Limit                int
}

// LogPage is returned oldest → newest.
type LogPage struct {
	Entries []LogEntry `json:"entries"`
	// Cursor is the offset just after the last complete line; pass it as
	// `after` to receive lines appended later.
	Cursor int64 `json:"cursor"`
	// NextBefore is the offset to pass as `before` to page further back.
	NextBefore int64 `json:"next_before"`
	HasMore    bool  `json:"has_more"`
	Size       int64 `json:"size"`
	// Reset reports that the supplied cursor no longer matched the file
	// (truncated or replaced) and the page restarted from the tail.
	Reset bool `json:"reset,omitempty"`
	// Gap reports that a follow read skipped lines to stay bounded.
	Gap bool `json:"gap,omitempty"`
}

func (q LogQuery) normalized() LogQuery {
	if q.Limit <= 0 {
		q.Limit = ReaderDefaultLimit
	}
	if q.Limit > ReaderMaxLimit {
		q.Limit = ReaderMaxLimit
	}
	q.Search = strings.ToLower(strings.TrimSpace(q.Search))
	return q
}

func (q LogQuery) matches(entry *LogEntry, rawLower string) bool {
	if len(q.Levels) > 0 && !q.Levels[entry.Level] {
		return false
	}
	for _, prefix := range q.ExcludeEventPrefixes {
		if prefix != "" && strings.HasPrefix(entry.Event, prefix) {
			return false
		}
	}
	return q.Search == "" || strings.Contains(rawLower, q.Search)
}

// ReadLogBefore returns up to q.Limit matching lines that end at or before
// `before`. A negative `before` reads from the end of the file.
func ReadLogBefore(path string, before int64, q LogQuery) (LogPage, error) {
	q = q.normalized()
	f, size, err := openLog(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return LogPage{Entries: []LogEntry{}}, nil
		}
		return LogPage{}, err
	}
	defer f.Close()

	tailEnd, err := completeLinesEnd(f, size)
	if err != nil {
		return LogPage{}, err
	}
	page := LogPage{Size: size, Cursor: tailEnd}
	end := tailEnd
	if before >= 0 {
		if before > tailEnd || !atLineStart(f, before) {
			page.Reset = true
		} else {
			end = before
		}
	}
	entries, nextBefore, err := scanBackward(f, end, q)
	if err != nil {
		return LogPage{}, err
	}
	page.Entries = entries
	page.NextBefore = nextBefore
	page.HasMore = nextBefore > 0
	return page, nil
}

// ReadLogAfter returns matching lines appended at or after `after`. When the
// cursor is stale it falls back to the tail and sets Reset.
func ReadLogAfter(path string, after int64, q LogQuery) (LogPage, error) {
	q = q.normalized()
	f, size, err := openLog(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return LogPage{Entries: []LogEntry{}, Reset: after > 0}, nil
		}
		return LogPage{}, err
	}
	if after < 0 || after > size || !atLineStart(f, after) {
		f.Close()
		page, err := ReadLogBefore(path, -1, q)
		page.Reset = true
		return page, err
	}
	defer f.Close()

	end, err := completeLinesEnd(f, size)
	if err != nil {
		return LogPage{}, err
	}
	page := LogPage{Entries: []LogEntry{}, Size: size, Cursor: end, NextBefore: after, HasMore: after > 0}
	if end <= after {
		page.Cursor = after
		return page, nil
	}
	start := after
	if end-start > readerMaxFollowBytes {
		// Too far behind: jump to a line boundary near the tail.
		start, err = nextLineStart(f, end-readerMaxFollowBytes, end)
		if err != nil {
			return LogPage{}, err
		}
		page.Gap = true
	}
	buf := make([]byte, end-start)
	if _, err := f.ReadAt(buf, start); err != nil && !errors.Is(err, io.EOF) {
		return LogPage{}, err
	}
	offset := start
	for len(buf) > 0 {
		i := bytes.IndexByte(buf, '\n')
		if i < 0 {
			break
		}
		if entry, ok := matchLine(buf[:i], offset, q); ok {
			page.Entries = append(page.Entries, entry)
		}
		offset += int64(i) + 1
		buf = buf[i+1:]
	}
	if len(page.Entries) > q.Limit {
		page.Entries = append([]LogEntry(nil), page.Entries[len(page.Entries)-q.Limit:]...)
		page.Gap = true
	}
	return page, nil
}

func openLog(path string) (*os.File, int64, error) {
	if path == "" {
		return nil, 0, fs.ErrNotExist
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, info.Size(), nil
}

// completeLinesEnd excludes a trailing partially written line so follow
// cursors always land on line boundaries.
func completeLinesEnd(f io.ReaderAt, size int64) (int64, error) {
	pos := size
	for pos > 0 && size-pos < readerMaxScanBytes {
		n := min(int64(readerChunkBytes), pos)
		buf := make([]byte, n)
		if _, err := f.ReadAt(buf, pos-n); err != nil && !errors.Is(err, io.EOF) {
			return 0, err
		}
		if i := bytes.LastIndexByte(buf, '\n'); i >= 0 {
			return pos - n + int64(i) + 1, nil
		}
		pos -= n
	}
	if pos == 0 {
		return 0, nil
	}
	return size, nil
}

func atLineStart(f io.ReaderAt, offset int64) bool {
	if offset == 0 {
		return true
	}
	var b [1]byte
	if _, err := f.ReadAt(b[:], offset-1); err != nil {
		return false
	}
	return b[0] == '\n'
}

func nextLineStart(f io.ReaderAt, from, limit int64) (int64, error) {
	if from <= 0 || atLineStart(f, from) {
		return max(from, 0), nil
	}
	for pos := from; pos < limit; {
		n := min(int64(readerChunkBytes), limit-pos)
		buf := make([]byte, n)
		if _, err := f.ReadAt(buf, pos); err != nil && !errors.Is(err, io.EOF) {
			return 0, err
		}
		if i := bytes.IndexByte(buf, '\n'); i >= 0 {
			return pos + int64(i) + 1, nil
		}
		pos += n
	}
	return limit, nil
}

// scanBackward walks complete lines ending at `end` from newest to oldest.
// It returns entries oldest → newest and the start offset of the oldest line
// examined, so the next page can continue exactly where this one stopped.
func scanBackward(f io.ReaderAt, end int64, q LogQuery) ([]LogEntry, int64, error) {
	reversed := make([]LogEntry, 0, q.Limit)
	pos := end
	var pending []byte // bytes [pos, lineEnd) not yet terminated on the left
	scanned := int64(0)
	for pos > 0 && scanned < readerMaxScanBytes {
		n := min(int64(readerChunkBytes), pos)
		chunk := make([]byte, n, n+int64(len(pending)))
		if _, err := f.ReadAt(chunk, pos-n); err != nil && !errors.Is(err, io.EOF) {
			return nil, 0, err
		}
		pos -= n
		scanned += n
		data := append(chunk, pending...)
		for {
			i := bytes.LastIndexByte(data, '\n')
			if i < 0 {
				break
			}
			lineStart := pos + int64(i) + 1
			if entry, ok := matchLine(data[i+1:], lineStart, q); ok {
				reversed = append(reversed, entry)
				if len(reversed) >= q.Limit {
					return reverseEntries(reversed), lineStart, nil
				}
			}
			data = data[:i]
		}
		pending = append(pending[:0], data...)
	}
	if pos == 0 {
		if entry, ok := matchLine(pending, 0, q); ok {
			reversed = append(reversed, entry)
		}
		return reverseEntries(reversed), 0, nil
	}
	// The chunk boundary may be in the middle of an unread line. Resume at
	// that line's end (just after its newline), not at pos: the next page must
	// reread the entire line, and ReadLogBefore requires a line-boundary cursor.
	return reverseEntries(reversed), pos + int64(len(pending)) + 1, nil
}

func reverseEntries(entries []LogEntry) []LogEntry {
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	return entries
}

func matchLine(line []byte, offset int64, q LogQuery) (LogEntry, bool) {
	line = bytes.TrimRight(line, "\r")
	if len(bytes.TrimSpace(line)) == 0 {
		return LogEntry{}, false
	}
	raw := strings.ToValidUTF8(string(line), "\uFFFD")
	var rawLower string
	if q.Search != "" {
		rawLower = strings.ToLower(raw)
	}
	truncated := false
	if len(raw) > readerMaxLineBytes {
		cut := readerMaxLineBytes
		for cut > 0 && !utf8.RuneStart(raw[cut]) {
			cut--
		}
		raw = raw[:cut]
		truncated = true
	}
	entry := ParseLogLine(raw)
	entry.Offset = offset
	entry.Truncated = truncated
	if !q.matches(&entry, rawLower) {
		return LogEntry{}, false
	}
	return entry, true
}

var (
	logHeaderRe = regexp.MustCompile(`^(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}(?:\.\d+)?) (?:(\S+:\d+): )?\[([A-Z]+)\] ?`)
	logFieldRe  = regexp.MustCompile(`(?:^|[ \t])([A-Za-z_][A-Za-z0-9_.\-]*)=`)
)

// ParseLogLine understands both the legacy "message: k=v" lines and the
// structured `event="..." k="v"` lines. Unrecognised lines keep only Raw.
func ParseLogLine(raw string) LogEntry {
	entry := LogEntry{Raw: raw}
	m := logHeaderRe.FindStringSubmatchIndex(raw)
	if m == nil {
		entry.Message = raw
		return entry
	}
	stamp := raw[m[2]:m[3]]
	if ts, err := time.ParseInLocation(readerTimestampLayout, stamp, time.Local); err == nil {
		entry.Time = ts.Format(time.RFC3339Nano)
	}
	if m[4] >= 0 {
		entry.Source = raw[m[4]:m[5]]
	}
	entry.Level = raw[m[6]:m[7]]
	body := raw[m[1]:]
	entry.Message, entry.Fields = parseLogBody(body)
	if strings.HasPrefix(body, `event="`) && len(entry.Fields) > 0 && entry.Fields[0].Key == "event" {
		entry.Event = entry.Fields[0].Value
		entry.Fields = entry.Fields[1:]
	}
	return entry
}

func parseLogBody(body string) (string, []LogField) {
	matches := logFieldRe.FindAllStringSubmatchIndex(body, -1)
	if len(matches) == 0 {
		return strings.TrimSpace(body), nil
	}
	message := strings.TrimSpace(body[:matches[0][0]])
	message = strings.TrimSpace(strings.TrimSuffix(message, ":"))
	fields := make([]LogField, 0, len(matches))
	consumed := 0
	for i := 0; i < len(matches); i++ {
		if matches[i][0] < consumed {
			continue
		}
		key := body[matches[i][2]:matches[i][3]]
		valueStart := matches[i][1]
		if strings.HasPrefix(body[valueStart:], `"`) {
			if quoted, err := strconv.QuotedPrefix(body[valueStart:]); err == nil {
				if value, err := strconv.Unquote(quoted); err == nil {
					fields = append(fields, LogField{Key: key, Value: value})
					consumed = valueStart + len(quoted)
					continue
				}
			}
		}
		valueEnd := len(body)
		for j := i + 1; j < len(matches); j++ {
			if matches[j][0] >= valueStart {
				valueEnd = matches[j][0]
				break
			}
		}
		fields = append(fields, LogField{Key: key, Value: strings.TrimSpace(body[valueStart:valueEnd])})
		consumed = valueEnd
	}
	return message, fields
}
