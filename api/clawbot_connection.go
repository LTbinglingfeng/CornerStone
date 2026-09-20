package api

import (
	"context"
	"strings"
	"time"
)

// A stale credential must not be retried by saving unrelated settings. A new
// credential (or a successful QR login) is required to resume the channel.
func (s *ClawBotService) markTokenStale(token string) {
	token = strings.TrimSpace(token)
	s.mu.Lock()
	if token == "" || strings.TrimSpace(s.handler.configManager.GetClawBotConfig().BotToken) != token {
		s.mu.Unlock()
		return
	}
	s.staleToken = token
	s.connected = false
	s.workerRunning = false
	s.lastError = "ClawBot token expired (-14); scan QR code to sign in again"
	s.lastErrorAt = time.Now()
	cancel := s.workerCancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func clawBotRetryDelay(failures int) time.Duration {
	if failures < 1 {
		failures = 1
	}
	if failures > 5 {
		return 30 * time.Second
	}
	delay := 2 * time.Second * time.Duration(1<<uint(failures-1))
	if delay > 30*time.Second {
		return 30 * time.Second
	}
	return delay
}

func waitClawBotRetry(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
