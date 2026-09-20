package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestClawBotMigrationAuthority(t *testing.T) {
	dir := t.TempDir()
	m := NewClawBotStateManager(dir)
	a, b, c := ClawBotAccountKey("a", "token"), ClawBotAccountKey("a", "bot-id:1"), ClawBotAccountKey("b", "bot-id:1")
	state := ClawBotState{GetUpdatesBuf: "old", Delivery: &ClawBotDeliveryState{Jobs: []ClawBotDeliveryJob{{ID: "pending", Status: "queued"}}, Receipts: map[string]string{"receipt": "job"}}}
	if err := m.Save(a, state); err != nil {
		t.Fatal(err)
	}
	if err := m.Migrate(a, b); err != nil {
		t.Fatal(err)
	}
	if err := m.Migrate(a, b); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Load(a); err == nil {
		t.Fatal("retired load accepted")
	}
	if err := m.Save(a, state); err == nil {
		t.Fatal("retired save accepted")
	}
	got, err := m.Load(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Delivery.Jobs) != 1 || got.Delivery.Receipts["receipt"] != "job" {
		t.Fatal("lost queue or receipts")
	}
	got.GetUpdatesBuf = "new"
	if err := m.Save(b, got); err != nil {
		t.Fatal(err)
	}
	if err := m.Migrate(b, c); err != nil {
		t.Fatal(err)
	}
	m = NewClawBotStateManager(dir)
	got, err = m.Load(c)
	if err != nil || got.GetUpdatesBuf != "new" {
		t.Fatalf("restart: %+v %v", got, err)
	}
	for _, key := range []string{a, b} {
		if err := m.Save(key, state); err == nil {
			t.Fatal("stale writer accepted")
		}
	}
	if err := m.Migrate(c, b); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Load(c); err == nil {
		t.Fatal("reverse migration left source active")
	}
}

