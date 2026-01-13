package script

import (
	"testing"

	"github.com/rw-r-r-0644/ctf-sync/jeopardy"
)

func TestScriptBackendRegistered(t *testing.T) {
	backends := jeopardy.Backends()
	for _, b := range backends {
		if b.ID == "script" {
			return
		}
	}
	t.Error("script backend not registered")
}

func TestBuildScript(t *testing.T) {
	backend, err := jeopardy.Build("script", map[string]string{
		"command": "python3 sync.py",
	})
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if backend == nil {
		t.Fatal("backend is nil")
	}
}
