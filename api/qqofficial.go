package api

import (
	"context"
	"cornerstone/client"
	"cornerstone/config"
	"cornerstone/logging"
	"cornerstone/storage"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type qqOfficialConnection interface {
	Run(context.Context, func(context.Context, client.QQOfficialMessage) error, func(client.QQOfficialStatus))
	SendText(context.Context, string, string, int, string) (string, error)
	SendImage(context.Context, string, string, int, []byte, string) (string, error)
}
type qqOfficialJob struct {
	ctx        context.Context
	generation uint64
	cfg        config.QQOfficialConfig
	conn       qqOfficialConnection
	msg        client.QQOfficialMessage
}
type QQOfficialService struct {
	mu           sync.Mutex
	handler      *Handler
	cfg          config.QQOfficialConfig
	generation   uint64
	cancel       context.CancelFunc
	closed       bool
	status       client.QQOfficialStatus
	queue        chan qqOfficialJob
	done         chan struct{}
	factory      func(string, string) qqOfficialConnection
	generate     func(context.Context, sessionReplyOptions) (*generatedSessionReply, error)
	download     func(context.Context, string) ([]byte, error)
	startBinding func(context.Context) (*client.QQOfficialBinding, error)
	pollBinding  func(context.Context, *client.QQOfficialBinding) (*client.QQOfficialBindResult, error)
	login        *qqOfficialLogin
	receipts     *storage.QQOfficialReceiptStore
}

func NewQQOfficialService(h *Handler) *QQOfficialService {
	s := &QQOfficialService{handler: h, queue: make(chan qqOfficialJob, 64), done: make(chan struct{}), factory: func(id, secret string) qqOfficialConnection { return client.NewQQOfficialClient(id, secret) }, generate: h.generateSessionReply, download: client.DownloadQQOfficialImage, startBinding: client.StartQQOfficialBinding, pollBinding: client.PollQQOfficialBinding}
	h.qqOfficialService = s
	go s.worker()
	s.ApplyCurrentConfig()
	return s
}
func (s *QQOfficialService) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	s.invalidateLocked()
	close(s.done)
}
func (s *QQOfficialService) invalidateLocked() {
	s.generation++
	if s.cancel != nil {
		s.cancel()
	}
	if s.login != nil {
		s.login.cancel()
		s.login.status = "cancelled"
		s.login.binding = nil
	}
	s.status = client.QQOfficialStatus{Status: "disconnected"}
}
func (s *QQOfficialService) ApplyCurrentConfig() { s.mu.Lock(); defer s.mu.Unlock(); s.applyLocked() }
func (s *QQOfficialService) applyLocked() {
	s.invalidateLocked()
	s.cfg = s.handler.configManager.GetQQOfficialConfig()
	if !s.cfg.Enabled {
		s.status.Status = "disabled"
		return
	}
	if s.closed {
		return
	}
	if strings.TrimSpace(s.cfg.AppID) == "" || strings.TrimSpace(s.cfg.AppSecret) == "" {
		s.status.Status = "missing_credentials"
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	gen := s.generation
	cfg := s.cfg
	conn := s.factory(cfg.AppID, cfg.AppSecret)
	s.status.Status = "connecting"
	go conn.Run(ctx, func(_ context.Context, m client.QQOfficialMessage) error {
		if m.ID == "" || m.UserOpenID == "" {
			return nil
		}
		if cfg.SourceFilterMode == "allowlist" {
			allowed := false
			for _, id := range cfg.AllowedUserOpenIDs {
				if id == m.UserOpenID {
					allowed = true
				}
			}
			if !allowed {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case s.queue <- qqOfficialJob{ctx, gen, cfg, conn, m}:
			return nil
		default:
			logging.Warnf("qqofficial incoming queue full: event=%s", m.ID)
			s.setError(gen, "incoming queue full")
			return fmt.Errorf("incoming queue full")
		}
	}, func(st client.QQOfficialStatus) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.generation == gen && !s.closed {
			st.LastError = s.redact(st.LastError)
			logQQOfficialStatus(s.status, st)
			s.status = st
		}
	})
}
func (s *QQOfficialService) redact(v string) string {
	if s.cfg.AppSecret != "" {
		v = strings.ReplaceAll(v, s.cfg.AppSecret, "[redacted]")
	}
	return v
}

// Status callbacks repeat during reconnect backoff; log transitions and new errors only.
func logQQOfficialStatus(previous, next client.QQOfficialStatus) {
	switch {
	case next.Status == "connected" && previous.Status != "connected":
		logging.Infof("qqofficial connected: bot_id=%s bot_name=%s", next.BotID, next.BotName)
	case next.Status == "error" && next.LastError != "":
		logging.Warnf("qqofficial connection error: err=%s", next.LastError)
	case next.Status == "disconnected" && previous.Status == "connected":
		logging.Infof("qqofficial disconnected")
	}
}
func (s *QQOfficialService) setError(gen uint64, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if gen == s.generation {
		s.status.LastError = s.redact(msg)
	}
}
func (s *QQOfficialService) currentLocked(j qqOfficialJob) bool {
	return j.ctx.Err() == nil && s.confirmationCurrentLocked(j)
}

// A send deadline stops further delivery, but does not revoke already confirmed
// history. Configuration changes and service shutdown still revoke both.
func (s *QQOfficialService) confirmationCurrentLocked(j qqOfficialJob) bool {
	return !s.closed && s.generation == j.generation && reflect.DeepEqual(j.cfg, s.handler.configManager.GetQQOfficialConfig())
}
func (s *QQOfficialService) worker() {
	for {
		select {
		case <-s.done:
			return
		case j := <-s.queue:
			if err := s.process(j); err != nil {
				msg := formatChannelLastError(err)
				s.mu.Lock()
				msg = s.redact(msg)
				s.mu.Unlock()
				logging.Errorf("qqofficial process message failed: event=%s err=%s", j.msg.ID, msg)
				s.setError(j.generation, msg)
			}
		}
	}
}
func qqOfficialSessionID(c config.QQOfficialConfig, user string) string {
	return fmt.Sprintf("qqofficial-%x", sha256.Sum256([]byte(c.AppID+"\x00"+user+"\x00"+c.PromptID)))
}
func qqOfficialEventID(id string) string {
	return fmt.Sprintf("qqofficial-in-%x", sha256.Sum256([]byte(id)))
}
func (s *QQOfficialService) process(j qqOfficialJob) error {
	s.mu.Lock()
	valid := s.currentLocked(j)
	s.mu.Unlock()
	if !valid {
		return nil
	}
	h := s.handler
	sid := qqOfficialSessionID(j.cfg, j.msg.UserOpenID)
	op := qqOfficialEventID(j.cfg.AppID + "\x00" + j.msg.UserOpenID + "\x00" + j.msg.ID)
	s.mu.Lock()
	if s.receipts == nil {
		// Resolve the configured data root (e.g. macOS /var -> /private/var),
		// but do not follow a symlink in the channel-owned receipt directory.
		root, err := filepath.EvalSymlinks(filepath.Dir(h.cachePhotoDir))
		if err != nil {
			s.mu.Unlock()
			return fmt.Errorf("QQ Official data root unavailable")
		}
		s.receipts = storage.NewQQOfficialReceiptStore(filepath.Join(root, "qqofficial", "receipts.json"))
	}
	receipts := s.receipts
	s.mu.Unlock()
	seen, err := receipts.Seen(op)
	if err != nil {
		return err
	}
	if seen {
		return nil
	}
	session, exists := h.chatManager.GetSession(sid)
	// Event receipts include the app and sender, but not the persona. A replay
	// after rebinding a persona must not generate the same upstream event again.
	for _, meta := range h.chatManager.ListSessions() {
		if !strings.HasPrefix(meta.ID, "qqofficial-") {
			continue
		}
		previous, ok := h.chatManager.GetSession(meta.ID)
		if !ok {
			continue
		}
		for _, id := range previous.AppliedOperations {
			if id == op {
				// Backfill independent receipts before a user can delete history.
				return receipts.Record(op)
			}
		}
	}
	promptName, persona := "", ""
	if j.cfg.PromptID != "" {
		if h.promptManager == nil {
			return fmt.Errorf("configured prompt missing")
		}
		p, ok := h.promptManager.Get(j.cfg.PromptID)
		if !ok {
			return fmt.Errorf("configured prompt missing")
		}
		promptName = p.Name
		persona = p.Content
	}
	input := storage.ChatMessage{Role: "user", Content: qqOfficialInputText(j.msg.Content), Timestamp: j.msg.Timestamp}
	if input.Timestamp.IsZero() {
		input.Timestamp = time.Now()
	}
	mediaCtx, mediaCancel := context.WithTimeout(j.ctx, 30*time.Second)
	defer mediaCancel()
	for i, a := range j.msg.Attachments {
		if i >= 8 {
			input.Content += "\n[附件过多，已忽略]"
			break
		}
		if !strings.HasPrefix(a.ContentType, "image/") {
			input.Content += "\n[不支持的附件]"
			continue
		}
		data, err := s.download(mediaCtx, a.URL)
		if err != nil {
			input.Content += "\n[图片下载失败]"
			continue
		}
		p, err := h.saveCachePhotoBytes(data)
		if err != nil {
			input.Content += "\n[图片保存失败]"
			continue
		}
		input.ImagePaths = append(input.ImagePaths, p)
	}
	mediaCancel()
	input.Content = qqOfficialInputText(input.Content)
	s.mu.Lock()
	if !s.currentLocked(j) {
		s.mu.Unlock()
		return nil
	}
	if !exists {
		session, err = h.chatManager.CreateSession(sid, "QQ Official Chat · "+j.msg.UserOpenID, j.cfg.PromptID, promptName)
	}
	if err == nil {
		err = h.chatManager.CommitDeliveryBatch(sid, op, []storage.ChatMessage{input}, false, nil)
	}
	if err == nil {
		err = receipts.Record(op)
	}
	s.mu.Unlock()
	if err != nil {
		return err
	}
	session, exists = h.chatManager.GetSession(sid)
	if !exists {
		return fmt.Errorf("QQ Official session was removed")
	}
	jobCtx := j.ctx
	ctx, cancel := context.WithTimeout(j.ctx, 2*time.Minute)
	defer cancel()
	j.ctx = ctx
	// Quotes are context, not authorization to fetch media.
	generationSession := *session
	generationSession.Messages = append([]storage.ChatMessage(nil), session.Messages...)
	for i := range generationSession.Messages {
		generationSession.Messages[i].Quote = nil
	}
	reply, err := s.generate(j.ctx, sessionReplyOptions{
		Session: &generationSession, PromptID: j.cfg.PromptID, PromptName: promptName, Persona: persona,
		Channel: chatToolChannelQQOfficial, ToolOptions: chatToolOptions{Channel: chatToolChannelQQOfficial}, PersistMode: sessionReplyPersistAll,
		ExtraSystemGuides: []string{"你正在通过 QQ 官方机器人私聊渠道回复用户。只支持当前私聊的文字和图片；不能设置提醒、主动问候、访问群或频道，也不能转写语音。未解析的附件仅为占位，不要声称已读取。不要输出路径或 CQ 码代替发送图片。"},
	})
	if err != nil {
		j.ctx = jobCtx
		return s.notifyGenerationFailure(j, sid, err)
	}
	if reply == nil {
		return nil
	}
	return s.deliver(j, sid, op, session.Messages, reply)
}

// Like ClawBot and NapCat, a failed generation is shown to the sender but kept
// out of history so error details never become model context. The passive
// reply uses the first sequence because nothing was sent for this message yet.
func (s *QQOfficialService) notifyGenerationFailure(j qqOfficialJob, sid string, genErr error) error {
	failure := fmt.Errorf("生成回复失败: %w", genErr)
	s.mu.Lock()
	current := s.currentLocked(j)
	detail := s.redact(formatDirectErrorDetail(genErr, 220))
	s.mu.Unlock()
	if !current {
		// Configuration change or shutdown cancelled the job; nothing to report.
		return nil
	}
	reply := "暂时无法处理你的消息，请稍后再试。"
	if detail != "" {
		reply = buildChannelFailureReply("暂时无法处理你的消息，请稍后再试", errors.New(detail))
	}
	ctx, cancel := context.WithTimeout(j.ctx, 15*time.Second)
	defer cancel()
	if _, err := j.conn.SendText(ctx, j.msg.UserOpenID, j.msg.ID, 1, reply); err != nil {
		return fmt.Errorf("%w；失败提示发送失败: %v", failure, err)
	}
	logging.Infof("qqofficial sent generation failure notice: session=%s event=%s", sid, j.msg.ID)
	return failure
}

// Only generated image metadata is eligible for upload; text is never treated as a path.
func (s *QQOfficialService) imageBytes(p string) ([]byte, string, error) {
	if strings.HasPrefix(p, storage.AttachmentURLPrefix) {
		b, err := s.handler.attachmentStore().Read(p)
		if len(b) > 5<<20 {
			return nil, "", fmt.Errorf("image exceeds 5 MiB")
		}
		return b, strings.TrimPrefix(p, storage.AttachmentURLPrefix), err
	}
	if !strings.HasPrefix(p, "cache_photo/") {
		return nil, "", fmt.Errorf("untrusted image path")
	}
	name := strings.TrimPrefix(p, "cache_photo/")
	if err := storage.ValidateFileName(name); err != nil {
		return nil, "", err
	}
	root, err := os.OpenRoot(s.handler.cachePhotoDir)
	if err != nil {
		return nil, "", err
	}
	defer root.Close()
	f, err := root.Open(name)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, "", err
	}
	if !info.Mode().IsRegular() || info.Size() > 5<<20 {
		return nil, "", fmt.Errorf("invalid image file")
	}
	b, err := io.ReadAll(io.LimitReader(f, (5<<20)+1))
	if len(b) > 5<<20 {
		return nil, "", fmt.Errorf("image exceeds 5 MiB")
	}
	return b, name, err
}

