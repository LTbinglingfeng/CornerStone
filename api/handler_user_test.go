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
