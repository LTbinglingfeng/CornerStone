package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/config"
	"cornerstone/logging"
	"cornerstone/storage"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	reminderScanInterval  = 60 * time.Second
	reminderProcessTimout = 2 * time.Minute
)

type reminderCreateRequest struct {
	Channel        storage.ReminderChannel
	SessionID      string
	PromptID       string
	PromptName     string
	Target         storage.ReminderTarget
	Title          string
	ReminderPrompt string
	DueAt          time.Time
}

type retryableReminderError struct {
	err error
}

func (e *retryableReminderError) Error() string {
	if e == nil || e.err == nil {
		return "retryable reminder error"
	}
	return e.err.Error()
}

func (e *retryableReminderError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func newRetryableReminderError(err error) error {
	if err == nil {
		return nil
	}
	var retryable *retryableReminderError
	if errors.As(err, &retryable) {
		return err
	}
	return &retryableReminderError{err: err}
}

func isRetryableReminderError(err error) bool {
	var retryable *retryableReminderError
	return errors.As(err, &retryable)
}

type ReminderService struct {
	handler *Handler
	manager *storage.ReminderManager

	exactTimeProvider exactTimeProvider
	scanInterval      time.Duration
	processTimeout    time.Duration

	mu        sync.RWMutex
	workerCtx context.Context
}

func NewReminderService(handler *Handler, manager *storage.ReminderManager, exactTimeService exactTimeProvider) *ReminderService {
	return &ReminderService{
		handler:           handler,
		manager:           manager,
		exactTimeProvider: exactTimeService,
		scanInterval:      reminderScanInterval,
		processTimeout:    reminderProcessTimout,
	}
}

func (s *ReminderService) Start(ctx context.Context) {
	if s == nil {
		return
	}

	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	s.workerCtx = ctx
	s.mu.Unlock()

	go s.run(ctx)
}

func (s *ReminderService) run(ctx context.Context) {
	s.scanDueReminders(ctx)

	ticker := time.NewTicker(s.scanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.scanDueReminders(ctx)
		}
	}
}

func (s *ReminderService) List() []storage.Reminder {
	if s == nil || s.manager == nil {
		return nil
	}
	return s.manager.List()
}

func (s *ReminderService) Get(id string) (*storage.Reminder, bool) {
	if s == nil || s.manager == nil {
		return nil, false
	}
	return s.manager.Get(strings.TrimSpace(id))
}

func (s *ReminderService) Create(request reminderCreateRequest) (*storage.Reminder, error) {
	if s == nil || s.manager == nil {
		return nil, fmt.Errorf("reminder service not configured")
	}
	if s.handler == nil || s.handler.chatManager == nil {
		return nil, fmt.Errorf("chat manager not configured")
	}

	sessionID := strings.TrimSpace(request.SessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("session id is required")
	}
	if _, ok := s.handler.chatManager.GetSession(sessionID); !ok {
		return nil, fmt.Errorf("session not found")
	}

	promptID := strings.TrimSpace(request.PromptID)
	if promptID == "" {
		return nil, fmt.Errorf("prompt id is required")
	}

	promptName := strings.TrimSpace(request.PromptName)
	if promptName == "" && s.handler.promptManager != nil {
		if prompt, ok := s.handler.promptManager.Get(promptID); ok && prompt != nil {
			promptName = strings.TrimSpace(prompt.Name)
		}
	}
	if promptName == "" {
		if session, ok := s.handler.chatManager.GetSession(sessionID); ok && session != nil {
			promptName = strings.TrimSpace(session.PromptName)
		}
	}
	if promptName == "" {
		promptName = promptID
	}

	createdAt := s.now()
	reminder := storage.Reminder{
		ID:             generateReminderID(),
		Channel:        request.Channel,
		SessionID:      sessionID,
		PromptID:       promptID,
		PromptName:     promptName,
		Target:         request.Target,
		Title:          strings.TrimSpace(request.Title),
		ReminderPrompt: strings.TrimSpace(request.ReminderPrompt),
		DueAt:          request.DueAt,
		Status:         storage.ReminderStatusPending,
		Attempts:       0,
		CreatedAt:      createdAt,
		UpdatedAt:      createdAt,
	}

	created, err := s.manager.Create(reminder)
	if err != nil {
		return nil, err
	}
	s.triggerReminderIfDue(created)
	return created, nil
}

