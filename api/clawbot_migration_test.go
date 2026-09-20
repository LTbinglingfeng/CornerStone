package api

import (
	"cornerstone/config"
	"cornerstone/storage"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestClawBotMigrationIdentityGate(t *testing.T) {
	for _, oldID := range []string{"", "old-bot"} {
		for _, nextID := range []string{"", "different-bot"} {
			t.Run(oldID+"/"+nextID, func(t *testing.T) {
				old := config.ClawBotConfig{BaseURL: "https://example.test", BotToken: "old", ILinkBotID: oldID}
				next := old
				next.BotToken = "new"
				next.ILinkBotID = nextID
				s := &ClawBotService{state: storage.ClawBotState{Delivery: &storage.ClawBotDeliveryState{Jobs: []storage.ClawBotDeliveryJob{{Status: "queued"}}}}}
				if err := s.prepareClawBotMigration(old, next); err == nil {
					t.Fatal("unverified identity accepted pending queue")
				}
				next.BotToken = ""
				next.ILinkBotID = ""
				if err := s.prepareClawBotMigration(old, next); err == nil {
					t.Fatal("clear hid pending queue")
				}
				s.state.Delivery.Jobs[0].Status = "done"
				if err := s.prepareClawBotMigration(old, next); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestClawBotMigrationQRUsesOldCredentialAndRegion(t *testing.T) {
	m := storage.NewClawBotStateManager(t.TempDir())
	old := config.ClawBotConfig{BaseURL: "https://old.example.test", BotToken: "old-token", ILinkBotID: "same-bot"}
	next := old
	next.BaseURL = "https://new.example.test"
	next.BotToken = "new-token"
	state := storage.ClawBotState{Delivery: &storage.ClawBotDeliveryState{Jobs: []storage.ClawBotDeliveryJob{{ID: "old-job", Status: "queued"}}, Receipts: map[string]string{"receipt": "old-job"}}}
	if err := m.Save(clawBotAccount(old), state); err != nil {
		t.Fatal(err)
	}
	s := &ClawBotService{stateManager: m, state: state}
	if err := s.prepareClawBotMigration(old, next); err != nil {
		t.Fatal(err)
	}
	got, err := m.Load(clawBotAccount(next))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Delivery.Jobs) != 1 || got.Delivery.Receipts["receipt"] != "old-job" {
		t.Fatal("rotation lost old queue")
	}
	if _, err := m.Load(clawBotAccount(old)); err == nil {
		t.Fatal("old region still executable")
	}
}

func TestClawBotMigrationRestartWithOldDiskConfig(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	cm := config.NewManager(cfgPath)
	old := config.ClawBotConfig{BaseURL: "https://example.test", BotToken: "old", Enabled: true}
	if err := cm.UpdateClawBotConfig(old); err != nil {
		t.Fatal(err)
	}
	next := old
	next.ILinkBotID = "verified-bot"
	next.Enabled = false
	dir := t.TempDir()
	m := storage.NewClawBotStateManager(dir)
	state := storage.ClawBotState{Delivery: &storage.ClawBotDeliveryState{Jobs: []storage.ClawBotDeliveryJob{{ID: "job", Status: "queued"}}, Receipts: map[string]string{"receipt": "job"}}}
	if err := m.Save(clawBotAccount(old), state); err != nil {
		t.Fatal(err)
	}
	prior := &ClawBotService{stateManager: m, state: state}
	if err := prior.prepareClawBotMigration(old, next); err != nil {
		t.Fatal(err)
	}
	// Crash before saving next: reconstruct BOTH managers and the service from disk.
	cm = config.NewManager(cfgPath)
	if got := cm.GetClawBotConfig(); clawBotAccount(got) != clawBotAccount(old) {
		t.Fatal("fixture did not retain old config")
	}
	s := newTestClawBotService(t, old.BaseURL)
	s.handler.configManager = cm
	s.stateManager = storage.NewClawBotStateManager(dir)
	s.handler.clawBotService = s
	s.ApplyCurrentConfig()
	if s.workerRunning || s.connected {
		t.Fatal("retired credential started worker")
	}
	if s.stateAccount != clawBotAccount(old) {
		t.Fatal("missing audit account")
	}
	w := httptest.NewRecorder()
	s.handler.handleClawBotDeliveries(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"id":"job"`) {
		t.Fatalf("queue invisible: %s", w.Body.String())
	}
	if err := s.actOnDelivery(old, clawBotDeliveryAction{ID: "job", AccountID: clawBotAccount(old), Action: "cancel", OperationID: "cancel-op"}); err == nil {
		t.Fatal("audit queue executable")
	}
	wrong := next
	wrong.ILinkBotID = "unrelated-bot"
	if err := s.replaceClawBotConfigLocked(wrong); err == nil {
		t.Fatal("unverified target accepted")
	}
	if len(s.state.Delivery.Jobs) != 1 || s.state.Delivery.Jobs[0].Status != "queued" {
		t.Fatal("failed switch lost audit")
	}
	if got := s.reusableQRCodeToken(old.BaseURL); got != "" {
		t.Fatal("retired local QR token reused")
	}
	if err := s.replaceClawBotConfigLocked(next); err != nil {
		t.Fatal(err)
	}
	s.ApplyCurrentConfig()
	if s.stateAccount != clawBotAccount(next) {
		t.Fatal("successor not restored")
	}
	if len(s.state.Delivery.Jobs) != 1 || s.state.Delivery.Receipts["receipt"] != "job" {
		t.Fatal("queue duplicated or receipts lost")
	}
	if err := s.restoreChannelState(next); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{clawBotAccount(old)} {
		if _, err := s.stateManager.Load(key); err == nil {
			t.Fatal("old Load accepted")
		}
		if err := s.stateManager.Save(key, state); err == nil {
			t.Fatal("old Save accepted")
		}
	}
	// Rotate region again, then repeat recovery from the original old disk config.
	final := next
	final.BaseURL = "https://region.example.test"
	final.BotToken = "rotated"
	if err := s.prepareClawBotMigration(next, final); err != nil {
		t.Fatal(err)
	}
	if err := cm.UpdateClawBotConfig(old); err != nil {
		t.Fatal(err)
	}
	s.handler.configManager = config.NewManager(cfgPath)
	s.stateManager = storage.NewClawBotStateManager(dir)
	if err := s.replaceClawBotConfigLocked(final); err != nil {
		t.Fatal(err)
	}
	s.ApplyCurrentConfig()
	if len(s.state.Delivery.Jobs) != 1 || s.state.Delivery.Receipts["receipt"] != "job" {
		t.Fatal("chain recovery lost queue")
	}
}
