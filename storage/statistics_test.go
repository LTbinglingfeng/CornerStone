package storage

import (
	"cornerstone/client"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func statisticsTestManager(t *testing.T) (*StatisticsManager, time.Time) {
	t.Helper()
	m, err := NewStatisticsManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return m, time.Date(2026, 7, 2, 0, 0, 30, 0, time.UTC)
}
func statisticsTestRecord(at time.Time) client.CallRecord {
	return client.CallRecord{CompletedAt: at, ProviderID: "p", Model: "m", Success: true, UsageReported: true, InputTokens: 100, OutputTokens: 20, TotalTokens: 120, CacheReadTokens: 40, CacheCreationTokens: 10, ReasoningTokens: 5, DurationMS: 25}
}
func statisticsMustRecord(t *testing.T, m *StatisticsManager, r client.CallRecord) {
	t.Helper()
	if err := m.RecordCall(r); err != nil {
		t.Fatal(err)
	}
}
func statisticsMustQuery(t *testing.T, m *StatisticsManager, q StatisticsQuery, now time.Time) StatisticsSummary {
	t.Helper()
	s, err := m.Query(q, now)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func TestStatisticsRangeGroupsRestart(t *testing.T) {
	m, now := statisticsTestManager(t)
	for _, offset := range []time.Duration{-48 * time.Hour, -time.Hour, -30 * time.Second, 0} {
		statisticsMustRecord(t, m, statisticsTestRecord(now.Add(offset)))
	}
	r := statisticsTestRecord(now.Add(-time.Hour))
	r.ProviderID = "other"
	r.Success = false
	statisticsMustRecord(t, m, r)
	r.Model = "second"
	statisticsMustRecord(t, m, r)
	reloaded, err := NewStatisticsManager(m.dir)
	if err != nil {
		t.Fatal(err)
	}
	s := statisticsMustQuery(t, reloaded, StatisticsQuery{From: now.Add(-time.Hour), To: now}, now)
	if s.Totals.Requests != 4 || s.Totals.SuccessfulRequests != 2 || s.Totals.FailedRequests != 2 || len(s.Groups) != 3 || s.Realtime.RPM != 1 || !s.DataComplete {
		t.Fatalf("unexpected summary: %+v", s)
	}
	if s.Totals.TotalTokens != 480 || s.Totals.InputTokens != 400 || s.Totals.CacheCreationTokens != 40 || s.Totals.ReasoningTokens != 20 || s.Totals.DurationMS != 100 || *s.Totals.CacheHitRate != 0.4 {
		t.Fatalf("unexpected totals: %+v", s.Totals)
	}
	s = statisticsMustQuery(t, m, StatisticsQuery{ProviderID: "other", Model: "second"}, now)
	if s.Totals.Requests != 1 || len(s.Groups) != 1 || s.Realtime.RPM != 0 || s.Realtime.UsageCoverage != nil {
		t.Fatalf("filter: %+v", s)
	}
	s = statisticsMustQuery(t, m, StatisticsQuery{}, now)
	if s.Totals.Requests != 6 {
		t.Fatal("zero endpoints must be unbounded")
	}
	if _, err = m.Query(StatisticsQuery{From: now, To: now.Add(-time.Second)}, now); err == nil {
		t.Fatal("reversed range accepted")
	}
}
func TestStatisticsRealtimeBoundaries(t *testing.T) {
	m, now := statisticsTestManager(t)
	for _, d := range []time.Duration{-60*time.Second - time.Nanosecond, -60 * time.Second, -time.Nanosecond, 0, time.Nanosecond} {
		statisticsMustRecord(t, m, statisticsTestRecord(now.Add(d)))
	}
	r := statisticsTestRecord(now.Add(-time.Second))
	r.ProviderID = "other"
	statisticsMustRecord(t, m, r)
	s := statisticsMustQuery(t, m, StatisticsQuery{From: now.Add(-time.Hour), To: now.Add(-time.Minute), ProviderID: "p", Model: "m"}, now)
	if s.Totals.Requests != 1 || s.Realtime.RPM != 2 || s.Realtime.TPM != 240 || s.Realtime.WindowSeconds != 60 || *s.Realtime.UsageCoverage != 1 {
		t.Fatalf("boundary: %+v", s)
	}
	// UTC sharding, not the timestamp's local calendar date.
	r = statisticsTestRecord(now.Add(-time.Second).In(time.FixedZone("west", -8*3600)))
	statisticsMustRecord(t, m, r)
	if _, err := os.Stat(filepath.Join(m.dir, "2026-07-02.jsonl")); err != nil {
		t.Fatal(err)
	}
}
func TestStatisticsUnknownZeroAndCache(t *testing.T) {
	m, now := statisticsTestManager(t)
	r := statisticsTestRecord(now.Add(-time.Second))
	r.UsageReported = false
	r.Success = false
	statisticsMustRecord(t, m, r)
	r = client.CallRecord{CompletedAt: now.Add(-time.Second), ProviderID: "p", Model: "m", Success: true, UsageReported: true}
	statisticsMustRecord(t, m, r)
	s := statisticsMustQuery(t, m, StatisticsQuery{}, now)
	if s.Totals.UnknownUsageRequests != 1 || s.Totals.UsageReportedRequests != 1 || s.Totals.TotalTokens != 0 || s.Totals.CacheHitRate != nil || *s.Totals.UsageCoverage != 0.5 || s.Realtime.TPM != 0 || *s.Realtime.UsageCoverage != 0.5 {
		t.Fatalf("usage: %+v", s)
	}
	data, _ := json.Marshal(s)
	if !strings.Contains(string(data), `"cache_hit_rate":null`) {
		t.Fatal(string(data))
	}
	r.InputTokens = 1
	r.CacheReadTokens = 2
	statisticsMustRecord(t, m, r)
	s = statisticsMustQuery(t, m, StatisticsQuery{}, now)
	if *s.Totals.CacheHitRate != 1 {
		t.Fatal("cache rate not clamped")
	}
	s = statisticsMustQuery(t, m, StatisticsQuery{Model: "missing"}, now)
	if s.Totals.UsageCoverage != nil || s.Totals.CacheHitRate != nil || len(s.Groups) != 0 {
		t.Fatal("empty ratios")
	}
}
func TestStatisticsConcurrent(t *testing.T) {
	m, now := statisticsTestManager(t)
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			statisticsMustRecord(t, m, statisticsTestRecord(now.Add(-time.Second)))
			statisticsMustQuery(t, m, StatisticsQuery{}, now)
		}()
	}
	wg.Wait()
	s := statisticsMustQuery(t, m, StatisticsQuery{}, now)
	if s.Totals.Requests != 40 || s.Realtime.RPM != 40 {
		t.Fatal(s)
	}
}
func TestStatisticsCorruptFiles(t *testing.T) {
	for _, bad := range []string{"{\n", "null\n", "{}\n", "\n", "{", strings.Repeat("x", statisticsMaxLine) + "\n", `{"completed_at":"2026-07-02T00:00:00Z","provider_id":"p","model":"m"}` + "\n"} {
		t.Run("corrupt", func(t *testing.T) {
			m, now := statisticsTestManager(t)
			if err := os.WriteFile(filepath.Join(m.dir, "2026-07-02.jsonl"), []byte(bad), 0600); err != nil {
				t.Fatal(err)
			}
			s, err := m.Query(StatisticsQuery{ProviderID: "not-matched"}, now)
			if err == nil || s.DataComplete {
				t.Fatal("corruption silently ignored")
			}
		})
	}
	m, now := statisticsTestManager(t)
	// Unrelated historical files are not scanned for a bounded query.
	if err := os.WriteFile(filepath.Join(m.dir, "2020-01-01.jsonl"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	statisticsMustQuery(t, m, StatisticsQuery{From: now.Add(-time.Hour), To: now}, now)
	if _, err := m.Query(StatisticsQuery{}, now); err == nil {
		t.Fatal("unbounded query ignored corrupt history")
	}
}
func TestStatisticsWriteFailuresAndPartialAppend(t *testing.T) {
	m, now := statisticsTestManager(t)
	r := statisticsTestRecord(now.Add(-time.Second))
	statisticsMustRecord(t, m, r)
	original := m.appendLine
	m.appendLine = func(f *os.File, b []byte) (int, error) {
		n, err := f.Write(b[:len(b)/2])
		if err != nil {
			return n, err
		}
		return n, errors.New("injected disk failure")
	}
	if err := m.RecordCall(r); err == nil {
		t.Fatal("write succeeded")
	}
	m.appendLine = func(f *os.File, b []byte) (int, error) { return f.Write(b[:3]) }
	if err := m.RecordCall(r); err == nil {
		t.Fatal("short write accepted")
	}
	m.appendLine = original
	statisticsMustRecord(t, m, r)
	s := statisticsMustQuery(t, m, StatisticsQuery{}, now)
	if s.Totals.Requests != 2 || s.WriteFailures != 2 || s.DataComplete || s.WriteFailureScope != "manager_lifetime" {
		t.Fatalf("rollback/failure: %+v", s)
	}
	// Simulate interruption that left a partial line, then reopen the manager.
	path := filepath.Join(m.dir, "2026-07-02.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("{\"completed_at\":")
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m, err = NewStatisticsManager(m.dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.RecordCall(r); err == nil {
		t.Fatal("appended to partial line")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(before) {
		t.Fatal("corrupt tail modified")
	}
	if s, err = m.Query(StatisticsQuery{}, now); err == nil || s.DataComplete || s.WriteFailures != 1 {
		t.Fatal("partial line not exposed")
	}
}
func TestStatisticsOpenFailure(t *testing.T) {
	m, now := statisticsTestManager(t)
	path := filepath.Join(m.dir, "2026-07-02.jsonl")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := m.RecordCall(statisticsTestRecord(now)); err == nil {
		t.Fatal("directory accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	s := statisticsMustQuery(t, m, StatisticsQuery{}, now)
	if s.WriteFailures != 1 || s.DataComplete {
		t.Fatal("write failure hidden")
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Skip(err)
	}
	if err := m.RecordCall(statisticsTestRecord(now)); err == nil {
		t.Fatal("symlink accepted")
	}
	if _, err := m.Query(StatisticsQuery{}, now); err == nil {
		t.Fatal("symlink read")
	}
}
