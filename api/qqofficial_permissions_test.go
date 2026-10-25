package api

import (
	"context"
	"cornerstone/client"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestQQOfficialSettingsCommandPermissions(t *testing.T) {
	s, _, _ := newQQOfficialTest(t)
	for _, tc := range []struct {
		permissions         string
		wantNew, wantPrompt bool
	}{
		{`,"command_permissions":{"new":false,"prompt":false,"menu":false,"unknown":true}`, false, false},
		{``, false, false},
		{`,"command_permissions":{"new":true,"prompt":false}`, true, false},
		{`,"command_permissions":{}`, true, true},
	} {
		w := httptest.NewRecorder()
		s.handler.handleQQOfficialSettings(w, httptest.NewRequest("PUT", "/api/settings/qq-official", strings.NewReader(`{"enabled":false,"app_id":"123","source_filter_mode":"all"`+tc.permissions+`}`)))
		if w.Code != 200 {
			t.Fatalf("update: %d %s", w.Code, w.Body)
		}
		var response struct {
			Data struct {
				Permissions map[string]bool `json:"command_permissions"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		got := response.Data.Permissions
		if len(got) != 7 || got["new"] != tc.wantNew || got["prompt"] != tc.wantPrompt {
			t.Fatalf("permissions: %v", got)
		}
		if !reflect.DeepEqual(got, s.handler.configManager.GetQQOfficialConfig().CommandPermissions) {
			t.Fatal("response differs from stored permissions")
		}
	}
}

func TestQQOfficialBindingPreservesCommandPermissions(t *testing.T) {
	s, _, _ := newQQOfficialTest(t)
	c := s.handler.configManager.GetQQOfficialConfig()
	c.CommandPermissions["delete"] = false
	if err := s.handler.configManager.UpdateQQOfficialConfig(c); err != nil {
		t.Fatal(err)
	}
	s.startBinding = func(context.Context) (*client.QQOfficialBinding, error) {
		return &client.QQOfficialBinding{ExpiresAt: time.Now().Add(time.Minute)}, nil
	}
	s.pollBinding = func(context.Context, *client.QQOfficialBinding) (*client.QQOfficialBindResult, error) {
		return &client.QQOfficialBindResult{Status: "created", AppID: "456", AppSecret: "new-secret"}, nil
	}
	login, err := s.startLogin()
	if err != nil {
		t.Fatal(err)
	}
	if result, err := s.loginStatus(login["id"].(string), false); err != nil || result["status"] != "created" {
		t.Fatalf("binding: %v %v", result, err)
	}
	if !reflect.DeepEqual(c.CommandPermissions, s.handler.configManager.GetQQOfficialConfig().CommandPermissions) {
		t.Fatal("binding changed permissions")
	}
}
