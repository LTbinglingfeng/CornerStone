package storage

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	qqOfficialReceiptTTL      = 30 * 24 * time.Hour
	qqOfficialReceiptLimit    = 100000
	qqOfficialReceiptMaxBytes = 16 << 20
)

// QQOfficialReceiptStore keeps hashed inbound IDs independently of chat history.
// Use one instance per path: the mutex coordinates this instance, not other
// instances or processes. The containing directory must be application-owned.
type QQOfficialReceiptStore struct {
	mu       sync.Mutex
	path     string
	now      func() time.Time
	loaded   bool
	receipts map[string]time.Time
}

func NewQQOfficialReceiptStore(path string) *QQOfficialReceiptStore {
	return &QQOfficialReceiptStore{path: path, now: time.Now}
}

func validQQOfficialReceiptID(id string) bool {
	const prefix = "qqofficial-in-"
	if len(id) != len(prefix)+64 || !strings.HasPrefix(id, prefix) {
		return false
	}
	for _, c := range id[len(prefix):] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// Reject symlinks (including parent components and dangling links) and special
// files. This is not a defense against a hostile writer racing directory changes.
func (s *QQOfficialReceiptStore) checkPath() error {
	if s.path == "" {
		return errors.New("invalid QQ official receipt path")
	}
	p, err := filepath.Abs(s.path)
	if err != nil {
		return errors.New("invalid QQ official receipt path")
	}
	for current := p; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil && !os.IsNotExist(err) {
			return errors.New("inspect QQ official receipt path failed")
		}
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return errors.New("QQ official receipt path contains symlink")
			}
			if current == p && !info.Mode().IsRegular() || current != p && !info.IsDir() {
				return errors.New("invalid QQ official receipt file type")
			}
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	return nil
}

// decodeReceiptObject enforces object shape and rejects duplicate keys instead
// of silently accepting the last value, as json.Unmarshal normally does.
func decodeReceiptObject(data []byte) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errors.New("invalid receipt object")
	}
	result := make(map[string]json.RawMessage)
	for d.More() {
		token, err := d.Token()
		if err != nil {
			return nil, errors.New("invalid receipt object")
		}
		key, ok := token.(string)
		if !ok {
			return nil, errors.New("invalid receipt object")
		}
		if _, exists := result[key]; exists {
			return nil, errors.New("duplicate receipt field")
		}
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return nil, errors.New("invalid receipt object")
		}
		result[key] = value
	}
	if _, err := d.Token(); err != nil {
		return nil, errors.New("invalid receipt object")
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, errors.New("invalid receipt object")
	}
	return result, nil
}

func (s *QQOfficialReceiptStore) load(now time.Time) error {
	if err := s.checkPath(); err != nil {
		return err
	}
	if s.loaded {
		return nil
	}
	f, err := os.Open(s.path)
	if os.IsNotExist(err) {
		s.receipts = make(map[string]time.Time)
		s.loaded = true
		return nil
	}
	if err != nil {
		return errors.New("open QQ official receipts failed")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("invalid QQ official receipt file")
	}
	if info.Size() > qqOfficialReceiptMaxBytes {
		return errors.New("QQ official receipts exceed size limit")
	}
	data, err := io.ReadAll(io.LimitReader(f, qqOfficialReceiptMaxBytes+1))
	if err != nil {
		return errors.New("read QQ official receipts failed")
	}
	if len(data) > qqOfficialReceiptMaxBytes {
		return errors.New("QQ official receipts exceed size limit")
	}
	fields, err := decodeReceiptObject(data)
	if err != nil || len(fields) != 2 {
		return errors.New("invalid QQ official receipt schema")
	}
	var version int
	if json.Unmarshal(fields["version"], &version) != nil || version != 1 {
		return errors.New("unsupported QQ official receipt version")
	}
	raw, err := decodeReceiptObject(fields["receipts"])
	if err != nil {
		return errors.New("invalid QQ official receipts")
	}
	receipts := make(map[string]time.Time)
	for id, value := range raw {
		var timestamp time.Time
		if !validQQOfficialReceiptID(id) || bytes.Equal(bytes.TrimSpace(value), []byte("null")) || json.Unmarshal(value, &timestamp) != nil || timestamp.IsZero() {
			return errors.New("invalid QQ official receipt entry")
		}
		if now.Before(timestamp.Add(qqOfficialReceiptTTL)) {
			receipts[id] = timestamp
		}
	}
	if len(receipts) > qqOfficialReceiptLimit {
		return errors.New("QQ official receipts exceed count limit")
	}
	s.receipts, s.loaded = receipts, true
	return nil
}

func (s *QQOfficialReceiptStore) Seen(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validQQOfficialReceiptID(id) {
		return false, errors.New("invalid QQ official receipt ID")
	}
	now := s.now()
	if err := s.load(now); err != nil {
		return false, err
	}
	timestamp, ok := s.receipts[id]
	return ok && now.Before(timestamp.Add(qqOfficialReceiptTTL)), nil
}

func (s *QQOfficialReceiptStore) Record(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validQQOfficialReceiptID(id) {
		return errors.New("invalid QQ official receipt ID")
	}
	now := s.now().UTC()
	if err := s.load(now); err != nil {
		return err
	}
	if timestamp, ok := s.receipts[id]; ok && now.Before(timestamp.Add(qqOfficialReceiptTTL)) {
		return nil
	}
	next := make(map[string]time.Time, len(s.receipts)+1)
	for key, timestamp := range s.receipts {
		if now.Before(timestamp.Add(qqOfficialReceiptTTL)) {
			next[key] = timestamp
		}
	}
	if len(next) >= qqOfficialReceiptLimit {
		return errors.New("QQ official receipt capacity reached")
	}
	next[id] = now
	snapshot := struct {
		Version  int                  `json:"version"`
		Receipts map[string]time.Time `json:"receipts"`
	}{1, next}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return errors.New("encode QQ official receipts failed")
	}
	if len(data) > qqOfficialReceiptMaxBytes {
		return errors.New("QQ official receipts exceed size limit")
	}
	if err := s.writeAtomic(data); err != nil {
		return err
	}
	s.receipts = next
	return nil
}

func (s *QQOfficialReceiptStore) writeAtomic(data []byte) error {
	if err := s.checkPath(); err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if os.MkdirAll(dir, 0700) != nil {
		return errors.New("create QQ official receipt directory failed")
	}
	f, err := os.CreateTemp(dir, ".qqofficial-receipts-*") // 0600
	if err != nil {
		return errors.New("create QQ official receipt temporary file failed")
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return errors.New("write QQ official receipts failed")
	}
	if f.Sync() != nil {
		return errors.New("sync QQ official receipts failed")
	}
	// Close before rename, including on Windows. Rename is the commit point;
	// no fallible operations occur after it.
	if f.Close() != nil {
		return errors.New("close QQ official receipts failed")
	}
	if err := s.checkPath(); err != nil {
		return err
	}
	if os.Rename(f.Name(), s.path) != nil {
		return errors.New("replace QQ official receipts failed")
	}
	return nil
}
