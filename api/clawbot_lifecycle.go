package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/config"
	"cornerstone/logging"
	"time"
)

func (s *ClawBotService) runWorker(ctx context.Context, cfg config.ClawBotConfig, done chan struct{}) {
	defer close(done)
	defer func() {
		s.mu.Lock()
		if s.workerCtx == ctx {
			s.workerRunning = false
			s.connected = false
		}
		s.mu.Unlock()
	}()
	if ctx.Err() != nil {
		return
	}
	if err := s.client.NotifyStart(ctx, cfg.BaseURL, cfg.BotToken, s.wechatUIN); err != nil {
		if client.IsClawBotStaleToken(err) {
			s.markTokenStale(cfg.BotToken)
			return
		}
		if ctx.Err() == nil {
			logging.Warnf("clawbot notify start failed: %v", err)
		}
	}
	defer func() {
		s.mu.RLock()
		stale := s.staleToken == cfg.BotToken
		s.mu.RUnlock()
		if stale {
			return
		}
		// Worker cancellation must not cancel its own offline notification.
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.client.NotifyStop(stopCtx, cfg.BaseURL, cfg.BotToken, s.wechatUIN); err != nil {
			logging.Warnf("clawbot notify stop failed: %v", err)
		}
	}()
	if s.stateManager != nil {
		deliveryCtx, cancel := context.WithCancel(ctx)
		deliveryDone := make(chan struct{})
		go func() { defer close(deliveryDone); s.runDeliveryLoop(deliveryCtx, cfg) }()
		defer func() { cancel(); <-deliveryDone }()
	}
	s.pollLoop(ctx, cfg)
}

func clawBotPollTimeout(serverMS int, previous time.Duration) time.Duration {
	if serverMS <= 0 {
		return previous
	}
	// Bound untrusted suggestions before converting to Duration (and avoid
	// tiny timeouts creating a tight request loop).
	if serverMS < 1000 {
		return time.Second
	}
	if serverMS > 120000 {
		return 2 * time.Minute
	}
	return time.Duration(serverMS) * time.Millisecond
}
