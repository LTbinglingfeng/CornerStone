package storage

import (
	"cornerstone/logging"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type IdleGreetingStatus string

const (
	IdleGreetingStatusPending IdleGreetingStatus = "pending"
	IdleGreetingStatusFiring  IdleGreetingStatus = "firing"
)

type IdleGreetingTask struct {
	DeliveryID string             `json:"delivery_id,omitempty"`
	Revision   uint64             `json:"revision,omitempty"`
	ID         string             `json:"id"`
	Key        string             `json:"key"`
	Channel    ReminderChannel    `json:"channel"`
	SessionID  string             `json:"session_id"`
	PromptID   string             `json:"prompt_id,omitempty"`
	PromptName string             `json:"prompt_name,omitempty"`
	Target     ReminderTarget     `json:"target,omitempty"`
	DueAt      time.Time          `json:"due_at"`
	LastUserAt time.Time          `json:"last_user_at"`
	Status     IdleGreetingStatus `json:"status"`
	Attempts   int                `json:"attempts"`
	LastError  string             `json:"last_error,omitempty"`
	CreatedAt  time.Time          `json:"created_at"`
	UpdatedAt  time.Time          `json:"updated_at"`
}

type IdleGreetingManager struct {
	filePath  string
	committed map[string]*IdleGreetingTask
	mu        sync.RWMutex
	tasks     map[string]*IdleGreetingTask
}

func NewIdleGreetingManager(dataDir string) *IdleGreetingManager {
	_ = os.MkdirAll(dataDir, 0755)

	manager := &IdleGreetingManager{
		filePath: filepath.Join(dataDir, "idle_greetings.json"),
		tasks:    make(map[string]*IdleGreetingTask),
	}
	manager.load()
	return manager
}

func (m *IdleGreetingManager) load() {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(m.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		logging.Errorf("idle greetings load failed: path=%s err=%v", m.filePath, err)
		return
	}

	var stored []IdleGreetingTask
	if err := json.Unmarshal(data, &stored); err != nil {
		logging.Errorf("idle greetings parse failed: path=%s err=%v", m.filePath, err)
		return
	}

	changed := false
	next := make(map[string]*IdleGreetingTask, len(stored))
	for i := range stored {
		normalized, errNormalize := normalizeIdleGreetingTask(stored[i])
		if errNormalize != nil {
			logging.Warnf("ignore invalid idle greeting task while loading: id=%s err=%v", stored[i].ID, errNormalize)
			changed = true
			continue
		}
		if normalized.Status == IdleGreetingStatusFiring && normalized.DeliveryID == "" {
			normalized.Status = IdleGreetingStatusPending
			normalized.LastError = ""
			changed = true
		}
		next[normalized.ID] = cloneIdleGreetingTask(&normalized)
	}
	m.tasks = next
	m.committed = m.snapshotLocked()

	if changed {
		if errSave := m.saveLocked(); errSave != nil {
			logging.Errorf("idle greetings normalize save failed: path=%s err=%v", m.filePath, errSave)
		}
	}
}

func (m *IdleGreetingManager) saveLocked() error {
	tasks := make([]IdleGreetingTask, 0, len(m.tasks))
	for _, task := range m.tasks {
		if task == nil {
			continue
		}
		tasks = append(tasks, *cloneIdleGreetingTask(task))
	}

	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].CreatedAt.Equal(tasks[j].CreatedAt) {
			return tasks[i].ID < tasks[j].ID
		}
		return tasks[i].CreatedAt.Before(tasks[j].CreatedAt)
	})

	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		m.tasks = m.committed
		if m.tasks == nil {
			m.tasks = make(map[string]*IdleGreetingTask)
		}
		m.committed = m.snapshotLocked()
		return err
	}
	// Publish a complete snapshot atomically; roll memory back on any write failure.
	tmp, err := os.CreateTemp(filepath.Dir(m.filePath), ".producer-*")
	if err == nil {
		name := tmp.Name()
		defer os.Remove(name)
		_, err = tmp.Write(data)
		if err == nil {
			err = tmp.Sync()
		}
		closeErr := tmp.Close()
		if err == nil {
			err = closeErr
		}
		if err == nil {
			err = os.Rename(name, m.filePath)
		}
	}
	if err != nil {
		m.tasks = m.committed
		if m.tasks == nil {
			m.tasks = make(map[string]*IdleGreetingTask)
		}
		m.committed = m.snapshotLocked()
		return err
	}
	m.committed = m.snapshotLocked()
	return nil
}

func (m *IdleGreetingManager) List() []IdleGreetingTask {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tasks := make([]IdleGreetingTask, 0, len(m.tasks))
	for _, task := range m.tasks {
		if task == nil {
			continue
		}
		tasks = append(tasks, *cloneIdleGreetingTask(task))
	}

	sort.Slice(tasks, func(i, j int) bool {
		if !tasks[i].DueAt.Equal(tasks[j].DueAt) {
			return tasks[i].DueAt.Before(tasks[j].DueAt)
		}
		if !tasks[i].CreatedAt.Equal(tasks[j].CreatedAt) {
			return tasks[i].CreatedAt.Before(tasks[j].CreatedAt)
		}
		return tasks[i].ID < tasks[j].ID
	})
	return tasks
}

