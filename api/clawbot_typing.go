package api

import (
	"context"
	"cornerstone/config"
	"crypto/sha256"
	"sync"
	"time"
)

const clawBotTypingTTL = 24 * time.Hour
const clawBotTypingInterval = 8 * time.Second

type clawBotTypingCache struct {
	ticket, context          string
	expires, retryAt, sendAt time.Time
	backoff                  time.Duration
}
type clawBotTypingRun struct {
	cancel context.CancelFunc
	done   chan struct{}
}
type clawBotTypingState struct {
	mu    sync.Mutex
	cache map[[32]byte]clawBotTypingCache
	tails map[[32]byte]*clawBotTypingRun
	runs  map[*clawBotTypingRun]bool
}

// startClawBotTyping never performs network I/O on the generation/delivery
// goroutine. Per-recipient lanes serialize stop/start, including rapid replies.
func (s *ClawBotService) startClawBotTyping(parent context.Context, cfg config.ClawBotConfig, user string) func() {
	if parent.Err() != nil || user == "" || cfg.BotToken == "" {
		return func() {}
	}
	key := sha256.Sum256([]byte(clawBotAccount(cfg) + "\x00" + cfg.BotToken + "\x00" + user))
	ctx, cancel := context.WithCancel(parent)
	run := &clawBotTypingRun{cancel: cancel, done: make(chan struct{})}
	st := &s.typing
	st.mu.Lock()
	if st.cache == nil {
		st.cache = make(map[[32]byte]clawBotTypingCache)
		st.tails = make(map[[32]byte]*clawBotTypingRun)
		st.runs = make(map[*clawBotTypingRun]bool)
	}
	previous := st.tails[key]
	st.tails[key] = run
	st.runs[run] = true
	st.mu.Unlock()
	go func() {
		defer func() {
			st.mu.Lock()
			delete(st.runs, run)
			if st.tails[key] == run {
				delete(st.tails, key)
			}
			close(run.done)
			st.mu.Unlock()
		}()
		if previous != nil {
			<-previous.done
		}
		s.runClawBotTyping(ctx, cfg, user, key)
	}()
	return cancel
}

func (s *ClawBotService) runClawBotTyping(ctx context.Context, cfg config.ClawBotConfig, user string, key [32]byte) {
	st := &s.typing
	st.mu.Lock()
	entry := st.cache[key]
	st.mu.Unlock()
	defer func() {
		st.mu.Lock()
		// Bound memory even for installations with many one-off contacts.
		if len(st.cache) >= 2048 {
			for k, v := range st.cache {
				if time.Now().After(v.expires) {
					delete(st.cache, k)
				}
			}
			if len(st.cache) >= 2048 {
				clear(st.cache)
			}
		}
		st.cache[key] = entry
		st.mu.Unlock()
	}()
	sentTicket := ""
	defer func() {
		if sentTicket == "" {
			return
		}
		// Cancellation/disable/account switch must clear the OLD account's indicator.
		stopCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.client.SendTyping(stopCtx, cfg.BaseURL, cfg.BotToken, s.wechatUIN, user, sentTicket, 2)
	}()
	timer := time.NewTicker(clawBotTypingInterval)
	defer timer.Stop()
	for {
		current := s.handler.configManager.GetClawBotConfig()
		s.mu.RLock()
		closed := s.closed
		s.mu.RUnlock()
		if ctx.Err() != nil || closed || !s.isCurrentAccount(cfg) || current.BotToken != cfg.BotToken {
			return
		}
		now := time.Now()
		token := s.getContextToken(user)
		if (entry.context != token || !now.Before(entry.expires)) && !now.Before(entry.retryAt) {
			ticket, err := s.client.GetConfig(ctx, cfg.BaseURL, cfg.BotToken, s.wechatUIN, user, token)
			if err == nil {
				entry.ticket = ticket
				entry.context = token
				entry.expires = now.Add(clawBotTypingTTL)
				entry.backoff = 0
				entry.retryAt = now.Add(2 * time.Second)
			} else {
				entry.ticket = ""
				entry.expires = time.Time{}
				if entry.backoff == 0 {
					entry.backoff = 2 * time.Second
				} else {
					entry.backoff = min(entry.backoff*2, time.Hour)
				}
				entry.retryAt = now.Add(entry.backoff)
			}
		}
		if ctx.Err() != nil {
			return
		}
		if entry.ticket != "" && entry.context == token && now.Before(entry.expires) && !now.Before(entry.sendAt) {
			entry.sendAt = now.Add(clawBotTypingInterval)
			// A timeout may still have reached the server: always attempt stop.
			sentTicket = entry.ticket
			if err := s.client.SendTyping(ctx, cfg.BaseURL, cfg.BotToken, s.wechatUIN, user, entry.ticket, 1); err != nil && ctx.Err() == nil {
				entry.ticket = ""
				entry.expires = time.Time{}
				entry.retryAt = now.Add(clawBotTypingInterval)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
	}
}

// Called only after generation workers have drained. Shutdown can wait for a
// bounded best-effort stop; ordinary generation never waits for typing.
func (s *ClawBotService) stopClawBotTyping() {
	st := &s.typing
	st.mu.Lock()
	runs := make([]*clawBotTypingRun, 0, len(st.runs))
	for run := range st.runs {
		run.cancel()
		runs = append(runs, run)
	}
	st.mu.Unlock()
	for _, run := range runs {
		<-run.done
	}
}
