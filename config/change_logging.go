package config

import (
	"bytes"
	"cornerstone/logging"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
)

// Only provider state is retained; values are never emitted to logs.
type providerLogState struct {
	Providers []Provider
	ActiveID  string
	ImageID   string
	Fields    map[string]json.RawMessage
}

func (m *Manager) captureProviderLogState() {
	// Providers contain pointer fields: copy through JSON to avoid aliasing updates.
	data, err := json.Marshal(m.config.Providers)
	if err != nil {
		return
	}
	state := &providerLogState{ActiveID: m.config.ActiveProviderID, ImageID: m.config.ImageProviderID}
	if json.Unmarshal(data, &state.Providers) != nil {
		return
	}
	configData, err := json.Marshal(m.config)
	if err != nil || json.Unmarshal(configData, &state.Fields) != nil {
		return
	}
	m.providerLogSnapshot = state
}

func providerChangedFields(before, after Provider) []string {
	a, b := reflect.ValueOf(before), reflect.ValueOf(after)
	fields := make([]string, 0)
	for i := 0; i < a.NumField(); i++ {
		name := strings.Split(a.Type().Field(i).Tag.Get("json"), ",")[0]
		if name != "" && name != "-" && !reflect.DeepEqual(a.Field(i).Interface(), b.Field(i).Interface()) {
			fields = append(fields, name)
		}
	}
	return fields
}

// Called only after a successful save, covering full and partial config updates
// as well as provider-specific methods. Loading/migration does not emit events.
func (m *Manager) logProviderChanges() {
	old := m.providerLogSnapshot
	if old == nil {
		return
	}
	previous := make(map[string]Provider, len(old.Providers))
	for _, p := range old.Providers {
		previous[p.ID] = p
	}
	current := make(map[string]bool, len(m.config.Providers))
	for _, p := range m.config.Providers {
		current[p.ID] = true
		if before, ok := previous[p.ID]; !ok {
			logging.Infof("provider.added: id=%q", p.ID)
		} else if fields := providerChangedFields(before, p); len(fields) > 0 {
			logging.Infof("provider.updated: id=%q changed_fields=%v", p.ID, fields)
		}
	}
	for _, p := range old.Providers {
		if !current[p.ID] {
			logging.Infof("provider.deleted: id=%q", p.ID)
		}
	}
	if old.ActiveID != m.config.ActiveProviderID {
		logging.Infof("provider.switched: role=chat previous_id=%q id=%q", old.ActiveID, m.config.ActiveProviderID)
	}
	if old.ImageID != m.config.ImageProviderID {
		logging.Infof("provider.switched: role=image previous_id=%q id=%q", old.ImageID, m.config.ImageProviderID)
	}
	m.captureProviderLogState()
	if current := m.providerLogSnapshot; current != nil && current != old {
		var fields []string
		for key, value := range current.Fields {
			if key == "providers" || key == "active_provider_id" || key == "image_provider_id" {
				continue
			}
			if !bytes.Equal(old.Fields[key], value) {
				fields = append(fields, key)
			}
		}
		for key := range old.Fields {
			if _, exists := current.Fields[key]; !exists && key != "providers" && key != "active_provider_id" && key != "image_provider_id" {
				fields = append(fields, key)
			}
		}
		sort.Strings(fields)
		if len(fields) > 0 {
			logging.Infof("config.updated: changed_fields=%v", fields)
		}
	}
}
