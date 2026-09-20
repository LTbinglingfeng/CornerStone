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
	"runtime"
	"strings"
	"sync"
)

const maxClawBotStateBytes = 64 << 20

// ClawBotState contains polling and reply state, never the bot credential.
type ClawBotState struct {
	Delivery         *ClawBotDeliveryState `json:"delivery,omitempty"`
	GetUpdatesBuf    string                `json:"get_updates_buf"`
	ContextTokens    map[string]string     `json:"context_tokens"`
	RecentMessageIDs []string              `json:"recent_message_ids,omitempty"`
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
	mu     sync.Mutex
	dir    string
	failed error // An uncertain durable write poisons this instance; never continue sending.
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
	path, err := m.resolvePath(accountKey)
	if err != nil {
		return ClawBotState{}, err
	}
	return loadClawBotState(path)
}

// LoadAudit reads a retired account's snapshot without granting execution or
// write authority. The snapshot remains shared with its active successor.
func (m *ClawBotStateManager) LoadAudit(accountKey string) (ClawBotState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.statePath(accountKey); err != nil {
		return ClawBotState{}, err
	}
	aliases, err := m.aliases()
	if err != nil {
		return ClawBotState{}, err
	}
	key := strings.ToLower(accountKey)
	if _, err := m.migrationTarget(key, aliases); err != nil {
		return ClawBotState{}, err
	}
	path, err := m.statePath(aliases[key].File)
	if err != nil {
		return ClawBotState{}, err
	}
	return loadClawBotState(path)
}

// MigrationTarget returns the unique active successor of a retired source.
// Active sources, broken links, inconsistent snapshots and cycles fail closed.
func (m *ClawBotStateManager) MigrationTarget(source string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.statePath(source); err != nil {
		return "", err
	}
	aliases, err := m.aliases()
	if err != nil {
		return "", err
	}
	return m.migrationTarget(strings.ToLower(source), aliases)
}

func (m *ClawBotStateManager) migrationTarget(source string, aliases map[string]clawBotStateAlias) (string, error) {
	first, ok := aliases[source]
	if !ok || !first.Retired {
		return "", fmt.Errorf("ClawBot account is not retired")
	}
	seen := map[string]bool{}
	key := source
	for {
		if seen[key] {
			return "", fmt.Errorf("ClawBot migration cycle")
		}
		seen[key] = true
		a, ok := aliases[key]
		if !ok || a.File != first.File {
			return "", fmt.Errorf("broken ClawBot migration chain")
		}
		if !a.Retired {
			if a.Successor != "" {
				return "", fmt.Errorf("active ClawBot account has successor")
			}
			for other, alias := range aliases {
				if other != key && !alias.Retired && alias.File == a.File {
					return "", fmt.Errorf("ambiguous ClawBot migration authority")
				}
			}
			return key, nil
		}
		if _, err := m.statePath(a.Successor); err != nil {
			return "", err
		}
		key = strings.ToLower(a.Successor)
	}
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
	path, err := m.resolvePath(accountKey)
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
	return m.writeAtomic(path, data)
}

func (m *ClawBotStateManager) writeAtomic(path string, data []byte) (err error) {
	defer func() {
		if err != nil {
			m.failed = err
		}
	}()
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
	// Go cannot fsync directory handles on Windows; the file has been synced.
	if runtime.GOOS == "windows" {
		return nil
	}
	dir, err := os.Open(m.dir)
	if err != nil {
		return fmt.Errorf("open ClawBot state directory for sync: %w", err)
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("sync ClawBot state directory: %w", err)
	}
	return nil
}

// Aliases select a single physical snapshot. Migration changes only this index,
// atomically: no copying window ever exposes two executable queues. Retired keys
// reject both reads and writes, including stale workers and old configurations.
type clawBotStateAlias struct {
	File      string `json:"file"`
	Retired   bool   `json:"retired,omitempty"`
	Successor string `json:"successor,omitempty"`
}

func (m *ClawBotStateManager) aliases() (map[string]clawBotStateAlias, error) {
	if m.failed != nil {
		return nil, fmt.Errorf("ClawBot state manager requires restart after write failure: %w", m.failed)
	}
	data, err := os.ReadFile(filepath.Join(m.dir, "aliases.json"))
	if os.IsNotExist(err) {
		return map[string]clawBotStateAlias{}, nil
	}
	if err != nil {
		return nil, err
	}
	var aliases map[string]clawBotStateAlias
	if err := json.Unmarshal(data, &aliases); err != nil {
		return nil, err
	}
	if aliases == nil {
		return nil, fmt.Errorf("invalid ClawBot aliases")
	}
	for key, a := range aliases {
		if _, err := m.statePath(key); err != nil {
			return nil, err
		}
		if _, err := m.statePath(a.File); err != nil {
			return nil, err
		}
	}
	return aliases, nil
}

func (m *ClawBotStateManager) resolvePath(key string) (string, error) {
	if _, err := m.statePath(key); err != nil {
		return "", err
	}
	aliases, err := m.aliases()
	if err != nil {
		return "", err
	}
	if a, ok := aliases[strings.ToLower(key)]; ok {
		if a.Retired {
			return "", fmt.Errorf("ClawBot account migrated; retired key cannot execute or save state")
		}
		key = a.File
	}
	return m.statePath(key)
}

// Migrate transfers authority from source to target, retaining all receipts and
// jobs. Caller must stop workers first and prove identical account identity.
// Conflicting snapshots fail closed, never merge or overwrite queues. Retrying
// a completed migration is safe. On error do not start either worker.
func (m *ClawBotStateManager) Migrate(source, target string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	source, target = strings.ToLower(source), strings.ToLower(target)
	if _, err := m.statePath(source); err != nil {
		return err
	}
	if _, err := m.statePath(target); err != nil {
		return err
	}
	aliases, err := m.aliases()
	if err != nil {
		return err
	}
	if a, ok := aliases[source]; ok && a.Retired {
		successor, err := m.migrationTarget(source, aliases)
		if err != nil {
			return err
		}
		if successor == target {
			return nil
		}
		return fmt.Errorf("ClawBot migration source is retired")
	}
	if source == target {
		return nil
	}
	sourceFile, targetFile := source, target
	if a, ok := aliases[source]; ok {
		sourceFile = a.File
	}
	if a, ok := aliases[target]; ok {
		targetFile = a.File
	}
	if a, ok := aliases[target]; ok && a.Retired && a.File != sourceFile {
		return fmt.Errorf("ClawBot migration target is retired")
	}
	sp, _ := m.statePath(sourceFile)
	tp, _ := m.statePath(targetFile)
	if _, err := loadClawBotState(sp); err != nil {
		return err
	}
	if _, err := loadClawBotState(tp); err != nil {
		return err
	}
	_, se := os.Stat(sp)
	_, te := os.Stat(tp)
	if se != nil && !os.IsNotExist(se) {
		return se
	}
	if te != nil && !os.IsNotExist(te) {
		return te
	}
	if se == nil && te == nil && sourceFile != targetFile {
		return fmt.Errorf("ClawBot migration has conflicting snapshots; resolve queues before switching")
	}
	file := sourceFile
	if os.IsNotExist(se) {
		file = targetFile
	}
	aliases[source] = clawBotStateAlias{File: file, Retired: true, Successor: target}
	aliases[target] = clawBotStateAlias{File: file}
	data, err := json.Marshal(aliases)
	if err != nil {
		return err
	}
	return m.writeAtomic(filepath.Join(m.dir, "aliases.json"), data)
}
