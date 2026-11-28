package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// Defends the go run check against refusing directories that merely start
// with "go-build": a prefix match refused a binary under $TMP/go-builders,
// which `go run` never creates (os.MkdirTemp(..., "go-build") always appends
// a decimal number). Only a first path element of go-build<digits> directly
// beneath the temp dir is a go run build.
func TestIsGoRunBuildRequiresANumberedGoBuildDir(t *testing.T) {
	tmp := os.TempDir()
	for _, c := range []struct {
		rel  string
		want bool
	}{
		{filepath.Join("go-build123", "x", "llm-data"), true},
		{filepath.Join("go-build123456789", "b001", "exe", "llm-data"), true},
		{filepath.Join("go-builders", "tools", "llm-data"), false},
		{filepath.Join("go-build", "llm-data"), false},
		{filepath.Join("go-build12x", "exe", "llm-data"), false},
		{filepath.Join("tools", "llm-data"), false},
		{filepath.Join("nested", "go-build123", "llm-data"), false},
	} {
		t.Run(c.rel, func(t *testing.T) {
			if got := isGoRunBuild(filepath.Join(tmp, c.rel)); got != c.want {
				t.Errorf("isGoRunBuild(%s) = %v, want %v", filepath.Join(tmp, c.rel), got, c.want)
			}
		})
	}
}

// Defends both sides of the narrowed go run refusal at the setup level: a
// binary under $TMP/go-builders (or any non-go-build sibling) registers its
// absolute path as the hook, while a real go run build under
// $TMP/go-build123 still fails and writes nothing.
func TestSetupRefusesOnlyNumberedGoBuildDirs(t *testing.T) {
	tmp := os.TempDir()
	for _, c := range []struct {
		exe     string
		refused bool
	}{
		{filepath.Join(tmp, "go-builders", "tools", "llm-data"), false},
		{filepath.Join(tmp, "tools", "llm-data"), false},
		{filepath.Join(tmp, "go-build123", "x", "llm-data"), true},
	} {
		t.Run(c.exe, func(t *testing.T) {
			project, _ := setupEnv(t)
			stubBinary(t, false, c.exe)
			out, err := runSetupCmd(t, "--path", project)
			if c.refused {
				if err == nil {
					t.Fatalf("setup succeeded registering a go run build:\n%s", out)
				}
				if !strings.Contains(err.Error(), "go run") {
					t.Errorf("error does not say why: %v", err)
				}
				assertEmptyDir(t, project)
				return
			}
			if err != nil {
				t.Fatalf("setup refused %s: %v", c.exe, err)
			}
			if cmds := sessionStartCommands(t, readFile(t, settingsPath(project))); len(cmds) != 1 || cmds[0] != shellQuote(c.exe) {
				t.Errorf("SessionStart commands = %v, want [%s]", cmds, shellQuote(c.exe))
			}
		})
	}
}