func qqOfficialInputText(text string) string {
	const limit = 64 << 10
	const marker = "\n[输入过长，已截断]"
	if len(text) <= limit {
		return text
	}
	n := limit - len(marker)
	for n > 0 && !utf8.RuneStart(text[n]) {
		n--
	}
	return text[:n] + marker
}

func (s *QQOfficialService) deliver(j qqOfficialJob, sid, op string, expected []storage.ChatMessage, r *generatedSessionReply) error {
	confirmed := []storage.ChatMessage{}
	var toolImages []string
	var deliveryErr error
	sent := map[string]bool{}
	authorizedMessages := append([]storage.ChatMessage(nil), expected...)
	for i := range authorizedMessages {
		authorizedMessages[i].Quote = nil
	}
	authorized := sessionToolAttachments(authorizedMessages)
	seq := 0
	token := configuredAssistantMessageSplitToken(s.handler.configManager)
	// Commit the confirmed prefix even after a later send fails. Never resend on a save failure.
	commit := func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if !s.confirmationCurrentLocked(j) {
			return nil
		}
		if len(confirmed) == 0 {
			return nil
		}
		replacement := append(append([]storage.ChatMessage(nil), expected...), confirmed...)
		if err := s.handler.chatManager.CommitDeliverySuffix(sid, op+"-out", expected, replacement); err != nil {
			return err
		}
		if deliveryErr == nil && r.MemSession != nil {
			r.MemSession.OnRoundComplete()
		}
		return nil
	}
	currentConversation := func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		if !s.currentLocked(j) {
			return false
		}
		record, exists := s.handler.chatManager.GetSession(sid)
		return exists && reflect.DeepEqual(record.Messages, expected)
	}
	send := func(text, path string) error {
		// A deleted/edited history also revokes the old attachment catalogue.
		// Check each segment, not only the final history commit.
		if !currentConversation() {
			return context.Canceled
		}
		seq++
		if path != "" {
			b, name, err := s.imageBytes(path)
			if err != nil {
				return err
			}
			if !currentConversation() {
				return context.Canceled
			}
			_, err = j.conn.SendImage(j.ctx, j.msg.UserOpenID, j.msg.ID, seq, b, name)
			return err
		}
		_, err := j.conn.SendText(j.ctx, j.msg.UserOpenID, j.msg.ID, seq, text)
		return err
	}
	for _, m := range r.StorageMessages {
		saved := m
		saved.ProviderState = nil
		if len(m.ToolCalls) > 0 {
			// Signed thinking / tool-call state is required for provider replay,
			// even when later visible output is not delivered.
			saved.ProviderState = client.CloneProviderState(m.ProviderState)
		}
		saved.Attachments = nil
		saved.ImagePaths = nil
		saved.TTSAudioPaths = nil
		if m.Role == "tool" {
			var result chatToolResult
			if json.Unmarshal([]byte(m.Content), &result) == nil && result.Tool == sendAttachmentToolName {
				a, ok := attachmentFromToolResult(m.Content)
				registered, allowed := authorized[a.Path]
				if result.OK && (!ok || !allowed || registered.Kind != "image" || a.Kind != "image") && !r.NoReply && deliveryErr == nil {
					deliveryErr = fmt.Errorf("image intent is not authorized for current session")
				}
				if ok && allowed && registered.Kind == "image" && a.Kind == "image" && !r.NoReply && deliveryErr == nil {
					wasSent := sent[a.Path]
					if !sent[a.Path] {
						deliveryErr = send("", a.Path)
						if deliveryErr == nil {
							sent[a.Path] = true
						}
					}
					if sent[a.Path] {
						data, _ := json.Marshal(chatToolResult{OK: true, Tool: sendAttachmentToolName, Data: map[string]interface{}{"attachment": registered, "delivery_confirmed": true}})
						saved.Content = string(data)
						confirmed = append(confirmed, saved)
						if !wasSent {
							toolImages = append(toolImages, a.Path)
						}
						continue
					}
				}
				saved.Content = `{"ok":false,"tool":"send_attachment","error":"图片投递未确认；未尝试或发送失败，不得声称已发送"}`
			}
			confirmed = append(confirmed, saved)
			continue
		}
		if m.Role != "assistant" {
			confirmed = append(confirmed, saved)
			continue
		}
		saved.Content = ""
		saved.AssistantMessageSplitToken = &token
		if !r.NoReply && deliveryErr == nil && len(m.ToolCalls) == 0 {
			for _, part := range splitAssistantReplyMessages(m.Content, token, 2000) {
				if deliveryErr = send(part, ""); deliveryErr != nil {
					break
				}
				if saved.Content != "" {
					saved.Content += token
				}
				saved.Content += part
			}
			paths := append([]string(nil), m.ImagePaths...)
			for _, a := range m.Attachments {
				if a.Kind == "image" {
					paths = append(paths, a.Path)
				}
			}
			for _, p := range paths {
				if deliveryErr != nil {
					break
				}
				if sent[p] {
					continue
				}
				if a, ok := authorized[p]; !ok || a.Kind != "image" {
					deliveryErr = fmt.Errorf("image not registered in current session")
					break
				}
				if deliveryErr = send("", p); deliveryErr != nil {
					break
				}
				sent[p] = true
				saved.ImagePaths = append(saved.ImagePaths, p)
			}
			if deliveryErr == nil {
				saved.ProviderState = m.ProviderState
			}
		}
		if !shouldDropEmptyAssistantStorageMessage(saved) {
			confirmed = append(confirmed, saved)
		}
	}
	// Do not insert visible assistant media between tool responses: providers
	// require the complete tool-result batch before another assistant message.
	if len(toolImages) > 0 {
		confirmed = append(confirmed, storage.ChatMessage{Role: "assistant", ImagePaths: toolImages, Timestamp: time.Now()})
	}
	if deliveryErr != nil {
		for i := range confirmed {
			if len(confirmed[i].ToolCalls) == 0 {
				confirmed[i].ProviderState = nil
			}
		}
		confirmed = append(confirmed, storage.ChatMessage{Role: "system", Content: "QQ 官方渠道本轮投递未完整确认，已停止后续发送；未自动重试。", Timestamp: time.Now()})
	}
	if err := commit(); err != nil {
		return err
	}
	return deliveryErr
}
