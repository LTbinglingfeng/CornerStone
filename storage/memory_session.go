package storage

import (
	"fmt"
	"sync"
	"time"

	"cornerstone/logging"
)

type MemorySession struct {
	promptID   string
	sessionID  string
	roundCount int

	activeMemories []Memory

	mm        *MemoryManager
	extractor *MemoryExtractor

	lastAccess time.Time
	mu         sync.Mutex
}

// LastAccess 返回最后访问时间
func (s *MemorySession) LastAccess() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastAccess
}

// touch 更新最后访问时间
func (s *MemorySession) touch() {
	s.lastAccess = time.Now()
}

func NewMemorySession(promptID, sessionID string, mm *MemoryManager, extractor *MemoryExtractor) *MemorySession {
	s := &MemorySession{
		promptID:   promptID,
		sessionID:  sessionID,
		mm:         mm,
		extractor:  extractor,
		lastAccess: time.Now(),
	}
	s.refresh()
	logging.Infof("memory session created: prompt=%s session=%s", promptID, sessionID)
	return s
}

func (s *MemorySession) GetActiveMemories() []Memory {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.touch()
	return append([]Memory(nil), s.activeMemories...)
}

func (s *MemorySession) OnRoundComplete() {
	s.mu.Lock()
	s.roundCount++
	refreshInterval := RefreshInterval
	if s.extractor != nil {
		refreshInterval = s.extractor.GetRefreshInterval()
	}
	shouldRefresh := s.roundCount >= refreshInterval
	if shouldRefresh {
		s.roundCount = 0
	}
	s.mu.Unlock()

	if !shouldRefresh {
		return
	}

	go func() {
		if errRefresh := s.refreshWithExtractRetry(1); errRefresh != nil {
			logging.Errorf("记忆刷新失败（已重试）session=%s: %v", s.sessionID, errRefresh)
		}
	}()
}

func (s *MemorySession) RefreshNow() {
	s.mu.Lock()
	s.roundCount = 0
	s.touch()
	s.mu.Unlock()

	s.refresh()
}

func (s *MemorySession) refresh() {
	active := s.mm.GetActiveMemories(s.promptID)
	s.mu.Lock()
	s.activeMemories = active
	s.mu.Unlock()
}

// refreshWithExtractRetry 带重试的记忆提取
func (s *MemorySession) refreshWithExtractRetry(maxRetries int) error {
	if s.extractor == nil {
		s.refresh()
		return nil
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// 重试前等待一小段时间
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
			logging.Infof("记忆提取重试 attempt=%d session=%s", attempt, s.sessionID)
		}

		if errExtract := s.extractor.ExtractAndSave(s.promptID, s.sessionID); errExtract != nil {
			lastErr = errExtract
			continue
		}

		// 成功
		s.refresh()
		return nil
	}

	return fmt.Errorf("提取失败（重试%d次）: %w", maxRetries, lastErr)
}

func (s *MemorySession) refreshWithExtract() error {
	return s.refreshWithExtractRetry(0)
}