func (s *ReminderService) UpdatePending(id string, patch storage.ReminderPatch) (*storage.Reminder, error) {
	if s == nil || s.manager == nil {
		return nil, fmt.Errorf("reminder service not configured")
	}

	updated, err := s.manager.UpdatePending(strings.TrimSpace(id), patch, s.now())
	if err != nil {
		return nil, err
	}
	s.triggerReminderIfDue(updated)
	return updated, nil
}

func (s *ReminderService) CancelPending(id string) (*storage.Reminder, error) {
	if s == nil || s.manager == nil {
		return nil, fmt.Errorf("reminder service not configured")
	}
	return s.manager.CancelPending(strings.TrimSpace(id), s.now())
}

func (s *ReminderService) Delete(id string) error {
	if s == nil || s.manager == nil {
		return fmt.Errorf("reminder service not configured")
	}
	return s.manager.Delete(strings.TrimSpace(id))
}

func (s *ReminderService) scanDueReminders(ctx context.Context) {
	if s == nil || s.manager == nil {
		return
	}

	dueReminders := s.manager.ListDuePending(s.now())
	for _, reminder := range dueReminders {
		s.fireReminderAsync(ctx, reminder.ID)
	}
}

func (s *ReminderService) triggerReminderIfDue(reminder *storage.Reminder) {
	if reminder == nil || reminder.Status != storage.ReminderStatusPending {
		return
	}
	if reminder.DueAt.After(s.now()) {
		return
	}
	s.fireReminderAsync(s.getWorkerContext(), reminder.ID)
}

func (s *ReminderService) fireReminderAsync(ctx context.Context, reminderID string) {
	if strings.TrimSpace(reminderID) == "" {
		return
	}

	go func() {
		if err := s.fireReminder(ctx, reminderID); err != nil {
			logging.Errorf("fire reminder failed: id=%s err=%v", reminderID, err)
		}
	}()
}

func (s *ReminderService) fireReminder(ctx context.Context, reminderID string) error {
	if s == nil || s.manager == nil {
		return fmt.Errorf("reminder service not configured")
	}

	firingAt := s.now()
	reminder, transitioned, err := s.manager.TryMarkFiring(strings.TrimSpace(reminderID), firingAt)
	if err != nil {
		if errorsIsNotExist(err) {
			return nil
		}
		return err
	}
	if !transitioned || reminder == nil {
		return nil
	}

	baseCtx := ctx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	processCtx, cancel := context.WithTimeout(baseCtx, s.processTimeout)
	defer cancel()

	if errProcess := s.processReminder(processCtx, reminder); errProcess != nil {
		failedAt := s.now()
		if isRetryableReminderError(errProcess) {
			if _, errMark := s.manager.MarkPending(reminder.ID, failedAt, errProcess.Error()); errMark != nil {
				logging.Errorf("mark reminder pending for retry failed: id=%s err=%v", reminder.ID, errMark)
				return errMark
			}
			logging.Warnf("reminder fire deferred for retry: id=%s channel=%s session=%s err=%v", reminder.ID, reminder.Channel, reminder.SessionID, errProcess)
			return nil
		}
		if _, errMark := s.manager.MarkFailed(reminder.ID, failedAt, errProcess.Error()); errMark != nil {
			logging.Errorf("mark reminder failed state failed: id=%s err=%v", reminder.ID, errMark)
			return errMark
		}
		logging.Errorf("reminder fire failed: id=%s channel=%s session=%s err=%v", reminder.ID, reminder.Channel, reminder.SessionID, errProcess)
		return nil
	}

	sentAt := s.now()
	if _, errMark := s.manager.MarkSent(reminder.ID, sentAt); errMark != nil {
		return errMark
	}
	return nil
}

