package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	clawBotReferenceTTL       = 30 * 24 * time.Hour
	clawBotReferenceCapacity  = 2000
	clawBotReferenceBytes     = 16 << 20
	clawBotReferenceItemBytes = 256 << 10
)

// ClawBotReferenceMedia is written only by trusted post-download API code.
// ItemIndex refers to the original raw item list, never synthesized quotes.
type ClawBotReferenceMedia struct {
	ItemIndex  int            `json:"item_index"`
	Attachment ChatAttachment `json:"attachment"`
}

type clawBotReferenceEntry struct {
	LocalMedia []ClawBotReferenceMedia `json:"local_media,omitempty"`
	Key        string                  `json:"key"`
	At         time.Time               `json:"at"`
	Items      json.RawMessage         `json:"items"`
}

func clawBotReferenceKey(account, conversation, id string) string {
	b, _ := json.Marshal([3]string{account, conversation, id})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// ClawBotReferenceConversation includes both the peer and the protocol session.
// Do not substitute the local chat session, or fall back across protocol sessions.
func ClawBotReferenceConversation(peer, session string) string {
	b, _ := json.Marshal([2]string{peer, session})
	return string(b)
}

// FindClawBotReference is a best-effort cache, independent of durable delivery
// state. Reads also prune expired entries, including after process restart.
func (m *ClawBotStateManager) FindClawBotReference(account, conversation, id string) (json.RawMessage, error) {
	return m.clawBotReference(account, conversation, id, nil, time.Time{})
}

// SaveClawBotReference stores the complete raw item list (not credentials or
// parsed attachments in the wire payload). Trusted local metadata is separate.
// Limits are global, so new accounts cannot grow disk use
// without bound. Errors must never prevent receiving or sending a message.
func (m *ClawBotStateManager) SaveClawBotReference(account, conversation, id string, items json.RawMessage) error {
	if len(items) > clawBotReferenceItemBytes || !json.Valid(items) {
		return errors.New("invalid reference cache payload")
	}
	_, err := m.clawBotReference(account, conversation, id, items, time.Time{})
	return err
}

func (m *ClawBotStateManager) FindClawBotReferenceMedia(account, conversation, id string) ([]ClawBotReferenceMedia, error) {
	var local []ClawBotReferenceMedia
	_, err := m.referenceWithMedia(account, conversation, id, nil, time.Time{}, nil, &local)
	return local, err
}

func (m *ClawBotStateManager) SaveClawBotReferenceMedia(account, conversation, id string, media []ClawBotReferenceMedia) error {
	b, err := json.Marshal(media)
	if err != nil || len(b) > clawBotReferenceItemBytes {
		return errors.New("invalid reference metadata")
	}
	_, err = m.referenceWithMedia(account, conversation, id, nil, time.Time{}, media, nil)
	return err
}

func (m *ClawBotStateManager) clawBotReference(account, conversation, id string, items json.RawMessage, now time.Time) (json.RawMessage, error) {
	return m.referenceWithMedia(account, conversation, id, items, now, nil, nil)
}

func (m *ClawBotStateManager) referenceWithMedia(account, conversation, id string, items json.RawMessage, now time.Time, local []ClawBotReferenceMedia, result *[]ClawBotReferenceMedia) (json.RawMessage, error) {
	if account == "" || conversation == "" || id == "" {
		return nil, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	scope, err := m.resolvePath(account)
	if err != nil {
		return nil, err
	}
	account = strings.TrimSuffix(filepath.Base(scope), ".json")
	if now.IsZero() {
		now = time.Now()
	}
	dir := filepath.Join(m.dir, "references")
	path := filepath.Join(dir, "items.json")
	var entries []clawBotReferenceEntry
	f, err := os.Open(path)
	if err == nil {
		b, readErr := io.ReadAll(io.LimitReader(f, clawBotReferenceBytes+1))
		f.Close()
		if readErr != nil {
			return nil, readErr
		}
		// Corruption is cache loss, not a durable-delivery fault.
		if len(b) <= clawBotReferenceBytes {
			_ = json.Unmarshal(b, &entries)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	key := clawBotReferenceKey(account, conversation, id)
	kept := make([]clawBotReferenceEntry, 0, len(entries)+1)
	var found json.RawMessage
	var previous []ClawBotReferenceMedia
	for _, e := range entries {
		if !e.At.After(now.Add(-clawBotReferenceTTL)) || e.At.After(now) || len(e.Items) > clawBotReferenceItemBytes || !json.Valid(e.Items) {
			continue
		}
		metadata, _ := json.Marshal(e.LocalMedia)
		if len(metadata)+len(e.Items) > clawBotReferenceItemBytes {
			continue
		}
		if e.Key == key {
			previous = e.LocalMedia
			if local != nil {
				e.LocalMedia = append([]ClawBotReferenceMedia(nil), local...)
			}
			if result != nil {
				*result = append([]ClawBotReferenceMedia(nil), e.LocalMedia...)
			}
			found = append(json.RawMessage(nil), e.Items...)
			if items != nil {
				continue
			}
		}
		if b, _ := json.Marshal(e.LocalMedia); len(b)+len(e.Items) <= clawBotReferenceItemBytes {
			kept = append(kept, e)
		}
	}
	if items != nil {
		if b, _ := json.Marshal(previous); len(b)+len(items) > clawBotReferenceItemBytes {
			previous = nil
		}
		kept = append(kept, clawBotReferenceEntry{Key: key, At: now, Items: append(json.RawMessage(nil), items...), LocalMedia: previous})
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].At.Before(kept[j].At) })
	if len(kept) > clawBotReferenceCapacity {
		kept = kept[len(kept)-clawBotReferenceCapacity:]
	}
	// Include JSON overhead in the on-disk budget.
	size := 2
	for _, e := range kept {
		b, _ := json.Marshal(e)
		size += len(b) + 1
	}
	for size > clawBotReferenceBytes && len(kept) > 0 {
		b, _ := json.Marshal(kept[0])
		size -= len(b) + 1
		kept = kept[1:]
	}
	data, err := json.Marshal(kept)
	if err != nil {
		return found, err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return found, err
	}
	// A crash before rename leaves only a private temporary file. Remove such
	// leftovers while holding the shared manager lock (one manager per directory).
	leftovers, _ := filepath.Glob(filepath.Join(dir, ".items-*"))
	for _, p := range leftovers {
		_ = os.Remove(p)
	}
	temp, err := os.CreateTemp(dir, ".items-*")
	if err != nil {
		return found, err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	if _, err = temp.Write(data); err != nil {
		return found, err
	}
	if err = temp.Sync(); err != nil {
		return found, err
	}
	if err = temp.Close(); err != nil {
		return found, err
	}
	if err = os.Rename(temp.Name(), path); err != nil {
		return found, err
	}
	if runtime.GOOS != "windows" {
		d, e := os.Open(dir)
		if e != nil {
			return found, e
		}
		err = d.Sync()
		d.Close()
	}
	return found, err
}
