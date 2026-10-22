package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/config"
	"cornerstone/logging"
	"strings"
	"sync"
	"time"
)

// User-facing failure notices, aligned with official openclaw-weixin error-notice.
const (
	clawBotSendFailedNotice         = "⚠️ 消息发送失败，请稍后重试。"
	clawBotUploadFailedNotice       = "⚠️ 媒体文件上传失败，请稍后重试。"
	clawBotMediaPrepareFailedNotice = "⚠️ 媒体文件处理失败，未能发送。"
)

const (
	clawBotToolProgressQueue        = 32
	clawBotToolProgressDrainTimeout = 10 * time.Second
)

// clawBotSenderAllowed mirrors the official inbound authorization: once a QR
// login has recorded the scanning user, only that user may talk to the bot.
// Legacy/manual-token accounts without a recorded owner accept every sender.
func clawBotSenderAllowed(cfg config.ClawBotConfig, userID string) bool {
	owner := strings.TrimSpace(cfg.ILinkUserID)
	return owner == "" || strings.TrimSpace(userID) == owner
}

// filterClawBotAuthorizedMessages drops user messages from other senders before
// anything (context tokens, quote cache, receipts, queue) observes them. The
// cursor still advances past them, so they are never redelivered.
func filterClawBotAuthorizedMessages(cfg config.ClawBotConfig, messages []client.ClawBotIncomingMessage) []client.ClawBotIncomingMessage {
	if strings.TrimSpace(cfg.ILinkUserID) == "" {
		return messages
	}
	kept := messages[:0:0]
	dropped := 0
	for _, msg := range messages {
		if msg.MessageType == 1 && !clawBotSenderAllowed(cfg, msg.FromUserID) {
			dropped++
			continue
		}
		kept = append(kept, msg)
	}
	if dropped > 0 {
		logging.Infof("clawbot authorization: dropped %d message(s) from unauthorized senders", dropped)
	}
	return kept
}

func (s *ClawBotService) tokenIsStale(cfg config.ClawBotConfig) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.staleToken != "" && s.staleToken == strings.TrimSpace(cfg.BotToken)
}

// sendClawBotErrorNotice is fire-and-forget: it never retries, never touches
// history or the delivery queue, and is abandoned when the worker stops.
func (s *ClawBotService) sendClawBotErrorNotice(cfg config.ClawBotConfig, userID, runID, text string) {
	userID = strings.TrimSpace(userID)
	if userID == "" || strings.TrimSpace(text) == "" || strings.TrimSpace(cfg.BotToken) == "" {
		return
	}
	parent, ok := s.getWorkerCtx()
	if !ok || parent.Err() != nil {
		return
	}
	s.notices.Add(1)
	go func() {
		defer s.notices.Done()
		if !s.isCurrentAccount(cfg) || s.tokenIsStale(cfg) {
			return
		}
		ctx := client.WithClawBotRunID(parent, runID)
		item := client.ClawBotIncomingMessageItem{Type: 1, TextItem: &client.ClawBotItemText{Text: text}}
		if _, err := s.client.SendItemMessageWithID(ctx, cfg.BaseURL, cfg.BotToken, s.wechatUIN, userID, s.getContextToken(userID), item, generateID()); err != nil {
			logging.Event(ctx, "channel.delivery.notice_unconfirmed", "source", "clawbot", "job_id", runID, "error_kind", logging.ErrorKind(err))
		}
	}()
}

// clawBotToolProgress sends TOOL_CALL_START / TOOL_CALL_RESULT items while a
// reply is generated (official reply-progress-sender). Sends are serialized on
// one goroutine so the tool loop is never blocked; failures are only logged.
type clawBotToolProgress struct {
	s     *ClawBotService
	cfg   config.ClawBotConfig
	ctx   context.Context
	user  string
	runID string

	mu     sync.Mutex
	closed bool
	items  chan client.ClawBotIncomingMessageItem
	done   chan struct{}
}