func TestClawBotMigrationFailureClosed(t *testing.T) {
	dir := t.TempDir()
	m := NewClawBotStateManager(dir)
	a, b := ClawBotAccountKey("a", "a"), ClawBotAccountKey("a", "b")
	if err := m.Save(a, ClawBotState{GetUpdatesBuf: "source"}); err != nil {
		t.Fatal(err)
	}
	// Force index replacement failure, after the temporary file has been synced.
	if err := os.Mkdir(filepath.Join(dir, "aliases.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := m.Migrate(a, b); err == nil {
		t.Fatal("migration succeeded")
	}
	if _, err := m.Load(b); err == nil {
		t.Fatal("target activated on failure")
	}
	if err := os.Remove(filepath.Join(dir, "aliases.json")); err != nil {
		t.Fatal(err)
	}
	m = NewClawBotStateManager(dir)
	got, err := m.Load(a)
	if err != nil || got.GetUpdatesBuf != "source" {
		t.Fatal("source lost", err)
	}
	if err := m.Migrate(a, b); err != nil {
		t.Fatal(err)
	}
}

func TestClawBotMigrationConflict(t *testing.T) {
	m := NewClawBotStateManager(t.TempDir())
	a, b := ClawBotAccountKey("a", "a"), ClawBotAccountKey("a", "b")
	for _, key := range []string{a, b} {
		if err := m.Save(key, ClawBotState{GetUpdatesBuf: key}); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.Migrate(a, b); err == nil {
		t.Fatal("overwrote conflicting state")
	}
	for _, key := range []string{a, b} {
		got, err := m.Load(key)
		if err != nil || got.GetUpdatesBuf != key {
			t.Fatal("changed conflicting state")
		}
	}
}

func TestClawBotMigrationWriteFailurePoisonsManager(t *testing.T) {
	dir := t.TempDir()
	m := NewClawBotStateManager(dir)
	a, b := ClawBotAccountKey("a", "a"), ClawBotAccountKey("a", "b")
	if err := m.Save(a, ClawBotState{GetUpdatesBuf: "source"}); err != nil {
		t.Fatal(err)
	}
	// Simulate a failed atomic replacement. Even if the caller repairs the path,
	// this instance must not continue with uncertain durability.
	path := filepath.Join(dir, "blocked")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := m.writeAtomic(path, []byte("{}")); err == nil {
		t.Fatal("expected failure")
	}
	if err := m.Migrate(a, b); err == nil {
		t.Fatal("poisoned migration accepted")
	}
	if err := m.Save(a, ClawBotState{}); err == nil {
		t.Fatal("poisoned save accepted")
	}
	if _, err := m.Load(a); err == nil {
		t.Fatal("poisoned load accepted")
	}
}

func TestClawBotMigrationAuditAndFinalTarget(t *testing.T) {
	dir := t.TempDir()
	m := NewClawBotStateManager(dir)
	a, b, c := ClawBotAccountKey("a", "a"), ClawBotAccountKey("b", "b"), ClawBotAccountKey("c", "c")
	state := ClawBotState{Delivery: &ClawBotDeliveryState{Jobs: []ClawBotDeliveryJob{{ID: "job", Status: "queued"}}, Receipts: map[string]string{"receipt": "job"}}}
	if err := m.Save(a, state); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{a, b}, {b, c}} {
		if err := m.Migrate(pair[0], pair[1]); err != nil {
			t.Fatal(err)
		}
	}
	m = NewClawBotStateManager(dir)
	for _, key := range []string{a, b} {
		target, err := m.MigrationTarget(key)
		if err != nil || target != c {
			t.Fatalf("target %s: %s %v", key, target, err)
		}
		if err := m.Migrate(key, c); err != nil {
			t.Fatal(err)
		}
		audit, err := m.LoadAudit(key)
		if err != nil || len(audit.Delivery.Jobs) != 1 || audit.Delivery.Receipts["receipt"] != "job" {
			t.Fatalf("audit %+v %v", audit, err)
		}
		audit.Delivery.Jobs[0].Status = "done"
		if _, err := m.Load(key); err == nil {
			t.Fatal("retired read")
		}
		if err := m.Save(key, audit); err == nil {
			t.Fatal("retired write")
		}
	}
	got, err := m.Load(c)
	if err != nil || got.Delivery.Jobs[0].Status != "queued" {
		t.Fatal("audit mutated snapshot", err)
	}
	if err := m.Migrate(a, b); err == nil {
		t.Fatal("retired intermediate accepted")
	}
	if _, err := m.LoadAudit(c); err == nil {
		t.Fatal("active audit accepted")
	}
}

func TestClawBotMigrationBrokenChains(t *testing.T) {
	a, b, c := ClawBotAccountKey("a", "a"), ClawBotAccountKey("b", "b"), ClawBotAccountKey("c", "c")
	for name, aliases := range map[string]map[string]clawBotStateAlias{
		"cycle":              {a: {File: a, Retired: true, Successor: b}, b: {File: a, Retired: true, Successor: a}},
		"missing":            {a: {File: a, Retired: true, Successor: b}},
		"invalid":            {a: {File: a, Retired: true, Successor: "../bad"}},
		"different snapshot": {a: {File: a, Retired: true, Successor: b}, b: {File: b}},
		"ambiguous":          {a: {File: a, Retired: true, Successor: b}, b: {File: a}, c: {File: a}},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			data, _ := json.Marshal(aliases)
			if err := os.WriteFile(filepath.Join(dir, "aliases.json"), data, 0600); err != nil {
				t.Fatal(err)
			}
			m := NewClawBotStateManager(dir)
			if _, err := m.MigrationTarget(a); err == nil {
				t.Fatal("invalid chain accepted")
			}
			if _, err := m.LoadAudit(a); err == nil {
				t.Fatal("invalid audit accepted")
			}
			if err := m.Migrate(a, b); err == nil {
				t.Fatal("invalid migration accepted")
			}
		})
	}
}
