package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/config"
	"cornerstone/logging"
	"cornerstone/storage"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

var errClawBotDeliveryPending = errors.New("clawbot delivery durably queued")
var errClawBotJobUnavailable = errors.New("clawbot delivery job unavailable")

const clawBotMaxPendingJobs = 1000

type clawBotDeliveryContextKey struct{}

func deliveryState(state *storage.ClawBotState) *storage.ClawBotDeliveryState {
	if state.Delivery == nil {
		state.Delivery = &storage.ClawBotDeliveryState{}
	}
	d := state.Delivery
	if d.Receipts == nil {
		d.Receipts = make(map[string]string)
		for _, key := range state.RecentMessageIDs {
			d.Receipts[key] = "legacy"
		}
	}
	if d.Origins == nil {
		d.Origins = make(map[string]string)
	}
	if d.Finished == nil {
		d.Finished = make(map[string]string)
	}
	if d.Actions == nil {
		d.Actions = make(map[string]string)
	}
	if d.ActiveSessions == nil {
		d.ActiveSessions = make(map[string]string)
	}
	return d
}

func deliveryTerminal(status string) bool { return status == "done" || status == "cancelled" }
func deliveryJob(d *storage.ClawBotDeliveryState, id string) *storage.ClawBotDeliveryJob {
	for i := range d.Jobs {
		if d.Jobs[i].ID == id {
			return &d.Jobs[i]
		}
	}
	return nil
}
func deliveryCapacity(d *storage.ClawBotDeliveryState) error {
	pending := 0
	for _, job := range d.Jobs {
		if !deliveryTerminal(job.Status) {
			pending++
		}
	}
	if pending >= clawBotMaxPendingJobs {
		return fmt.Errorf("clawbot delivery queue is full; receive cursor was not advanced")
	}
	return nil
}
func pruneDeliveryJobs(d *storage.ClawBotDeliveryState) {
	// Keep all work plus the latest 100 terminal entries. Permanent receipts live separately.
	terminal := 0
	for i := len(d.Jobs) - 1; i >= 0; i-- {
		job := d.Jobs[i]
		retainAudit := job.CancelRequested && !job.HistoryCommitted
		for _, chunk := range job.Chunks {
			if chunk.Status == "uncertain" || chunk.Status == "sending" {
				retainAudit = true
			}
		}
		if deliveryTerminal(job.Status) && !retainAudit {
			terminal++
			if terminal > 100 {
				d.Jobs = append(d.Jobs[:i], d.Jobs[i+1:]...)
			}
		}
	}
}
func newDeliveryJob(kind, user string, now time.Time) storage.ClawBotDeliveryJob {
	return storage.ClawBotDeliveryJob{Revision: 1, ID: generateID(), Kind: kind, UserID: user, Status: "queued", CreatedAt: now, UpdatedAt: now, ReadyAt: now, Chunks: []storage.ClawBotDeliveryChunk{}}
}

// Inbox contents, dedup receipts, contexts and cursor are one atomic commit.
// No handler, model or external side effect runs before this succeeds.
func (s *ClawBotService) receiveDeliveryBatch(cfg config.ClawBotConfig, cursor, nextCursor string, messages []client.ClawBotIncomingMessage) error {
	mode, delay := s.getReplyWaitWindow()
	now := time.Now()
	for _, msg := range messages {
		if msg.MessageType == 1 {
			s.cacheClawBotReference(cfg, msg)
		}
	}
	return s.updateChannelStateChecked(cfg, func(state *storage.ClawBotState) error {
		d := deliveryState(state)
		for index, msg := range messages {
			key := clawBotMessageKey(msg)
			if key == "" {
				raw, _ := json.Marshal(msg)
				digest := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%s", cursor, index, raw)))
				key = "fallback:" + hex.EncodeToString(digest[:])
			}
			if _, ok := d.Receipts[key]; ok {
				continue
			}
			user := strings.TrimSpace(msg.FromUserID)
			if msg.MessageType != 1 || user == "" {
				d.Receipts[key] = "ignored"
				continue
			}
			// Echo opaque context tokens verbatim, including surrounding whitespace.
			if token := msg.ContextToken; token != "" {
				state.ContextTokens[user] = token
			}
			text := strings.TrimSpace(client.ExtractTextFromClawBotMessage(msg))
			images := client.ExtractImageItemsFromClawBotMessage(msg)
			if text == "" && len(images) == 0 && !client.HasClawBotAttachments(msg) && !client.HasClawBotReference(msg) {
				d.Receipts[key] = "unsupported"
				continue
			}
			kind := "chat"
			if len(images) == 0 && !client.HasClawBotAttachments(msg) && !client.HasClawBotReference(msg) {
				if _, escaped := unescapeClawBotLiteralCommandText(text); !escaped {
					if _, command := parseClawBotCommand(text); command {
						kind = "command"
					}
				}
			}
			var merge *storage.ClawBotDeliveryJob
			if kind == "chat" && delay > 0 {
				for i := len(d.Jobs) - 1; i >= 0; i-- {
					j := &d.Jobs[i]
					if j.UserID != user {
						continue
					}
					if j.Kind == "chat" && j.Status == "queued" && now.Before(j.ReadyAt) {
						merge = j
					}
					break
				}
			}
			if merge == nil {
				if err := deliveryCapacity(d); err != nil {
					return err
				}
				job := newDeliveryJob(kind, user, now)
				if kind == "chat" {
					job.ReadyAt = now.Add(delay)
				}
				d.Jobs = append(d.Jobs, job)
				merge = &d.Jobs[len(d.Jobs)-1]
			} else if mode != string(config.ReplyWaitWindowModeFixed) {
				merge.ReadyAt = now.Add(delay)
			}
			for len(merge.InputTimes) < len(merge.Inputs) {
				merge.InputTimes = append(merge.InputTimes, merge.CreatedAt.Add(time.Duration(len(merge.InputTimes))*time.Millisecond))
			}
			merge.Inputs = append(merge.Inputs, msg)
			merge.InputTimes = append(merge.InputTimes, now)
			merge.UpdatedAt = now
			merge.Revision++
			d.Receipts[key] = merge.ID
		}
		if nextCursor != "" {
			state.GetUpdatesBuf = nextCursor
		}
		pruneDeliveryJobs(d)
		return nil
	})
}

