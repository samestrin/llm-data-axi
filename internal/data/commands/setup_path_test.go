package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Defends the hook command against a different llm-data on PATH: setup wrote
// the bare `llm-data` whenever PATH had any llm-data, so a hook installed by
// one build ran another (an old release, or someone else's binary). The bare
// name is used only when PATH resolves to this binary.
func TestHookCommandIsBareOnlyWhenPATHFindsThisBinary(t *testing.T) {
	dir := evalTemp(t)
	real := filepath.Join(dir, "real", "llm-data")
	other := filepath.Join(dir, "other", "llm-data")
	link := filepath.Join(dir, "bin", "llm-data")
	for _, p := range []string{real, other} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}

	for _, c := range []struct {
		name, onPath, want string
	}{
		{"another binary on PATH", other, real},
		{"this binary through a symlink on PATH", link, setupHookCommand},
		{"this binary on PATH", real, setupHookCommand},
	} {
		t.Run(c.name, func(t *testing.T) {
			oldLook, oldExe := setupLookPath, setupExecutable
			t.Cleanup(func() { setupLookPath, setupExecutable = oldLook, oldExe })
			setupLookPath = func(string) (string, error) { return c.onPath, nil }
			setupExecutable = func() (string, error) { return real, nil }
			got, err := resolveHookCommand()
			if err != nil {
				t.Fatal(err)
			}
			if got != shellQuote(c.want) {
				t.Errorf("hook command = %q, want %q", got, shellQuote(c.want))
			}
		})
	}

	// PATH has an llm-data but this binary's own path is unknown: the bare
	// name cannot be shown to be this binary, so setup refuses.
	oldLook, oldExe := setupLookPath, setupExecutable
	t.Cleanup(func() { setupLookPath, setupExecutable = oldLook, oldExe })
	setupLookPath = func(string) (string, error) { return other, nil }
	setupExecutable = func() (string, error) { return "", fmt.Errorf("unknown") }
	if got, err := resolveHookCommand(); err == nil {
		t.Errorf("hook command %q with this binary's path unknown, want an error", got)
	}
}