// startClawBotToolProgress returns nil when progress messages are disabled; all
// methods are nil-safe.
func (s *ClawBotService) startClawBotToolProgress(ctx context.Context, cfg config.ClawBotConfig, userID, runID string) *clawBotToolProgress {
	userID = strings.TrimSpace(userID)
	if s == nil || s.handler == nil || s.handler.configManager == nil || s.client == nil || userID == "" || runID == "" || strings.TrimSpace(cfg.BotToken) == "" {
		return nil
	}
	if s.handler.configManager.GetClawBotConfig().ToolProgressDisabled {
		return nil
	}
	p := &clawBotToolProgress{
		s:     s,
		cfg:   cfg,
		ctx:   client.WithClawBotRunID(ctx, runID),
		user:  userID,
		runID: runID,
		items: make(chan client.ClawBotIncomingMessageItem, clawBotToolProgressQueue),
		done:  make(chan struct{}),
	}
	go p.run()
	return p
}

func (p *clawBotToolProgress) callbacks() *toolLoopCallbacks {
	if p == nil {
		return nil
	}
	return &toolLoopCallbacks{
		OnToolCallStart: func(callID, name string) {
			if clawBotToolProgressHidden(name) {
				return
			}
			incomplete := false
			p.enqueue(client.ClawBotIncomingMessageItem{
				Type:              client.ClawBotItemTypeToolCallStart,
				CreateTimeMS:      time.Now().UnixMilli(),
				IsCompleted:       &incomplete,
				ToolCallStartItem: &client.ClawBotToolCallStartItem{ToolName: clawBotToolProgressName(name), ToolCallID: strings.TrimSpace(callID)},
			})
		},
		OnToolCallResult: func(callID, name, status string) {
			if clawBotToolProgressHidden(name) {
				return
			}
			switch status {
			case "completed", "failed", "blocked":
			default:
				status = "unknown"
			}
			complete := true
			p.enqueue(client.ClawBotIncomingMessageItem{
				Type:               client.ClawBotItemTypeToolCallResult,
				CreateTimeMS:       time.Now().UnixMilli(),
				IsCompleted:        &complete,
				ToolCallResultItem: &client.ClawBotToolCallResultItem{ToolName: clawBotToolProgressName(name), ToolCallID: strings.TrimSpace(callID), Status: status},
			})
		},
	}
}

// no_reply must stay silent: announcing it would defeat "已读不回".
func clawBotToolProgressHidden(name string) bool {
	return strings.TrimSpace(name) == "no_reply"
}

func clawBotToolProgressName(name string) string {
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	return "tool"
}

func (p *clawBotToolProgress) enqueue(item client.ClawBotIncomingMessageItem) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	select {
	case p.items <- item:
	default: // Progress is advisory; never stall the tool loop.
		logging.Warnf("clawbot tool progress dropped: run=%s queue full", p.runID)
	}
}

func (p *clawBotToolProgress) run() {
	defer close(p.done)
	stopped := false
	for item := range p.items {
		if stopped || p.ctx.Err() != nil || !p.s.isCurrentAccount(p.cfg) || p.s.tokenIsStale(p.cfg) {
			continue
		}
		_, err := p.s.client.SendItemMessageWithID(p.ctx, p.cfg.BaseURL, p.cfg.BotToken, p.s.wechatUIN, p.user, p.s.getContextToken(p.user), item, generateID())
		if err == nil {
			continue
		}
		logging.Event(p.ctx, "channel.delivery.progress_unconfirmed", "job_id", p.runID, "type", item.Type, "error_kind", logging.ErrorKind(err))
		if client.IsClawBotStaleToken(err) {
			stopped = true
			p.s.markTokenStale(p.cfg.BotToken)
		}
	}
}

// finish stops accepting events and waits (bounded) for queued sends so they
// reach WeChat before the final reply is staged.
func (p *clawBotToolProgress) finish() {
	if p == nil {
		return
	}
	p.mu.Lock()
	if !p.closed {
		p.closed = true
		close(p.items)
	}
	p.mu.Unlock()
	timer := time.NewTimer(clawBotToolProgressDrainTimeout)
	defer timer.Stop()
	select {
	case <-p.done:
	case <-timer.C:
	}
}

func firstToolLoopCallbacks(callbacks []*toolLoopCallbacks) *toolLoopCallbacks {
	if len(callbacks) == 0 {
		return nil
	}
	return callbacks[0]
}
