package storage

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"cornerstone/logging"
)

type MemorySession struct {
	promptID   string
	sessionID  string
	roundCount int

	mm        *MemoryManager
	extractor *MemoryExtractor

	lastAccess time.Time
	mu         sync.Mutex
}

// PromptID identifies the immutable persona used by this memory session.
func (s *MemorySession) PromptID() string { return s.promptID }

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
	logging.Infof("memory session created: prompt=%s session=%s", promptID, sessionID)
	return s
}

func (s *MemorySession) GetActiveMemories() []Memory {
	s.mu.Lock()
	s.touch()
	s.mu.Unlock()
	// MemoryManager owns the only cache. Read a fresh snapshot for each prompt,
	// so edits, deletions, other sessions' writes and decay are visible immediately.
	return s.mm.GetActiveMemories(s.promptID)
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
			logging.Errorf("记忆提取失败 session=%s: %v", s.sessionID, errRefresh)
		}
	}()
}

// RefreshNow retains the existing round-counter behavior for chat tool callers.
// There is no session-local memory snapshot to refresh anymore.
func (s *MemorySession) RefreshNow() {
	s.mu.Lock()
	s.roundCount = 0
	s.touch()
	s.mu.Unlock()
}

// refreshWithExtractRetry 带重试的记忆提取
func (s *MemorySession) refreshWithExtractRetry(maxRetries int) error {
	if s.extractor == nil {
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
			// A missing update target may have been deliberately deleted. Do not
			// immediately ask the model again and let it turn that update into an add.
			if errors.Is(errExtract, errMemoryMatchingIDNotFound) || errors.Is(errExtract, ErrInvalidID) {
				return fmt.Errorf("记忆提取结果被拒绝: %w", errExtract)
			}
			lastErr = errExtract
			continue
		}

		return nil
	}

	return fmt.Errorf("提取失败（重试%d次）: %w", maxRetries, lastErr)
}

func (s *MemorySession) refreshWithExtract() error {
	return s.refreshWithExtractRetry(0)
}