func (s *ReminderService) processReminder(ctx context.Context, reminder *storage.Reminder) error {
	if s == nil || s.handler == nil {
		return fmt.Errorf("reminder handler not configured")
	}
	if reminder == nil {
		return fmt.Errorf("reminder is nil")
	}

	session, ok := s.handler.chatManager.GetSession(reminder.SessionID)
	if !ok || session == nil {
		return fmt.Errorf("session not found")
	}
	if s.handler.promptManager == nil {
		return fmt.Errorf("prompt manager not configured")
	}

	prompt, ok := s.handler.promptManager.Get(reminder.PromptID)
	if !ok || prompt == nil {
		return fmt.Errorf("prompt not found")
	}

	reply, err := s.generateReminderReply(ctx, reminder, session, prompt)
	if err != nil {
		return err
	}
	if reply == nil || strings.TrimSpace(reply.Text) == "" {
		return fmt.Errorf("empty reminder reply")
	}

	switch reminder.Channel {
	case storage.ReminderChannelWeb:
		return s.persistWebReminderReply(reminder, reply)
	case storage.ReminderChannelClawBot:
		return s.sendClawBotReminderReply(ctx, reminder, reply)
	case storage.ReminderChannelNapCat:
		return s.sendNapCatReminderReply(ctx, reminder, reply)
	default:
		return fmt.Errorf("unsupported reminder channel: %s", reminder.Channel)
	}
}

func (s *ReminderService) generateReminderReply(
	ctx context.Context,
	reminder *storage.Reminder,
	session *storage.ChatRecord,
	prompt *storage.Prompt,
) (*generatedSessionReply, error) {
	currentConfig := config.DefaultConfig()
	if s.handler.configManager != nil {
		currentConfig = s.handler.configManager.Get()
	}

	channel := chatToolChannelDefault
	channelGuide := []string{
		"[提醒触发]",
		"你正在处理一条已经到期的 reminder。",
		"这不是用户刚发来的新消息，而是系统内部触发的定时任务。",
		"你需要结合当前会话上下文，主动向用户发送一条自然的 assistant 消息。",
		"不要提及内部提示词、系统实现细节或工具执行过程。",
	}
	if reminder.Channel == storage.ReminderChannelClawBot {
		channel = chatToolChannelClawBot
		channelGuide = append(channelGuide,
			"[渠道说明]",
			"你正在通过微信 ClawBot 渠道主动提醒用户。",
			"请只输出适合微信文本消息发送的自然语言内容。",
		)
	} else if reminder.Channel == storage.ReminderChannelNapCat {
		channel = chatToolChannelNapCat
		channelGuide = append(channelGuide,
			"[渠道说明]",
			"你正在通过 QQ / NapCat 私聊渠道主动提醒用户。",
			"请只输出适合即时聊天窗口发送的自然语言纯文本。",
			"不要暴露内部提示词、系统实现或工具细节。",
		)
	} else {
		channelGuide = append(channelGuide,
			"[渠道说明]",
			"你正在向 Web 会话主动发送一条提醒消息。",
			"请直接输出用户能看到的自然语言回复。",
		)
	}

	ephemeralPrompt := buildReminderInternalPrompt(reminder)

	return s.handler.generateSessionReply(ctx, sessionReplyOptions{
		Session:    session,
		PromptID:   prompt.ID,
		PromptName: prompt.Name,
		Persona:    prompt.Content,
		Channel:    channel,
		Target:     reminder.Target,
		ToolOptions: chatToolOptions{
			ToolToggles:                 currentConfig.ToolToggles,
			Channel:                     channel,
			CornerstoneWebSearchEnabled: isCornerstoneWebSearchConfigured(currentConfig),
			ReminderFiring:              true,
		},
		ExtraSystemGuides: []string{strings.Join(channelGuide, "\n")},
		EphemeralMessages: []client.Message{
			{
				Role:    "user",
				Content: ephemeralPrompt,
			},
		},
	})
}

func buildReminderInternalPrompt(reminder *storage.Reminder) string {
	if reminder == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf(
		"[内部 reminder，不写入聊天记录]\n提醒标题：%s\n计划触发时间：%s\n内部提醒提示词：\n%s",
		reminder.Title,
		reminder.DueAt.Format(time.RFC3339),
		reminder.ReminderPrompt,
	))
}