func (m *IdleGreetingManager) Get(id string) (*IdleGreetingTask, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	task, ok := m.tasks[strings.TrimSpace(id)]
	if !ok {
		return nil, false
	}
	return cloneIdleGreetingTask(task), true
}

func (m *IdleGreetingManager) UpsertPending(task IdleGreetingTask) (*IdleGreetingTask, error) {
	normalized, err := normalizeIdleGreetingTask(task)
	if err != nil {
		return nil, err
	}
	normalized.Status = IdleGreetingStatusPending

	m.mu.Lock()
	defer m.mu.Unlock()

	if old := m.tasks[normalized.ID]; old != nil {
		normalized.Revision = old.Revision + 1
	}
	normalized.DeliveryID = ""
	for existingID, existing := range m.tasks {
		if existing == nil || existing.ID == normalized.ID {
			continue
		}
		if existing.Status != IdleGreetingStatusPending || existing.Key != normalized.Key {
			continue
		}
		delete(m.tasks, existingID)
	}

	m.tasks[normalized.ID] = cloneIdleGreetingTask(&normalized)
	if errSave := m.saveLocked(); errSave != nil {
		return nil, errSave
	}
	return cloneIdleGreetingTask(&normalized), nil
}

func (m *IdleGreetingManager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	if _, ok := m.tasks[id]; !ok {
		return os.ErrNotExist
	}
	delete(m.tasks, id)
	return m.saveLocked()
}

func (m *IdleGreetingManager) DeletePendingByKey(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}

	changed := false
	for id, task := range m.tasks {
		if task == nil || task.Status != IdleGreetingStatusPending || task.Key != key {
			continue
		}
		delete(m.tasks, id)
		changed = true
	}
	if !changed {
		return nil
	}
	return m.saveLocked()
}

func (m *IdleGreetingManager) Clear() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.tasks) == 0 {
		return nil
	}
	m.tasks = make(map[string]*IdleGreetingTask)
	return m.saveLocked()
}

func (m *IdleGreetingManager) HasNewerTaskForKey(key string, lastUserAt time.Time, excludeID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key = strings.TrimSpace(key)
	excludeID = strings.TrimSpace(excludeID)
	if key == "" || lastUserAt.IsZero() {
		return false
	}

	for _, task := range m.tasks {
		if task == nil || task.ID == excludeID || task.Key != key {
			continue
		}
		if task.LastUserAt.After(lastUserAt) {
			return true
		}
	}
	return false
}

func (m *IdleGreetingManager) ListDuePending(now time.Time) []IdleGreetingTask {
	m.mu.RLock()
	defer m.mu.RUnlock()

	due := make([]IdleGreetingTask, 0)
	for _, task := range m.tasks {
		if task == nil || task.Status != IdleGreetingStatusPending {
			continue
		}
		if task.DueAt.After(now) {
			continue
		}
		due = append(due, *cloneIdleGreetingTask(task))
	}

	sort.Slice(due, func(i, j int) bool {
		if due[i].DueAt.Equal(due[j].DueAt) {
			return due[i].CreatedAt.Before(due[j].CreatedAt)
		}
		return due[i].DueAt.Before(due[j].DueAt)
	})
	return due
}

func (m *IdleGreetingManager) TryMarkFiring(id string, updatedAt time.Time) (*IdleGreetingTask, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	task, ok := m.tasks[strings.TrimSpace(id)]
	if !ok {
		return nil, false, os.ErrNotExist
	}
	if task.Status != IdleGreetingStatusPending {
		return cloneIdleGreetingTask(task), false, nil
	}
	if task.DueAt.After(updatedAt) {
		return cloneIdleGreetingTask(task), false, nil
	}

	task.Status = IdleGreetingStatusFiring
	task.Attempts++
	task.LastError = ""
	task.UpdatedAt = updatedAt

	if errSave := m.saveLocked(); errSave != nil {
		return nil, false, errSave
	}
	return cloneIdleGreetingTask(task), true, nil
}

func (m *IdleGreetingManager) MarkPending(id string, updatedAt time.Time, lastError string, nextDueAt time.Time) (*IdleGreetingTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	task, ok := m.tasks[strings.TrimSpace(id)]
	if !ok {
		return nil, os.ErrNotExist
	}
	if task.Status != IdleGreetingStatusFiring {
		return nil, os.ErrInvalid
	}

	lastError = strings.TrimSpace(lastError)
	if lastError == "" {
		lastError = "unknown idle greeting error"
	}

	task.Status = IdleGreetingStatusPending
	task.LastError = lastError
	if !nextDueAt.IsZero() {
		task.DueAt = nextDueAt
	}
	task.UpdatedAt = updatedAt

	if errSave := m.saveLocked(); errSave != nil {
		return nil, errSave
	}
	return cloneIdleGreetingTask(task), nil
}

