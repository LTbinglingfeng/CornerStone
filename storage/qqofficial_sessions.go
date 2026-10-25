package storage

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
)

const (
	qqOfficialSessionLimit    = 10000
	qqOfficialSessionMaxBytes = 4 << 20
)

// QQOfficialSessionStore persists active session selections by opaque scope.
// Use one instance per path in an application-owned directory; the mutex does
// not coordinate separate instances or processes. Chat existence and scope
// ownership are the caller's responsibility.
type QQOfficialSessionStore struct {
	mu       sync.Mutex
	path     string
	loaded   bool
	sessions map[string]string
}

func NewQQOfficialSessionStore(path string) *QQOfficialSessionStore {
	return &QQOfficialSessionStore{path: path}
}

// Get returns an empty string when the scope has no selection.
func (s *QQOfficialSessionStore) Get(scope string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ValidateID(scope); err != nil {
		return "", err
	}
	if err := s.load(); err != nil {
		return "", err
	}
	return s.sessions[scope], nil
}

// Set durably selects a session; an empty sessionID removes the selection.
// A failed save leaves the in-memory selection unchanged.
func (s *QQOfficialSessionStore) Set(scope, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ValidateID(scope); err != nil {
		return err
	}
	if sessionID != "" {
		if err := ValidateID(sessionID); err != nil {
			return err
		}
	}
	if err := s.load(); err != nil {
		return err
	}
	if s.sessions[scope] == sessionID {
		return nil
	}
	next := make(map[string]string, len(s.sessions)+1)
	for k, v := range s.sessions {
		next[k] = v
	}
	if sessionID == "" {
		delete(next, scope)
	} else {
		next[scope] = sessionID
	}
	if len(next) > qqOfficialSessionLimit {
		return errors.New("QQ official session capacity reached")
	}
	data, err := json.Marshal(next)
	if err != nil {
		return errors.New("encode QQ official sessions failed")
	}
	if len(data) > qqOfficialSessionMaxBytes {
		return errors.New("QQ official sessions exceed size limit")
	}
	if err := s.writeAtomic(data); err != nil {
		return err
	}
	s.sessions = next
	return nil
}

func (s *QQOfficialSessionStore) load() error {
	if err := s.checkPath(); err != nil {
		return err
	}
	if s.loaded {
		return nil
	}
	f, err := os.Open(s.path)
	if os.IsNotExist(err) {
		s.sessions, s.loaded = make(map[string]string), true
		return nil
	}
	if err != nil {
		return errors.New("open QQ official sessions failed")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("invalid QQ official session file")
	}
	if info.Size() > qqOfficialSessionMaxBytes {
		return errors.New("QQ official sessions exceed size limit")
	}
	data, err := io.ReadAll(io.LimitReader(f, qqOfficialSessionMaxBytes+1))
	if err != nil {
		return errors.New("read QQ official sessions failed")
	}
	if len(data) > qqOfficialSessionMaxBytes {
		return errors.New("QQ official sessions exceed size limit")
	}
	// Reuse the strict object decoder to reject duplicate keys and trailing data.
	raw, err := decodeReceiptObject(data)
	if err != nil {
		return errors.New("invalid QQ official sessions object")
	}
	if len(raw) > qqOfficialSessionLimit {
		return errors.New("QQ official sessions exceed count limit")
	}
	next := make(map[string]string, len(raw))
	for scope, value := range raw {
		var id string
		if ValidateID(scope) != nil || json.Unmarshal(value, &id) != nil || ValidateID(id) != nil {
			return errors.New("invalid QQ official session entry")
		}
		next[scope] = id
	}
	s.sessions, s.loaded = next, true
	return nil
}

// Reject symlinks (including parent components and dangling links) and special
// files. This is not a defense against a hostile writer racing directory changes.
func (s *QQOfficialSessionStore) checkPath() error {
	if s.path == "" {
		return errors.New("invalid QQ official session path")
	}
	p, err := filepath.Abs(s.path)
	if err != nil {
		return errors.New("invalid QQ official session path")
	}
	for current := p; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil && !os.IsNotExist(err) {
			return errors.New("inspect QQ official session path failed")
		}
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return errors.New("QQ official session path contains symlink")
			}
			if current == p && !info.Mode().IsRegular() || current != p && !info.IsDir() {
				return errors.New("invalid QQ official session file type")
			}
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	return nil
}

func (s *QQOfficialSessionStore) writeAtomic(data []byte) error {
	if err := s.checkPath(); err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if os.MkdirAll(dir, 0700) != nil {
		return errors.New("create QQ official session directory failed")
	}
	f, err := os.CreateTemp(dir, ".qqofficial-sessions-*") // 0600
	if err != nil {
		return errors.New("create QQ official session temporary file failed")
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return errors.New("write QQ official sessions failed")
	}
	if f.Sync() != nil {
		return errors.New("sync QQ official sessions failed")
	}
	// Close before rename, including on Windows. Rename is the commit point;
	// no fallible operations occur after it.
	if f.Close() != nil {
		return errors.New("close QQ official sessions failed")
	}
	if err := s.checkPath(); err != nil {
		return err
	}
	if os.Rename(f.Name(), s.path) != nil {
		return errors.New("replace QQ official sessions failed")
	}
	return nil
}
