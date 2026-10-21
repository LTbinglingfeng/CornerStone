package api

import (
	"context"
	"cornerstone/client"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestQQOfficialSettingsRevisionAfterBinding(t *testing.T) {
	s, _, _ := newQQOfficialTest(t)
	s.startBinding = func(context.Context) (*client.QQOfficialBinding, error) {
		return &client.QQOfficialBinding{ExpiresAt: time.Now().Add(time.Minute)}, nil
	}
	s.pollBinding = func(context.Context, *client.QQOfficialBinding) (*client.QQOfficialBindResult, error) {
		return &client.QQOfficialBindResult{Status: "created", AppID: "456", AppSecret: "new-secret"}, nil
	}
	s.mu.Lock()
	old := s.settingsLocked()
	s.mu.Unlock()
	login, err := s.startLogin()
	if err != nil {
		t.Fatal(err)
	}
	if result, err := s.loginStatus(login["id"].(string), false); err != nil || result["status"] != "created" {
		t.Fatalf("completion: %v %v", result, err)
	}
	if result, err := s.loginStatus(login["id"].(string), true); err != nil || result["status"] != "created" {
		t.Fatalf("late cancel: %v %v", result, err)
	}
	body, _ := json.Marshal(old)
	w := httptest.NewRecorder()
	s.handler.handleQQOfficialSettings(w, httptest.NewRequest("PUT", "/api/settings/qq-official", strings.NewReader(string(body))))
	if w.Code != 409 {
		t.Fatalf("stale update: %d %s", w.Code, w.Body)
	}
	c := s.handler.configManager.GetQQOfficialConfig()
	if c.AppID != "456" || c.AppSecret != "new-secret" {
		t.Fatal("stale form overwrote binding")
	}
}

func TestQQOfficialSecretNeverMovesToDifferentAppID(t *testing.T) {
	for _, appID := range []string{"123", "456"} {
		t.Run(appID, func(t *testing.T) {
			s, _, _ := newQQOfficialTest(t)
			w := httptest.NewRecorder()
			s.handler.handleQQOfficialSettings(w, httptest.NewRequest("PUT", "/api/settings/qq-official", strings.NewReader(`{"enabled":false,"app_id":"`+appID+`","source_filter_mode":"all"}`)))
			if w.Code != 200 {
				t.Fatalf("update: %s", w.Body)
			}
			c := s.handler.configManager.GetQQOfficialConfig()
			if appID == "123" && c.AppSecret != "test-secret" {
				t.Fatal("same-account secret not retained")
			}
			if appID != "123" && c.AppSecret != "" {
				t.Fatal("secret transferred to another AppID")
			}
		})
	}
}

func TestQQOfficialSettingsRevisionRejectsOtherTab(t *testing.T) {
	s, _, _ := newQQOfficialTest(t)
	body := `{"revision":"1","enabled":false,"app_id":"123","source_filter_mode":"all","app_secret":"rotated"}`
	for _, want := range []int{200, 409} {
		w := httptest.NewRecorder()
		s.handler.handleQQOfficialSettings(w, httptest.NewRequest("PUT", "/api/settings/qq-official", strings.NewReader(body)))
		if w.Code != want {
			t.Fatalf("got %d want %d: %s", w.Code, want, w.Body)
		}
	}
}
