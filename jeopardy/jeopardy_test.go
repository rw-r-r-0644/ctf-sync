package jeopardy

import (
	"testing"
)

func TestBackendsRegistered(t *testing.T) {
	backends := Backends()

	expected := map[string]bool{
		"ctfd_token":  false,
		"ctfd_cookie": false,
		"rctf":        false,
	}

	for _, b := range backends {
		if _, ok := expected[b.ID]; ok {
			expected[b.ID] = true
		}
	}

	for id, found := range expected {
		if !found {
			t.Errorf("backend %q not registered", id)
		}
	}
}

func TestBuildCTFdToken(t *testing.T) {
	backend, err := Build("ctfd_token", map[string]string{
		"base_url": "https://ctf.example.com",
		"token":    "test-token",
	})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if backend == nil {
		t.Fatal("backend is nil")
	}
}

func TestBuildCTFdCookie(t *testing.T) {
	backend, err := Build("ctfd_cookie", map[string]string{
		"base_url": "https://ctf.example.com",
		"cookie":   "session=abc123",
	})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if backend == nil {
		t.Fatal("backend is nil")
	}
}

func TestBuildRCTF(t *testing.T) {
	backend, err := Build("rctf", map[string]string{
		"base_url":   "https://rctf.example.com",
		"team_token": "test-team-token",
	})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if backend == nil {
		t.Fatal("backend is nil")
	}
}

func TestBuildMissingRequired(t *testing.T) {
	_, err := Build("ctfd_token", map[string]string{
		"base_url": "https://ctf.example.com",
	})
	if err == nil {
		t.Fatal("expected error for missing required setting")
	}
}

func TestBuildUnknownBackend(t *testing.T) {
	_, err := Build("unknown", map[string]string{})
	if err == nil {
		t.Fatal("expected error for unknown backend")
	}
}
