package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestProviderChangeLogs(t *testing.T) {
	if os.Getenv("CORNERSTONE_CONFIG_LOG_CHILD") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestProviderChangeLogs$")
		cmd.Env = append(os.Environ(), "CORNERSTONE_CONFIG_LOG_CHILD=1")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("child: %v\n%s", err, output)
		}
		text := string(output)
		for event, count := range map[string]int{"provider.added": 1, "provider.updated": 2, "provider.switched": 2, "provider.deleted": 1} {
			if n := strings.Count(text, event+":"); n != count {
				t.Errorf("%s count=%d want=%d\n%s", event, n, count, text)
			}
		}
		if strings.Contains(text, "PRIVATE_SENTINEL") {
			t.Fatalf("private value leaked: %s", text)
		}
		if !strings.Contains(text, "config.updated: changed_fields=[system_prompt]") {
			t.Fatalf("missing configuration change event: %s", text)
		}
		if !strings.Contains(text, "api_key") {
			t.Fatalf("missing changed field: %s", text)
		}
		return
	}
	dir := t.TempDir()
	m := NewManager(filepath.Join(dir, "config.json"))
	p := DefaultProvider()
	p.ID = "test-provider"
	p.Name = "PRIVATE_SENTINEL_NAME"
	p.APIKey = "PRIVATE_SENTINEL_KEY"
	if err := m.AddProvider(p); err != nil {
		t.Fatal(err)
	}
	if err := m.SetActiveProvider(p.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.SetActiveProvider(p.ID); err != nil {
		t.Fatal(err)
	}
	p.APIKey = "PRIVATE_SENTINEL_NEW_KEY"
	if err := m.UpdateProvider(p); err != nil {
		t.Fatal(err)
	}
	if err := m.UpdatePartial(map[string]string{"api_key": "PRIVATE_SENTINEL_PARTIAL_KEY"}); err != nil {
		t.Fatal(err)
	}
	if err := m.DeleteProvider(p.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.UpdatePartial(map[string]string{"system_prompt": "PRIVATE_SENTINEL_SYSTEM_PROMPT"}); err != nil {
		t.Fatal(err)
	}
	// os.WriteFile fails when the destination is a directory.
	m.configPath = dir
	if err := m.AddProvider(p); err == nil {
		t.Fatal("expected save failure")
	}
}
