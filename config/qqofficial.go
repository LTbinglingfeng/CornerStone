package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type QQOfficialConfig struct {
	Enabled            bool     `json:"enabled"`
	AppID              string   `json:"app_id"`
	AppSecret          string   `json:"app_secret"`
	PromptID           string   `json:"prompt_id"`
	SourceFilterMode   string   `json:"source_filter_mode"`
	AllowedUserOpenIDs []string `json:"allowed_user_openids"`
}

func normalizeQQOfficial(c QQOfficialConfig) QQOfficialConfig {
	c.AppID = strings.TrimSpace(c.AppID)
	c.AppSecret = strings.TrimSpace(c.AppSecret)
	c.PromptID = strings.TrimSpace(c.PromptID)
	c.SourceFilterMode = strings.TrimSpace(c.SourceFilterMode)
	if c.SourceFilterMode == "" {
		c.SourceFilterMode = "all"
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, id := range c.AllowedUserOpenIDs {
		id = strings.TrimSpace(id)
		if id != "" && !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	c.AllowedUserOpenIDs = ids
	return c
}
func (m *Manager) GetQQOfficialConfig() QQOfficialConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return normalizeQQOfficial(m.config.QQOfficial)
}
func (m *Manager) UpdateQQOfficialConfig(c QQOfficialConfig) error {
	c = normalizeQQOfficial(c)
	if c.SourceFilterMode != "all" && c.SourceFilterMode != "allowlist" {
		return fmt.Errorf("invalid source filter mode")
	}
	if c.Enabled && (c.AppID == "" || c.AppSecret == "") {
		return fmt.Errorf("AppID and AppSecret required")
	}
	for _, r := range c.AppID {
		if r < '0' || r > '9' {
			return fmt.Errorf("AppID must contain digits only")
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	previous := m.config.QQOfficial
	m.config.QQOfficial = c
	if err := m.saveQQOfficialUnsafe(); err != nil {
		m.config.QQOfficial = previous
		return err
	}
	return nil
}

// Persist the entire snapshot with one atomic rename so credentials cannot be
// partially replaced on a failed write. Caller holds m.mu.
func (m *Manager) saveQQOfficialUnsafe() error {
	data, err := json.MarshalIndent(m.config, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(m.configPath), ".qqofficial-config-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(name, m.configPath)
}
