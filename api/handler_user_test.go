package api

import (
	"bytes"
	"cornerstone/storage"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func newTestUserManager(t *testing.T) *storage.UserManager {
	t.Helper()

	um := storage.NewUserManager(filepath.Join(t.TempDir(), "user"))
	if _, err := um.Update("Tester", "old bio"); err != nil {
		t.Fatalf("Update user info failed: %v", err)
	}
	return um
}

func TestHandleUser_CanClearDescriptionExplicitly(t *testing.T) {
	handler := &Handler{userManager: newTestUserManager(t)}

	req := httptest.NewRequest(http.MethodPut, "/management/user", bytes.NewBufferString(`{"description":""}`))
	rec := httptest.NewRecorder()

	handler.handleUser(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	updated := handler.userManager.Get()
	if updated.Description != "" {
		t.Fatalf("description = %q, want empty string", updated.Description)
	}
	if updated.Username != "Tester" {
		t.Fatalf("username = %q, want %q", updated.Username, "Tester")
	}
}

func TestHandleUser_PreservesDescriptionWhenOmitted(t *testing.T) {
	handler := &Handler{userManager: newTestUserManager(t)}

	req := httptest.NewRequest(http.MethodPut, "/management/user", bytes.NewBufferString(`{"username":"Tester Updated"}`))
	rec := httptest.NewRecorder()

	handler.handleUser(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	updated := handler.userManager.Get()
	if updated.Description != "old bio" {
		t.Fatalf("description = %q, want %q", updated.Description, "old bio")
	}
	if updated.Username != "Tester Updated" {
		t.Fatalf("username = %q, want %q", updated.Username, "Tester Updated")
	}
}

func newTestAuthHandler(t *testing.T) *Handler {
	t.Helper()

	baseDir := filepath.Join(t.TempDir(), "user_about")
	return &Handler{
		authManager: storage.NewAuthManager(baseDir),
		tokenStore:  newAuthTokenStore(),
		userManager: storage.NewUserManager(baseDir),
	}
}

func TestHandleAuthSetup_RejectsRemoteBootstrap(t *testing.T) {
	handler := newTestAuthHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/management/auth/setup", bytes.NewBufferString(`{"username":"owner","password":"secret"}`))
	req.RemoteAddr = "203.0.113.10:42318"
	req.Host = "198.51.100.8:1205"
	rec := httptest.NewRecorder()

	handler.handleAuthSetup(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if handler.authManager.IsSetup() {
		t.Fatal("auth should remain unset after remote bootstrap attempt")
	}
}

func TestHandleAuthSetup_AllowsLoopbackBootstrap(t *testing.T) {
	handler := newTestAuthHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/management/auth/setup", bytes.NewBufferString(`{"username":"owner","password":"secret"}`))
	req.RemoteAddr = "127.0.0.1:42318"
	req.Host = "127.0.0.1:1205"
	req.Header.Set("Origin", "http://127.0.0.1:1205")
	rec := httptest.NewRecorder()

	handler.handleAuthSetup(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if !handler.authManager.IsSetup() {
		t.Fatal("auth should be initialized after loopback bootstrap")
	}
	if got := handler.userManager.Get().Username; got != "owner" {
		t.Fatalf("username = %q, want %q", got, "owner")
	}
}

func TestHandleAuthSetup_RejectsCrossOriginLoopbackBootstrap(t *testing.T) {
	handler := newTestAuthHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/management/auth/setup", bytes.NewBufferString(`{"username":"owner","password":"secret"}`))
	req.RemoteAddr = "127.0.0.1:42318"
	req.Host = "127.0.0.1:1205"
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()

	handler.handleAuthSetup(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if handler.authManager.IsSetup() {
		t.Fatal("auth should remain unset after cross-origin bootstrap attempt")
	}
}
