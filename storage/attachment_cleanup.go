package storage

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// CleanupAttachmentsBeforeStart reclaims old orphaned attachments. It MUST run
// before managers, receivers and delivery workers start, with exclusive ownership
// of dataDir. Never call it from DeleteSession: an in-flight receive, legacy
// pending batch or outgoing queue may still own bytes absent from chat history.
//
// All JSON/JSONL snapshots are roots (including completed delivery jobs and the
// reference cache). We inspect every string, including SourcePath, tool results
// and nested JSON strings. Unreadable/corrupt snapshots and symlinks fail closed.
// Session deletion therefore reclaims bytes on a later restart, not immediately.
func CleanupAttachmentsBeforeStart(dataDir string, now time.Time) (int, error) {
	store := AttachmentStore{Dir: filepath.Join(dataDir, "attachments")}
	entries, err := os.ReadDir(store.Dir)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	candidates := make(map[string]string)
	for _, entry := range entries {
		id := entry.Name()
		_, idErr := attachmentID(AttachmentURLPrefix + id)
		if idErr != nil && !attachmentTempName.MatchString(id) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return 0, err
		}
		// Keep newly received files and all unexpected filesystem objects.
		if info.Mode().IsRegular() && info.ModTime().Before(now.Add(-24*time.Hour)) {
			candidates[id] = AttachmentURLPrefix + id
			if idErr != nil {
				candidates[id] = "" // interrupted Save; never a published reference
			}
		}
	}
	if len(candidates) == 0 {
		return 0, nil
	}
	err = filepath.WalkDir(dataDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("attachment cleanup refused symlink in data directory")
		}
		if path == store.Dir {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".json" && ext != ".jsonl" {
			return nil
		}
		if !entry.Type().IsRegular() {
			return errors.New("attachment cleanup refused non-regular snapshot")
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		dec := json.NewDecoder(f)
		// Decode each JSONL record separately; malformed trailing data aborts the
		// entire mark phase, before any deletion. UseNumber accepts large IDs.
		dec.UseNumber()
		for {
			var value interface{}
			err := dec.Decode(&value)
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}
			markAttachmentReferences(value, candidates)
		}
	})
	if err != nil {
		return 0, err
	}
	removed := 0
	root, err := os.OpenRoot(store.Dir)
	if err != nil {
		return 0, err
	}
	defer root.Close()
	for id, path := range candidates {
		var err error
		if path == "" {
			err = root.Remove(id)
		} else {
			err = store.Remove(path)
		}
		if err != nil && !os.IsNotExist(err) {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

var attachmentTempName = regexp.MustCompile(`^\.incoming-[0-9]+$`)
var attachmentReferenceID = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

func markAttachmentReferences(value interface{}, candidates map[string]string) {
	switch v := value.(type) {
	case string:
		// Matching the ID rather than the URL also retains paths embedded in JSON
		// tool content, escaped URLs and references to the absolute local filename.
		for _, id := range attachmentReferenceID.FindAllString(v, -1) {
			delete(candidates, id)
		}
		// Tool results/reference payloads may themselves be JSON strings with
		// escaped characters; decoding decreases the nesting at every step.
		if strings.Contains(v, `\u`) {
			var nested interface{}
			if json.Unmarshal([]byte(v), &nested) == nil {
				markAttachmentReferences(nested, candidates)
			}
		}
	case []interface{}:
		for _, item := range v {
			markAttachmentReferences(item, candidates)
		}
	case map[string]interface{}:
		for key, item := range v {
			markAttachmentReferences(key, candidates)
			markAttachmentReferences(item, candidates)
		}
	}
}
