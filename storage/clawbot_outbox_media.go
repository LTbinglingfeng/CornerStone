package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const ClawBotDeliveryMediaMaxBytes = 100 << 20
const clawBotDeliveryMediaQuota = 1 << 30

// Delivery blobs follow the state file's migration alias, not the credential
// hash. Rebinding the same account therefore preserves pending uploads.
func (m *ClawBotStateManager) mediaDir(account string) (string, error) {
	p, err := m.resolvePath(account)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(p, ".json") + "-media", nil
}

func validDeliveryBlob(id string) bool {
	if len(id) != 64 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil && strings.ToLower(id) == id
}

// SaveDeliveryMedia snapshots bytes before any upload; later source edits cannot
// silently change a pending message. Blobs are immutable and content addressed.
func (m *ClawBotStateManager) SaveDeliveryMedia(account string, data []byte) (string, error) {
	if len(data) == 0 || len(data) > ClawBotDeliveryMediaMaxBytes {
		return "", errors.New("invalid delivery media size")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	dir, err := m.mediaDir(account)
	if err != nil {
		return "", err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", err
	}
	defer root.Close()
	digest := sha256.Sum256(data)
	id := hex.EncodeToString(digest[:])
	if existing, err := readDeliveryBlob(root, id); err == nil {
		if sha256.Sum256(existing) != digest {
			return "", errors.New("corrupt delivery media snapshot")
		}
		return id, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var used int64
	for _, entry := range entries {
		if info, e := entry.Info(); e == nil {
			used += info.Size()
		}
	}
	if used+int64(len(data)) > clawBotDeliveryMediaQuota {
		return "", errors.New("delivery media quota exceeded; clean unused blobs")
	}
	f, err := os.CreateTemp(dir, ".upload-*.tmp")
	if err != nil {
		return "", err
	}
	temp := f.Name()
	defer os.Remove(temp)
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	if err = root.Rename(filepath.Base(temp), id); err != nil {
		return "", err
	}
	// Windows cannot sync directory handles; the blob itself was synced before rename.
	if runtime.GOOS == "windows" {
		return id, nil
	}
	d, err := os.Open(dir)
	if err == nil {
		err = d.Sync()
		_ = d.Close()
	}
	if err != nil {
		return "", err
	}
	return id, nil
}

func readDeliveryBlob(root *os.Root, id string) ([]byte, error) {
	if !validDeliveryBlob(id) {
		return nil, errors.New("invalid delivery blob ID")
	}
	info, err := root.Lstat(id)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > ClawBotDeliveryMediaMaxBytes {
		return nil, errors.New("invalid delivery blob")
	}
	f, err := root.Open(id)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, ClawBotDeliveryMediaMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > ClawBotDeliveryMediaMaxBytes {
		return nil, errors.New("delivery blob exceeds size limit")
	}
	return data, nil
}

func (m *ClawBotStateManager) ReadDeliveryMedia(account, id string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	dir, err := m.mediaDir(account)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	data, err := readDeliveryBlob(root, id)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != id {
		return nil, fmt.Errorf("delivery media integrity check failed")
	}
	return data, nil
}

// CleanupDeliveryMedia only removes unreferenced blobs older than one day.
// A staged write not yet linked into the queue is protected by this grace period.
// Cancelled/audit jobs retain references until normal queue retention prunes them.
func (m *ClawBotStateManager) CleanupDeliveryMedia(account string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	statePath, err := m.resolvePath(account)
	if err != nil {
		return err
	}
	state, err := loadClawBotState(statePath)
	if err != nil {
		return err
	}
	keep := map[string]bool{}
	if state.Delivery != nil {
		for _, j := range state.Delivery.Jobs {
			for _, c := range j.Chunks {
				if c.Media != nil {
					keep[c.Media.BlobID] = true
				}
			}
		}
	}
	dir := strings.TrimSuffix(statePath, ".json") + "-media"
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, entry := range entries {
		name := entry.Name()
		if keep[name] || (!validDeliveryBlob(name) && !strings.HasPrefix(name, ".upload-")) {
			continue
		}
		info, e := entry.Info()
		if e != nil {
			continue
		}
		if now.Sub(info.ModTime()) < 24*time.Hour {
			continue
		}
		if e = root.Remove(name); e != nil && !errors.Is(e, os.ErrNotExist) {
			return e
		}
	}
	return nil
}