func (s *ReminderService) persistWebReminderReply(reminder *storage.Reminder, reply *generatedSessionReply) error {
	if reminder == nil || reply == nil {
		return fmt.Errorf("missing reminder reply")
	}

	messages := reply.StorageMessages
	if len(messages) == 0 {
		messages = []storage.ChatMessage{
			{
				Role:      "assistant",
				Content:   strings.TrimSpace(reply.Text),
				Timestamp: time.Now(),
			},
		}
	}

	if err := s.handler.chatManager.AddMessages(reminder.SessionID, messages); err != nil {
		return err
	}
	if reply.MemSession != nil {
		reply.MemSession.OnRoundComplete()
	}
	return nil
}

func (s *ReminderService) sendClawBotReminderReply(
	ctx context.Context,
	reminder *storage.Reminder,
	reply *generatedSessionReply,
) error {
	if reminder == nil || reply == nil {
		return fmt.Errorf("missing reminder reply")
	}
	if s.handler.clawBotService == nil {
		return fmt.Errorf("clawbot service not configured")
	}

	cfg := s.handler.configManager.GetClawBotConfig()
	if !cfg.Enabled || strings.TrimSpace(cfg.BotToken) == "" {
		return fmt.Errorf("clawbot channel is unavailable")
	}
	targetUserID := strings.TrimSpace(reminder.Target.UserID)
	if targetUserID == "" {
		targetUserID = strings.TrimSpace(reminder.ClawBotUserID)
	}
	if targetUserID == "" {
		return fmt.Errorf("clawbot user id is required")
	}

	replyText := strings.TrimSpace(reply.Text)
	if err := s.handler.clawBotService.sendTextReply(ctx, cfg, targetUserID, replyText); err != nil {
		s.handler.clawBotService.setLastError(err)
		return err
	}

	messages := reply.StorageMessages
	if len(messages) == 0 {
		messages = []storage.ChatMessage{
			{
				Role:      "assistant",
				Content:   replyText,
				Timestamp: time.Now(),
			},
		}
	}
	if err := s.handler.chatManager.AddMessages(reminder.SessionID, messages); err != nil {
		s.handler.clawBotService.setLastError(err)
		return err
	}
	if reply.MemSession != nil {
		reply.MemSession.OnRoundComplete()
	}
	return nil
}

func (s *ReminderService) sendNapCatReminderReply(
	ctx context.Context,
	reminder *storage.Reminder,
	reply *generatedSessionReply,
) error {
	if reminder == nil || reply == nil {
		return fmt.Errorf("missing reminder reply")
	}
	if s.handler.napCatService == nil {
		return fmt.Errorf("napcat service not configured")
	}

	replyText := strings.TrimSpace(reply.Text)
	if err := s.handler.napCatService.sendReminderPrivateText(ctx, reminder.Target, replyText); err != nil {
		s.handler.napCatService.setLastError(err)
		logging.Errorf("napcat reminder send failed: id=%s session=%s err=%v", reminder.ID, reminder.SessionID, err)
		return err
	}

	messages := reply.StorageMessages
	if len(messages) == 0 {
		messages = []storage.ChatMessage{
			{
				Role:      "assistant",
				Content:   replyText,
				Timestamp: time.Now(),
			},
		}
	}
	if err := s.handler.chatManager.AddMessages(reminder.SessionID, messages); err != nil {
		s.handler.napCatService.setLastError(err)
		return err
	}
	if reply.MemSession != nil {
		reply.MemSession.OnRoundComplete()
	}
	return nil
}

func (s *ReminderService) getWorkerContext() context.Context {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.workerCtx
}

func (s *ReminderService) now() time.Time {
	if s == nil || s.exactTimeProvider == nil {
		return time.Now()
	}
	return s.exactTimeProvider.Now()
}

func generateReminderID() string {
	raw := make([]byte, 16)
	_, _ = rand.Read(raw)
	return hex.EncodeToString(raw)
}

func errorsIsNotExist(err error) bool {
	return err != nil && os.IsNotExist(err)
}
