package storage

import (
	"cornerstone/logging"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type ReminderChannel string

const (
	ReminderChannelWeb     ReminderChannel = "web"
	ReminderChannelClawBot ReminderChannel = "clawbot"
)

type ReminderStatus string

const (
	ReminderStatusPending   ReminderStatus = "pending"
	ReminderStatusFiring    ReminderStatus = "firing"
	ReminderStatusSent      ReminderStatus = "sent"
	ReminderStatusFailed    ReminderStatus = "failed"
	ReminderStatusCancelled ReminderStatus = "cancelled"
)

var ErrReminderImmutable = errors.New("reminder is not editable")

type Reminder struct {
	ID             string          `json:"id"`
	Channel        ReminderChannel `json:"channel"`
	SessionID      string          `json:"session_id"`
	PromptID       string          `json:"prompt_id"`
	PromptName     string          `json:"prompt_name,omitempty"`
	ClawBotUserID  string          `json:"clawbot_user_id,omitempty"`
	Title          string          `json:"title"`
	ReminderPrompt string          `json:"reminder_prompt"`
	DueAt          time.Time       `json:"due_at"`
	Status         ReminderStatus  `json:"status"`
	Attempts       int             `json:"attempts"`
	LastError      string          `json:"last_error,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	FiredAt        *time.Time      `json:"fired_at,omitempty"`
}

type ReminderPatch struct {
	Title          *string
	ReminderPrompt *string
	DueAt          *time.Time
}

type ReminderManager struct {
	filePath  string
	mu        sync.RWMutex
	reminders map[string]*Reminder
}

func NewReminderManager(dataDir string) *ReminderManager {
	_ = os.MkdirAll(dataDir, 0755)

	manager := &ReminderManager{
		filePath:  filepath.Join(dataDir, "reminders.json"),
		reminders: make(map[string]*Reminder),
	}
	manager.load()
	return manager
}

func (m *ReminderManager) load() {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(m.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		logging.Errorf("reminders load failed: path=%s err=%v", m.filePath, err)
		return
	}

	var stored []Reminder
	if err := json.Unmarshal(data, &stored); err != nil {
		logging.Errorf("reminders parse failed: path=%s err=%v", m.filePath, err)
		return
	}

	changed := false
	next := make(map[string]*Reminder, len(stored))
	for i := range stored {
		normalized, errNormalize := normalizeReminder(stored[i])
		if errNormalize != nil {
			logging.Warnf("ignore invalid reminder while loading: id=%s err=%v", stored[i].ID, errNormalize)
			changed = true
			continue
		}
		if normalized.Status == ReminderStatusFiring {
			normalized.Status = ReminderStatusPending
			normalized.LastError = ""
			changed = true
		}
		next[normalized.ID] = cloneReminder(&normalized)
	}
	m.reminders = next

	if changed {
		if errSave := m.saveLocked(); errSave != nil {
			logging.Errorf("reminders normalize save failed: path=%s err=%v", m.filePath, errSave)
		}
	}
}

func (m *ReminderManager) saveLocked() error {
	reminders := make([]Reminder, 0, len(m.reminders))
	for _, reminder := range m.reminders {
		if reminder == nil {
			continue
		}
		reminders = append(reminders, *cloneReminder(reminder))
	}

	sort.Slice(reminders, func(i, j int) bool {
		if reminders[i].CreatedAt.Equal(reminders[j].CreatedAt) {
			return reminders[i].ID < reminders[j].ID
		}
		return reminders[i].CreatedAt.Before(reminders[j].CreatedAt)
	})

	data, err := json.MarshalIndent(reminders, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(m.filePath, data, 0600); err != nil {
		return err
	}
	_ = os.Chmod(m.filePath, 0600)
	return nil
}

func (m *ReminderManager) List() []Reminder {
	m.mu.RLock()
	defer m.mu.RUnlock()

	reminders := make([]Reminder, 0, len(m.reminders))
	for _, reminder := range m.reminders {
		if reminder == nil {
			continue
		}
		reminders = append(reminders, *cloneReminder(reminder))
	}

	sort.Slice(reminders, func(i, j int) bool {
		leftRank := reminderStatusRank(reminders[i].Status)
		rightRank := reminderStatusRank(reminders[j].Status)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if !reminders[i].DueAt.Equal(reminders[j].DueAt) {
			return reminders[i].DueAt.Before(reminders[j].DueAt)
		}
		if !reminders[i].CreatedAt.Equal(reminders[j].CreatedAt) {
			return reminders[i].CreatedAt.After(reminders[j].CreatedAt)
		}
		return reminders[i].ID < reminders[j].ID
	})
	return reminders
}

func (m *ReminderManager) Get(id string) (*Reminder, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	reminder, ok := m.reminders[id]
	if !ok {
		return nil, false
	}
	return cloneReminder(reminder), true
}

func (m *ReminderManager) Create(reminder Reminder) (*Reminder, error) {
	normalized, err := normalizeReminder(reminder)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.reminders[normalized.ID]; exists {
		return nil, os.ErrExist
	}
	m.reminders[normalized.ID] = cloneReminder(&normalized)
	if err := m.saveLocked(); err != nil {
		delete(m.reminders, normalized.ID)
		return nil, err
	}
	return cloneReminder(&normalized), nil
}

func (m *ReminderManager) UpdatePending(id string, patch ReminderPatch, updatedAt time.Time) (*Reminder, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	reminder, ok := m.reminders[id]
	if !ok {
		return nil, os.ErrNotExist
	}
	if reminder.Status != ReminderStatusPending {
		return nil, ErrReminderImmutable
	}

	if patch.Title != nil {
		reminder.Title = strings.TrimSpace(*patch.Title)
	}
	if patch.ReminderPrompt != nil {
		reminder.ReminderPrompt = strings.TrimSpace(*patch.ReminderPrompt)
	}
	if patch.DueAt != nil {
		reminder.DueAt = *patch.DueAt
	}
	reminder.UpdatedAt = updatedAt

	normalized, err := normalizeReminder(*reminder)
	if err != nil {
		return nil, err
	}
	m.reminders[id] = cloneReminder(&normalized)
	if err := m.saveLocked(); err != nil {
		return nil, err
	}
	return cloneReminder(&normalized), nil
}

func (m *ReminderManager) CancelPending(id string, updatedAt time.Time) (*Reminder, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	reminder, ok := m.reminders[id]
	if !ok {
		return nil, os.ErrNotExist
	}
	if reminder.Status != ReminderStatusPending {
		return nil, ErrReminderImmutable
	}

	reminder.Status = ReminderStatusCancelled
	reminder.LastError = ""
	reminder.UpdatedAt = updatedAt

	if err := m.saveLocked(); err != nil {
		return nil, err
	}
	return cloneReminder(reminder), nil
}

func (m *ReminderManager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.reminders[id]; !ok {
		return os.ErrNotExist
	}
	delete(m.reminders, id)
	return m.saveLocked()
}

func (m *ReminderManager) ListDuePending(now time.Time) []Reminder {
	m.mu.RLock()
	defer m.mu.RUnlock()

	due := make([]Reminder, 0)
	for _, reminder := range m.reminders {
		if reminder == nil || reminder.Status != ReminderStatusPending {
			continue
		}
		if reminder.DueAt.After(now) {
			continue
		}
		due = append(due, *cloneReminder(reminder))
	}

	sort.Slice(due, func(i, j int) bool {
		if due[i].DueAt.Equal(due[j].DueAt) {
			return due[i].CreatedAt.Before(due[j].CreatedAt)
		}
		return due[i].DueAt.Before(due[j].DueAt)
	})
	return due
}

func (m *ReminderManager) TryMarkFiring(id string, updatedAt time.Time) (*Reminder, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	reminder, ok := m.reminders[id]
	if !ok {
		return nil, false, os.ErrNotExist
	}
	if reminder.Status != ReminderStatusPending {
		return cloneReminder(reminder), false, nil
	}
	// Recheck due-at at the state transition boundary to avoid races where
	// a reminder was listed as due but later edited to a future time.
	if reminder.DueAt.After(updatedAt) {
		return cloneReminder(reminder), false, nil
	}

	reminder.Status = ReminderStatusFiring
	reminder.Attempts++
	reminder.LastError = ""
	reminder.UpdatedAt = updatedAt

	if err := m.saveLocked(); err != nil {
		return nil, false, err
	}
	return cloneReminder(reminder), true, nil
}

func (m *ReminderManager) MarkSent(id string, firedAt time.Time) (*Reminder, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	reminder, ok := m.reminders[id]
	if !ok {
		return nil, os.ErrNotExist
	}

	reminder.Status = ReminderStatusSent
	reminder.LastError = ""
	reminder.UpdatedAt = firedAt
	firedAtCopy := firedAt
	reminder.FiredAt = &firedAtCopy

	if err := m.saveLocked(); err != nil {
		return nil, err
	}
	return cloneReminder(reminder), nil
}

func (m *ReminderManager) MarkFailed(id string, updatedAt time.Time, lastError string) (*Reminder, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	reminder, ok := m.reminders[id]
	if !ok {
		return nil, os.ErrNotExist
	}

	lastError = strings.TrimSpace(lastError)
	if lastError == "" {
		lastError = "unknown reminder error"
	}

	reminder.Status = ReminderStatusFailed
	reminder.LastError = lastError
	reminder.UpdatedAt = updatedAt

	if err := m.saveLocked(); err != nil {
		return nil, err
	}
	return cloneReminder(reminder), nil
}

func cloneReminder(reminder *Reminder) *Reminder {
	if reminder == nil {
		return nil
	}
	cloned := *reminder
	if reminder.FiredAt != nil {
		firedAt := *reminder.FiredAt
		cloned.FiredAt = &firedAt
	}
	return &cloned
}

func reminderStatusRank(status ReminderStatus) int {
	switch status {
	case ReminderStatusPending:
		return 0
	case ReminderStatusFiring:
		return 1
	case ReminderStatusSent:
		return 2
	case ReminderStatusFailed:
		return 3
	case ReminderStatusCancelled:
		return 4
	default:
		return 9
	}
}

func normalizeReminder(reminder Reminder) (Reminder, error) {
	reminder.ID = strings.TrimSpace(reminder.ID)
	reminder.SessionID = strings.TrimSpace(reminder.SessionID)
	reminder.PromptID = strings.TrimSpace(reminder.PromptID)
	reminder.PromptName = strings.TrimSpace(reminder.PromptName)
	reminder.ClawBotUserID = strings.TrimSpace(reminder.ClawBotUserID)
	reminder.Title = strings.TrimSpace(reminder.Title)
	reminder.ReminderPrompt = strings.TrimSpace(reminder.ReminderPrompt)
	reminder.LastError = strings.TrimSpace(reminder.LastError)

	if err := ValidateID(reminder.ID); err != nil {
		return Reminder{}, err
	}
	if err := ValidateID(reminder.SessionID); err != nil {
		return Reminder{}, err
	}
	if err := ValidateID(reminder.PromptID); err != nil {
		return Reminder{}, err
	}
	if reminder.PromptName == "" {
		reminder.PromptName = reminder.PromptID
	}
	if reminder.Title == "" {
		return Reminder{}, os.ErrInvalid
	}
	if reminder.ReminderPrompt == "" {
		return Reminder{}, os.ErrInvalid
	}
	if reminder.DueAt.IsZero() {
		return Reminder{}, os.ErrInvalid
	}
	if reminder.Status == "" {
		reminder.Status = ReminderStatusPending
	}
	switch reminder.Channel {
	case ReminderChannelWeb:
	case ReminderChannelClawBot:
		if reminder.ClawBotUserID == "" {
			return Reminder{}, os.ErrInvalid
		}
	default:
		return Reminder{}, os.ErrInvalid
	}
	switch reminder.Status {
	case ReminderStatusPending, ReminderStatusFiring, ReminderStatusSent, ReminderStatusFailed, ReminderStatusCancelled:
	default:
		return Reminder{}, os.ErrInvalid
	}
	if reminder.Attempts < 0 {
		reminder.Attempts = 0
	}
	if reminder.CreatedAt.IsZero() {
		reminder.CreatedAt = reminder.UpdatedAt
	}
	if reminder.CreatedAt.IsZero() {
		reminder.CreatedAt = reminder.DueAt
	}
	if reminder.UpdatedAt.IsZero() {
		reminder.UpdatedAt = reminder.CreatedAt
	}
	return reminder, nil
}