func (s *ClawBotService) snapshotDelivery(id string) (storage.ClawBotDeliveryJob, bool) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	clone, err := storage.CloneClawBotState(s.state)
	if err != nil || clone.Delivery == nil {
		return storage.ClawBotDeliveryJob{}, false
	}
	job := deliveryJob(clone.Delivery, id)
	if job == nil {
		return storage.ClawBotDeliveryJob{}, false
	}
	return *job, true
}
func (s *ClawBotService) changeDelivery(cfg config.ClawBotConfig, id string, change func(*storage.ClawBotDeliveryJob) error) error {
	return s.updateChannelStateChecked(cfg, func(state *storage.ClawBotState) error {
		j := deliveryJob(deliveryState(state), id)
		if j == nil {
			return errClawBotJobUnavailable
		}
		if err := change(j); err != nil {
			return err
		}
		j.UpdatedAt = time.Now()
		j.Revision++
		return nil
	})
}
func (s *ClawBotService) deliveryFault(err error) {
	s.setLastError(fmt.Errorf("clawbot durable delivery stopped: %w", err))
	s.mu.RLock()
	cancel := s.workerCancel
	s.mu.RUnlock()
	if cancel != nil {
		cancel()
	}
}

// parkDelivery resolves a failed step without waiting for a human. Like the
// official openclaw-weixin client, delivery is fire-and-forget, and a job never
// stalls the per-user ordering of later replies. See autoResolveDelivery.
func (s *ClawBotService) parkDelivery(cfg config.ClawBotConfig, id, phase, message string, uncertain bool) {
	logging.Warnf("clawbot delivery auto-resolved: job=%s phase=%s uncertain=%t reason=%s", id, phase, uncertain, message)
	err := s.updateChannelStateChecked(cfg, func(state *storage.ClawBotState) error {
		d := deliveryState(state)
		j := deliveryJob(d, id)
		if j == nil || deliveryTerminal(j.Status) {
			return errClawBotJobUnavailable
		}
		autoResolveDelivery(d, j, phase, message, uncertain)
		j.UpdatedAt = time.Now()
		j.Revision++
		return nil
	})
	if err != nil && !errors.Is(err, errClawBotJobUnavailable) {
		s.deliveryFault(err)
	}
}

// autoResolveDelivery replaces the former manual review states:
//   - An unknown send outcome counts as delivered. iLink has no idempotent
//     resend, so retrying risks duplicates; the official client never retries.
//   - Any other failure cancels the remaining work. Cancellation still commits
//     segments that were already sent to history and finalizes the origin.
//   - If finishing an already cancelled job fails, it is finalized as cancelled
//     directly (audit kept in LastError) instead of looping.
func autoResolveDelivery(d *storage.ClawBotDeliveryState, j *storage.ClawBotDeliveryJob, phase, message string, uncertain bool) {
	j.Phase = phase
	j.LastError = message
	j.NextAttemptAt = time.Time{}
	j.Attempts = 0
	switch {
	case uncertain && phase == "send" && j.NextChunk < len(j.Chunks):
		j.Chunks[j.NextChunk].Status = "sent"
		j.NextChunk++
		j.Status = "committing"
		j.Phase = "history"
	case j.CancelRequested:
		j.Status = "cancelled"
		d.Finished[j.ID] = j.Status
	default:
		j.CancelRequested = true
		j.Status = "committing"
		j.Phase = "history"
	}
}

