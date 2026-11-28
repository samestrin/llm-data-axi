package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestStatsCommand(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test structure
	os.Mkdir(filepath.Join(tmpDir, "subdir"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("content"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "file2.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "subdir", "nested.txt"), []byte("nested"), 0644)

	tests := []struct {
		name     string
		args     []string
		expected []string
		hasError bool
	}{
		{
			name:     "directory stats",
			args:     []string{"--path", tmpDir, "--no-gitignore"},
			expected: []string{"FILES:", "DIRECTORIES:", "TOTAL_SIZE:"},
		},
		{
			name:     "non-existent path",
			args:     []string{"--path", "/nonexistent/path"},
			hasError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newStatsCmd()
			buf := new(bytes.Buffer)
			errBuf := new(bytes.Buffer)
			cmd.SetOut(buf)
			cmd.SetErr(errBuf)
			cmd.SetArgs(tt.args)

			err := cmd.Execute()

			if tt.hasError {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			output := buf.String()
			for _, exp := range tt.expected {
				if !strings.Contains(output, exp) {
					t.Errorf("output %q should contain %q", output, exp)
				}
			}
		})
	}
}

// The expected next steps are spelled out here rather than read from the
// production variables, so a change to what an agent is told to do next is a
// deliberate edit in two places and not a silent one in one.
const (
	helpStatsGitignore = "Add --no-gitignore to include ignored and hidden files"
	helpStatsParent    = "Run `llm-data stats --path <parent>` to check the parent directory"
)

// Defends epic 22 T4: stats on an empty directory carried bare zeros and
// left an agent to guess why nothing counted. The empty result must state
// the exact next steps in TOON and --json, including the separate --min
// shape (statsMinResult), and --plain must stay help-free. Fails when the
// emptyHelp call is removed from either shape.
func TestStatsEmptyDirCarriesNextSteps(t *testing.T) {
	want := []string{helpStatsGitignore, helpStatsParent}
	for _, hiddenOnly := range []bool{false, true} {
		dir := t.TempDir()
		if hiddenOnly {
			if err := os.WriteFile(filepath.Join(dir, ".hidden"), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		for _, min := range []bool{false, true} {
			args := []string{"stats", "--path", dir, "--json"}
			if min {
				args = append(args, "--min")
			}
			code, stdout, _ := runCLI(t, args...)
			if code != 0 {
				t.Fatalf("exit %d\n%s", code, stdout)
			}
			help, ok := jsonHelp(t, stdout)
			if !ok || !reflect.DeepEqual(help, want) {
				t.Errorf("--json (min %v, hiddenOnly %v) help = %q (present %v), want %q", min, hiddenOnly, help, ok, want)
			}
		}
		code, toon, _ := runCLI(t, "stats", "--path", dir)
		if code != 0 {
			t.Fatalf("exit %d\n%s", code, toon)
		}
		assertAXIHelp(t, toon, want)

		code, plain, _ := runCLI(t, "stats", "--path", dir, "--plain")
		if code != 0 {
			t.Fatalf("exit %d\n%s", code, plain)
		}
		if hasHelpKey(plain) {
			t.Errorf("--plain carried a help key:\n%s", plain)
		}
	}
}

// Defends the grep precedent on stats: --no-gitignore already passed is not
// offered again, and one unconditional step remains so an empty result never
// has nothing to say (emptyHelp panics on zero steps).
func TestStatsEmptyHelpDropsNoGitignoreAlreadyPassed(t *testing.T) {
	dir := t.TempDir()
	want := []string{helpStatsParent}
	code, stdout, _ := runCLI(t, "stats", "--path", dir, "--no-gitignore", "--json")
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stdout)
	}
	help, ok := jsonHelp(t, stdout)
	if !ok || !reflect.DeepEqual(help, want) {
		t.Errorf("help = %q (present %v), want %q", help, ok, want)
	}
}

// Defends the other half of epic 22's rule: a non-empty stats result carries
// no help key at all, in the full shape or the --min one. A directory holding
// only an empty subdirectory counts as non-empty: it reports directories 1.
func TestStatsNonEmptyCarriesNoHelp(t *testing.T) {
	withFile := t.TempDir()
	if err := os.WriteFile(filepath.Join(withFile, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	onlySubdir := t.TempDir()
	if err := os.Mkdir(filepath.Join(onlySubdir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{withFile, onlySubdir} {
		for _, min := range []bool{false, true} {
			args := []string{"stats", "--path", dir, "--json"}
			if min {
				args = append(args, "--min")
			}
			code, stdout, _ := runCLI(t, args...)
			if code != 0 {
				t.Fatalf("exit %d\n%s", code, stdout)
			}
			if help, ok := jsonHelp(t, stdout); ok {
				t.Errorf("--json (min %v) non-empty stats carried help %q", min, help)
			}
		}
		code, toon, _ := runCLI(t, "stats", "--path", dir)
		if code != 0 {
			t.Fatalf("exit %d\n%s", code, toon)
		}
		if hasHelpKey(toon) {
			t.Errorf("TOON non-empty stats carried help:\n%s", toon)
		}
	}
}