func cloneIdleGreetingTask(task *IdleGreetingTask) *IdleGreetingTask {
	if task == nil {
		return nil
	}
	cloned := *task
	return &cloned
}

func generateIdleGreetingTaskID() string {
	raw := make([]byte, 16)
	_, _ = rand.Read(raw)
	return hex.EncodeToString(raw)
}

func BuildIdleGreetingTaskKey(channel ReminderChannel, sessionID string, target ReminderTarget) string {
	switch channel {
	case ReminderChannelWeb:
		return "web|" + strings.TrimSpace(sessionID)
	case ReminderChannelClawBot:
		return "clawbot|" + strings.TrimSpace(target.UserID)
	case ReminderChannelNapCat:
		return "napcat|" + strings.TrimSpace(target.BotSelfID) + "|" + strings.TrimSpace(target.UserID)
	default:
		return ""
	}
}

func normalizeIdleGreetingTask(task IdleGreetingTask) (IdleGreetingTask, error) {
	task.ID = strings.TrimSpace(task.ID)
	task.SessionID = strings.TrimSpace(task.SessionID)
	task.PromptID = strings.TrimSpace(task.PromptID)
	task.PromptName = strings.TrimSpace(task.PromptName)
	task.LastError = strings.TrimSpace(task.LastError)

	if task.ID == "" {
		task.ID = generateIdleGreetingTaskID()
	}
	if err := ValidateID(task.ID); err != nil {
		return IdleGreetingTask{}, err
	}
	if err := ValidateID(task.SessionID); err != nil {
		return IdleGreetingTask{}, err
	}
	if task.PromptID != "" {
		if err := ValidateID(task.PromptID); err != nil {
			return IdleGreetingTask{}, err
		}
	}
	if task.PromptName == "" && task.PromptID != "" {
		task.PromptName = task.PromptID
	}
	if task.DueAt.IsZero() || task.LastUserAt.IsZero() {
		return IdleGreetingTask{}, os.ErrInvalid
	}

	target, errNormalizeTarget := normalizeReminderTarget(task.Channel, task.Target, "")
	if errNormalizeTarget != nil {
		return IdleGreetingTask{}, errNormalizeTarget
	}
	task.Target = target
	task.Key = BuildIdleGreetingTaskKey(task.Channel, task.SessionID, task.Target)
	if strings.TrimSpace(task.Key) == "" {
		return IdleGreetingTask{}, os.ErrInvalid
	}

	if task.Status == "" {
		task.Status = IdleGreetingStatusPending
	}
	switch task.Status {
	case IdleGreetingStatusPending, IdleGreetingStatusFiring:
	default:
		return IdleGreetingTask{}, os.ErrInvalid
	}
	if task.Attempts < 0 {
		task.Attempts = 0
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = task.UpdatedAt
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = task.LastUserAt
	}
	if task.UpdatedAt.IsZero() {
		task.UpdatedAt = task.CreatedAt
	}
	return task, nil
}

func (m *IdleGreetingManager) snapshotLocked() map[string]*IdleGreetingTask {
	out := make(map[string]*IdleGreetingTask, len(m.tasks))
	for id, item := range m.tasks {
		out[id] = cloneIdleGreetingTask(item)
	}
	return out
}

func idleGreetingDeliveryMatches(current *IdleGreetingTask, snapshot IdleGreetingTask, deliveryID string) bool {
	return current != nil && current.Revision == snapshot.Revision && current.CreatedAt.Equal(snapshot.CreatedAt) &&
		current.LastUserAt.Equal(snapshot.LastUserAt) && current.DueAt.Equal(snapshot.DueAt) &&
		(current.Status == IdleGreetingStatusPending || current.Status == IdleGreetingStatusFiring) &&
		(current.DeliveryID == "" || current.DeliveryID == deliveryID)
}

func (m *IdleGreetingManager) BindDelivery(snapshot IdleGreetingTask, deliveryID string, now time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current := m.tasks[snapshot.ID]
	if deliveryID == "" || !idleGreetingDeliveryMatches(current, snapshot, deliveryID) {
		return false, nil
	}
	for _, other := range m.tasks {
		if other.Key == current.Key && other.LastUserAt.After(current.LastUserAt) {
			return false, nil
		}
	}
	if current.DeliveryID == deliveryID {
		return true, nil
	}
	current.DeliveryID = deliveryID
	current.Status = IdleGreetingStatusFiring
	current.UpdatedAt = now
	if err := m.saveLocked(); err != nil {
		return false, err
	}
	return true, nil
}

func (m *IdleGreetingManager) CompleteDelivery(snapshot IdleGreetingTask, deliveryID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if deliveryID == "" || !idleGreetingDeliveryMatches(m.tasks[snapshot.ID], snapshot, deliveryID) {
		return nil
	}
	delete(m.tasks, snapshot.ID)
	return m.saveLocked()
}