func (s *ClawBotService) recoverDeliveries(cfg config.ClawBotConfig) error {
	return s.updateChannelStateChecked(cfg, func(state *storage.ClawBotState) error {
		d := deliveryState(state)
		for i := range d.Jobs {
			j := &d.Jobs[i]
			previous := j.Status
			switch j.Status {
			case "uploading":
				j.Status = "ready"
				j.Phase = "upload"
				if j.NextChunk < len(j.Chunks) {
					j.Chunks[j.NextChunk].Status = "pending"
				}
			case "preparing":
				j.Status = "queued" // only downloads and idempotent input history
			case "generating":
				// Rerunning could repeat tool side effects; drop it instead.
				autoResolveDelivery(d, j, "generation", "Reply generation was interrupted by a restart; cancelled.", true)
			case "executing":
				autoResolveDelivery(d, j, "command", "Command execution was interrupted by a restart; cancelled.", true)
			case "sending":
				autoResolveDelivery(d, j, "send", "Send was interrupted by a restart; treated as delivered.", true)
			case "uncertain":
				// Jobs parked for manual review by older versions.
				autoResolveDelivery(d, j, j.Phase, j.LastError, true)
			case "blocked":
				autoResolveDelivery(d, j, j.Phase, j.LastError, false)
			}
			if previous != j.Status {
				j.Revision++
				j.UpdatedAt = time.Now()
			}
		}
		return nil
	})
}

func (s *ClawBotService) runDeliveryLoop(ctx context.Context, cfg config.ClawBotConfig) {
	if err := s.recoverDeliveries(cfg); err != nil {
		s.deliveryFault(err)
		return
	}
	active := make(map[string]bool)
	finished := make(chan string, 4)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	defer func() {
		for len(active) > 0 {
			delete(active, <-finished)
		}
	}()
	for {
		if ctx.Err() != nil {
			return
		}
		s.stateMu.Lock()
		state, err := storage.CloneClawBotState(s.state)
		s.stateMu.Unlock()
		if err != nil {
			s.deliveryFault(err)
			return
		}
		blockedUsers := make(map[string]bool)
		if state.Delivery != nil {
			for _, job := range state.Delivery.Jobs {
				if deliveryTerminal(job.Status) || blockedUsers[job.UserID] {
					continue
				}
				blockedUsers[job.UserID] = true
				if active[job.UserID] || len(active) >= 4 || time.Now().Before(job.ReadyAt) || time.Now().Before(job.NextAttemptAt) {
					continue
				}
				if !slices.Contains([]string{"queued", "ready", "committing"}, job.Status) {
					continue
				}
				active[job.UserID] = true
				go func(j storage.ClawBotDeliveryJob) {
					defer func() { finished <- j.UserID }()
					s.stepDelivery(ctx, cfg, j)
				}(job)
			}
		}
		select {
		case <-ctx.Done():
			return
		case user := <-finished:
			delete(active, user)
		case <-ticker.C:
		}
	}
}

func (s *ClawBotService) enqueueDirectDelivery(cfg config.ClawBotConfig, user, text string) error {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	job := newDeliveryJob("direct", user, time.Now())
	job.Status = "ready"
	job.Phase = "send"
	token := configuredAssistantMessageSplitToken(s.handler.configManager)
	for i, part := range splitClawBotReplyMessages(text, token, clawBotReplyChunkMaxRune) {
		job.Chunks = append(job.Chunks, storage.ClawBotDeliveryChunk{ClientID: fmt.Sprintf("cs-%s-%d", job.ID, i), Text: part, Status: "pending"})
	}
	return s.enqueueProactiveDelivery(cfg, job, "direct:"+job.ID)
}

func (s *ClawBotService) enqueueProactiveDelivery(cfg config.ClawBotConfig, job storage.ClawBotDeliveryJob, origin string) error {
	completed := false
	err := s.updateChannelStateChecked(cfg, func(state *storage.ClawBotState) error {
		d := deliveryState(state)
		pruneDeliveryJobs(d)
		if id := d.Origins[origin]; id != "" {
			if d.Finished[id] == "cancelled" {
				return fmt.Errorf("clawbot delivery was cancelled")
			}
			completed = d.Finished[id] == "done"
			return nil
		}
		if err := deliveryCapacity(d); err != nil {
			return err
		}
		d.Origins[origin] = job.ID
		d.Jobs = append(d.Jobs, job)
		return nil
	})
	if err != nil {
		return err
	}
	if completed {
		return nil
	}
	return errClawBotDeliveryPending
}
