package storage

import (
	"bufio"
	"bytes"
	"cornerstone/client"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const statisticsMaxLine = 64 * 1024

// StatisticsQuery uses [From, To); zero endpoints are unbounded.
type StatisticsQuery struct {
	From       time.Time `json:"from"`
	To         time.Time `json:"to"`
	ProviderID string    `json:"provider_id"`
	Model      string    `json:"model"`
}

type StatisticsTotals struct {
	Requests              int64    `json:"requests"`
	SuccessfulRequests    int64    `json:"successful_requests"`
	FailedRequests        int64    `json:"failed_requests"`
	UsageReportedRequests int64    `json:"usage_reported_requests"`
	UnknownUsageRequests  int64    `json:"unknown_usage_requests"`
	InputTokens           int64    `json:"input_tokens"`
	OutputTokens          int64    `json:"output_tokens"`
	TotalTokens           int64    `json:"total_tokens"`
	CacheReadTokens       int64    `json:"cache_read_tokens"`
	CacheCreationTokens   int64    `json:"cache_creation_tokens"`
	ReasoningTokens       int64    `json:"reasoning_tokens"`
	DurationMS            int64    `json:"duration_ms"`
	CacheHitRate          *float64 `json:"cache_hit_rate"`
	UsageCoverage         *float64 `json:"usage_coverage"`
}

type StatisticsRealtime struct {
	WindowSeconds         int      `json:"window_seconds"`
	RPM                   int64    `json:"rpm"`
	TPM                   int64    `json:"tpm"`
	UsageReportedRequests int64    `json:"usage_reported_requests"`
	UnknownUsageRequests  int64    `json:"unknown_usage_requests"`
	UsageCoverage         *float64 `json:"usage_coverage"`
}

type StatisticsGroup struct {
	ProviderID string           `json:"provider_id"`
	Model      string           `json:"model"`
	Totals     StatisticsTotals `json:"totals"`
}

type StatisticsSummary struct {
	Filters           StatisticsQuery    `json:"filters"`
	AsOf              time.Time          `json:"as_of"`
	Totals            StatisticsTotals   `json:"totals"`
	Groups            []StatisticsGroup  `json:"groups"`
	Realtime          StatisticsRealtime `json:"realtime"`
	DataComplete      bool               `json:"data_complete"`
	WriteFailures     uint64             `json:"write_failures"`
	WriteFailureScope string             `json:"write_failure_scope"`
}

// StatisticsManager requires one instance/process per application-owned directory.
// Failure counters are in-memory: a failed disk cannot reliably persist them.
// DataComplete describes scanned data and failures observed by this instance,
// not calls lost before startup. Query errors must never be treated as totals.
type StatisticsManager struct {
	mu            sync.RWMutex
	dir           string
	writeFailures uint64
	appendLine    func(*os.File, []byte) (int, error) // fault-injection seam; set before use
}

func NewStatisticsManager(dir string) (*StatisticsManager, error) {
	if dir == "" {
		return nil, errors.New("empty statistics directory")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &StatisticsManager{dir: dir, appendLine: func(f *os.File, b []byte) (int, error) { return f.Write(b) }}, nil
}

func validStatisticsRecord(r client.CallRecord) bool {
	if r.CompletedAt.IsZero() || r.ProviderID == "" || r.Model == "" {
		return false
	}
	for _, n := range []int64{r.InputTokens, r.OutputTokens, r.TotalTokens, r.CacheReadTokens, r.CacheCreationTokens, r.ReasoningTokens, r.DurationMS} {
		if n < 0 {
			return false
		}
	}
	return true
}

// RecordCall synchronously commits one logical call, including failed calls.
// On append/sync failure it truncates back to the original offset. An interrupted
// process may leave a partial line; subsequent writes refuse that file rather
// than silently deleting evidence or joining the next record to it.
func (m *StatisticsManager) RecordCall(r client.CallRecord) (err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	defer func() {
		if err != nil {
			m.writeFailures++
		}
	}()
	if !validStatisticsRecord(r) {
		return errors.New("invalid statistics record")
	}
	r.CompletedAt = r.CompletedAt.UTC()
	data, err := json.Marshal(r)
	if err != nil {
		return errors.New("encode statistics record failed")
	}
	data = append(data, '\n')
	if len(data) >= statisticsMaxLine {
		return errors.New("statistics record too large")
	}
	path := filepath.Join(m.dir, r.CompletedAt.Format("2006-01-02")+".jsonl")
	if err = statisticsRegularFile(path, true); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	if size > 0 {
		var last [1]byte
		if _, err = f.ReadAt(last[:], size-1); err != nil {
			return err
		}
		if last[0] != '\n' {
			return errors.New("statistics file has incomplete final line; repair required")
		}
	}
	n, err := m.appendLine(f, data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = f.Sync()
	}
	if err != nil {
		rollbackErr := f.Truncate(size)
		syncErr := f.Sync()
		return errors.Join(err, rollbackErr, syncErr)
	}
	return nil
}

func statisticsRegularFile(path string, missingOK bool) error {
	info, err := os.Lstat(path)
	if missingOK && os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("statistics path is not a regular file")
	}
	return nil
}

func (t *StatisticsTotals) add(r client.CallRecord) {
	t.Requests++
	if r.Success {
		t.SuccessfulRequests++
	} else {
		t.FailedRequests++
	}
	t.DurationMS += r.DurationMS
	if !r.UsageReported {
		t.UnknownUsageRequests++
		return
	}
	t.UsageReportedRequests++
	t.InputTokens += r.InputTokens
	t.OutputTokens += r.OutputTokens
	t.TotalTokens += r.TotalTokens
	t.CacheReadTokens += r.CacheReadTokens
	t.CacheCreationTokens += r.CacheCreationTokens
	t.ReasoningTokens += r.ReasoningTokens
}
func statisticsRatio(a, b int64) *float64 {
	if b == 0 {
		return nil
	}
	v := float64(a) / float64(b)
	if v > 1 {
		v = 1
	}
	return &v
}
func (t *StatisticsTotals) finish() {
	t.CacheHitRate = statisticsRatio(t.CacheReadTokens, t.InputTokens)
	t.UsageCoverage = statisticsRatio(t.UsageReportedRequests, t.Requests)
}
func statisticsWithin(t, from, to time.Time) bool {
	return (from.IsZero() || !t.Before(from)) && (to.IsZero() || t.Before(to))
}
func statisticsDayOverlaps(day, from, to time.Time) bool {
	return (from.IsZero() || day.AddDate(0, 0, 1).After(from)) && (to.IsZero() || day.Before(to)) && (from.IsZero() || to.IsZero() || from.Before(to))
}

// Query scans only UTC day files overlapping history or the independent realtime
// window [now-60s,now). Memory is bounded by one record plus distinct groups.
func (m *StatisticsManager) Query(q StatisticsQuery, now time.Time) (s StatisticsSummary, err error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s = StatisticsSummary{Filters: q, AsOf: now, Groups: []StatisticsGroup{}, WriteFailures: m.writeFailures, WriteFailureScope: "manager_lifetime", DataComplete: m.writeFailures == 0, Realtime: StatisticsRealtime{WindowSeconds: 60}}
	defer func() {
		if err != nil {
			s.DataComplete = false
		}
	}()
	if now.IsZero() {
		return s, errors.New("statistics query requires now")
	}
	if !q.From.IsZero() && !q.To.IsZero() && q.From.After(q.To) {
		return s, errors.New("statistics from exceeds to")
	}
	start := now.Add(-60 * time.Second)
	groups := map[[2]string]*StatisticsTotals{}
	var realtime StatisticsTotals
	// ReadDir in batches avoids retaining the entire historical directory listing.
	dir, err := os.Open(m.dir)
	if err != nil {
		return s, err
	}
	defer dir.Close()
	for {
		entries, readErr := dir.ReadDir(128)
		for _, e := range entries {
			day, parseErr := time.Parse("2006-01-02.jsonl", e.Name())
			if parseErr != nil {
				continue
			}
			if !statisticsDayOverlaps(day, q.From, q.To) && !statisticsDayOverlaps(day, start, now) {
				continue
			}
			err = m.scan(e.Name(), func(r client.CallRecord) {
				if q.ProviderID != "" && q.ProviderID != r.ProviderID || q.Model != "" && q.Model != r.Model {
					return
				}
				if statisticsWithin(r.CompletedAt, start, now) {
					realtime.add(r)
				}
				if !statisticsWithin(r.CompletedAt, q.From, q.To) {
					return
				}
				s.Totals.add(r)
				key := [2]string{r.ProviderID, r.Model}
				if groups[key] == nil {
					groups[key] = &StatisticsTotals{}
				}
				groups[key].add(r)
			})
			if err != nil {
				return s, err
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return s, readErr
		}
	}
	s.Totals.finish()
	for k, t := range groups {
		t.finish()
		s.Groups = append(s.Groups, StatisticsGroup{ProviderID: k[0], Model: k[1], Totals: *t})
	}
	sort.Slice(s.Groups, func(i, j int) bool {
		a, b := s.Groups[i], s.Groups[j]
		if a.ProviderID != b.ProviderID {
			return a.ProviderID < b.ProviderID
		}
		return a.Model < b.Model
	})
	s.Realtime.RPM = realtime.Requests
	s.Realtime.TPM = realtime.TotalTokens
	s.Realtime.UsageReportedRequests = realtime.UsageReportedRequests
	s.Realtime.UnknownUsageRequests = realtime.UnknownUsageRequests
	s.Realtime.UsageCoverage = statisticsRatio(realtime.UsageReportedRequests, realtime.Requests)
	return s, nil
}

func (m *StatisticsManager) scan(name string, visit func(client.CallRecord)) error {
	path := filepath.Join(m.dir, name)
	if err := statisticsRegularFile(path, false); err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	reader := bufio.NewReaderSize(f, statisticsMaxLine)
	for line := 1; ; line++ {
		data, err := reader.ReadSlice('\n')
		if err == io.EOF && len(data) == 0 {
			return nil
		}
		if err != nil {
			return fmt.Errorf("statistics %s line %d: incomplete, oversized or unreadable record", name, line)
		}
		var r client.CallRecord
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&r); err != nil {
			return fmt.Errorf("statistics %s line %d: invalid record", name, line)
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF || !validStatisticsRecord(r) || r.CompletedAt.UTC().Format("2006-01-02.jsonl") != name {
			return fmt.Errorf("statistics %s line %d: invalid record", name, line)
		}
		// Require all stored fields: missing usage_reported must not turn a damaged
		// record into an apparently valid unknown-usage call.
		var fields map[string]json.RawMessage
		if json.Unmarshal(data, &fields) != nil || len(fields) != 12 {
			return fmt.Errorf("statistics %s line %d: incomplete schema", name, line)
		}
		for _, v := range fields {
			if bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
				return fmt.Errorf("statistics %s line %d: null field", name, line)
			}
		}
		visit(r)
	}
}
