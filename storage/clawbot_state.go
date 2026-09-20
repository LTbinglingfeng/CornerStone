package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const maxClawBotStateBytes = 8 << 20

// ClawBotState contains polling and reply state, never the bot credential.
type ClawBotState struct {
	GetUpdatesBuf    string            `json:"get_updates_buf"`
	ContextTokens    map[string]string `json:"context_tokens"`
	RecentMessageIDs []string          `json:"recent_message_ids,omitempty"`
}

// ClawBotAccountKey isolates credentials and endpoints without exposing either
// in filenames. Token bytes are significant; URL whitespace, trailing slashes,
// and scheme/host case are not. Paths remain case-sensitive.
func ClawBotAccountKey(baseURL, botToken string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if u, err := url.Parse(baseURL); err == nil && u.Host != "" {
		u.Scheme = strings.ToLower(u.Scheme)
		u.Host = strings.ToLower(u.Host)
		baseURL = u.String()
	}
	// JSON framing avoids ambiguous concatenations of endpoint and token.
	identity, _ := json.Marshal([2]string{baseURL, botToken})
	sum := sha256.Sum256(identity)
	return hex.EncodeToString(sum[:])
}

// ClawBotStateManager serializes access within this instance. Use one manager
// per directory; concurrent writers in other instances/processes are unsupported.
// State is read from disk rather than cached, so every Load is a fresh snapshot.
type ClawBotStateManager struct {
	mu  sync.Mutex
	dir string
}

// NewClawBotStateManager defers filesystem work (and errors) to Load/Save.
func NewClawBotStateManager(dir string) *ClawBotStateManager {
	return &ClawBotStateManager{dir: dir}
}

func (m *ClawBotStateManager) statePath(accountKey string) (string, error) {
	if len(accountKey) != 64 {
		return "", fmt.Errorf("invalid ClawBot account key: expected 64 hex characters")
	}
	if _, err := hex.DecodeString(accountKey); err != nil {
		return "", fmt.Errorf("invalid ClawBot account key: expected 64 hex characters")
	}
	return filepath.Join(m.dir, strings.ToLower(accountKey)+".json"), nil
}

// Load returns an independent snapshot. A missing account has a zero-value state;
// malformed or oversized files return an error and are left untouched.
func (m *ClawBotStateManager) Load(accountKey string) (ClawBotState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	path, err := m.statePath(accountKey)
	if err != nil {
		return ClawBotState{}, err
	}
	return loadClawBotState(path)
}

func loadClawBotState(path string) (ClawBotState, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return ClawBotState{}, nil
	}
	if err != nil {
		return ClawBotState{}, fmt.Errorf("open ClawBot state: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxClawBotStateBytes+1))
	if err != nil {
		return ClawBotState{}, fmt.Errorf("read ClawBot state: %w", err)
	}
	if len(data) > maxClawBotStateBytes {
		return ClawBotState{}, fmt.Errorf("ClawBot state exceeds %d bytes", maxClawBotStateBytes)
	}
	var state ClawBotState
	// null is not a state object, even though json.Unmarshal accepts it.
	if strings.TrimSpace(string(data)) == "null" {
		return ClawBotState{}, fmt.Errorf("invalid ClawBot state: expected object")
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return ClawBotState{}, fmt.Errorf("decode ClawBot state: %w", err)
	}
	return state, nil
}

// Save atomically replaces an account's state, refusing to overwrite corrupt
// existing data. It retains no caller-owned maps or slices after returning.
// Callers must not mutate state concurrently with this call.
func (m *ClawBotStateManager) Save(accountKey string, state ClawBotState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	path, err := m.statePath(accountKey)
	if err != nil {
		return err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode ClawBot state: %w", err)
	}
	if len(data) > maxClawBotStateBytes {
		return fmt.Errorf("ClawBot state exceeds %d bytes", maxClawBotStateBytes)
	}
	if _, err := loadClawBotState(path); err != nil {
		return err
	}
	if err := os.MkdirAll(m.dir, 0700); err != nil {
		return fmt.Errorf("create ClawBot state directory: %w", err)
	}
	if err := os.Chmod(m.dir, 0700); err != nil {
		return fmt.Errorf("secure ClawBot state directory: %w", err)
	}
	f, err := os.CreateTemp(m.dir, ".clawbot-state-*") // mode 0600
	if err != nil {
		return fmt.Errorf("create ClawBot state temporary file: %w", err)
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write ClawBot state: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync ClawBot state: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close ClawBot state: %w", err)
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return fmt.Errorf("replace ClawBot state: %w", err)
	}
	return nil
}
