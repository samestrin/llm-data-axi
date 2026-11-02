package commands

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGrepCommand(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files
	file1 := filepath.Join(tmpDir, "file1.txt")
	file2 := filepath.Join(tmpDir, "file2.txt")
	os.WriteFile(file1, []byte("hello world\nfoo bar"), 0644)
	os.WriteFile(file2, []byte("another line\nhello again"), 0644)

	tests := []struct {
		name     string
		args     []string
		expected []string
		hasError bool
	}{
		{
			name:     "basic grep",
			args:     []string{"hello", file1},
			expected: []string{"hello world"},
		},
		{
			name:     "grep with line numbers",
			args:     []string{"hello", file1, "-n"},
			expected: []string{"1:"},
		},
		{
			name:     "case insensitive",
			args:     []string{"HELLO", file1, "-i"},
			expected: []string{"hello world"},
		},
		{
			name:     "files only mode",
			args:     []string{"hello", tmpDir, "-l"},
			expected: []string{"file1.txt", "file2.txt"},
		},
		{
			name:     "no matches",
			args:     []string{"nonexistent", file1},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newGrepCmd()
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

// TestGrepJSONOutput tests JSON output mode
func TestGrepJSONOutput(t *testing.T) {
	tmpDir := t.TempDir()
	file := filepath.Join(tmpDir, "test.txt")
	os.WriteFile(file, []byte("hello world\nfoo bar"), 0644)

	cmd := newGrepCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"hello", file, "--json"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	// JSON output should have proper structure
	if !strings.Contains(output, "{") {
		t.Errorf("JSON output should contain JSON structure, got: %s", output)
	}
}

// TestGrepMinimalOutput tests minimal output mode
func TestGrepMinimalOutput(t *testing.T) {
	tmpDir := t.TempDir()
	file := filepath.Join(tmpDir, "test.txt")
	os.WriteFile(file, []byte("hello world\nfoo bar"), 0644)

	cmd := newGrepCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"hello", file, "--min"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	// Minimal output should contain the match
	if !strings.Contains(output, "hello") {
		t.Errorf("minimal output should contain 'hello', got: %s", output)
	}
}

// TestGrepDirectory tests grepping in a directory
func TestGrepDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "subdir")
	os.MkdirAll(subDir, 0755)

	os.WriteFile(filepath.Join(tmpDir, "root.txt"), []byte("root hello"), 0644)
	os.WriteFile(filepath.Join(subDir, "sub.txt"), []byte("sub hello"), 0644)

	cmd := newGrepCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"hello", tmpDir})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	// Should find matches in both files
	if !strings.Contains(output, "root") || !strings.Contains(output, "sub") {
		t.Errorf("should find matches in both files, got: %s", output)
	}
}

// TestGrepInvalidPattern tests error handling for invalid regex
func TestGrepInvalidPattern(t *testing.T) {
	tmpDir := t.TempDir()
	file := filepath.Join(tmpDir, "test.txt")
	os.WriteFile(file, []byte("hello world"), 0644)

	cmd := newGrepCmd()
	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"[invalid", file})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for invalid regex pattern")
	}
}

// grepCLIArgsEnv carries the CLI arguments to TestGrepCLIHelperProcess, one per
// line. When it is unset the helper does nothing.
const grepCLIArgsEnv = "LLM_DATA_GREP_CLI_ARGS"

// TestGrepCLIHelperProcess is not a test on its own. runGrepCLI re-executes the
// test binary into it so that Execute runs exactly as the shipped binary does,
// including os.Exit, and the process status can be observed.
func TestGrepCLIHelperProcess(t *testing.T) {
	raw, ok := os.LookupEnv(grepCLIArgsEnv)
	if !ok {
		return
	}
	os.Args = append([]string{"llm-data"}, strings.Split(raw, "\n")...)
	Execute()
	os.Exit(0)
}

// runGrepCLI runs llm-data with args in a child process and returns its stdout
// and exit code.
func runGrepCLI(t *testing.T, args ...string) (string, int) {
	t.Helper()
	c := exec.Command(os.Args[0], "-test.run=^TestGrepCLIHelperProcess$")
	c.Env = append(os.Environ(), grepCLIArgsEnv+"="+strings.Join(args, "\n"))
	stdout := new(bytes.Buffer)
	c.Stdout = stdout
	c.Stderr = new(bytes.Buffer)
	err := c.Run()
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		t.Fatalf("run %v: %v", args, err)
	}
	return stdout.String(), c.ProcessState.ExitCode()
}

// TestGrepMissingPathFailsInEveryMode defends against grep skipping a path that
// does not exist. It used to `continue` past the failed os.Stat, so
// `grep foo /nonexistent --axi` printed `pattern: foo` and exited 0: a wrong
// path read exactly like a search that found nothing.
func TestGrepMissingPathFailsInEveryMode(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")

	modes := []struct {
		name string
		flag []string
	}{
		{"plain", nil},
		{"json", []string{"--json"}},
		{"axi", []string{"--axi"}},
	}
	for _, m := range modes {
		t.Run(m.name, func(t *testing.T) {
			args := append([]string{"grep", "foo", missing}, m.flag...)
			out, code := runGrepCLI(t, args...)
			if code == 0 {
				t.Errorf("exit 0 for a missing path; output:\n%s", out)
			}
			if !strings.Contains(out, "path not found") || !strings.Contains(out, missing) {
				t.Errorf("output does not name the missing path:\n%s", out)
			}
			if strings.Contains(out, "pattern: foo") || strings.Contains(out, `"pattern": "foo"`) {
				t.Errorf("printed a result as if the search ran:\n%s", out)
			}
		})
	}
}

// TestGrepMissingPathAmongRealOnesFails defends against a missing path being
// hidden by a real one beside it: matches from the real file must not make the
// run look complete when one of the paths it was asked to search was never read.
func TestGrepMissingPathAmongRealOnesFails(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.txt")
	if err := os.WriteFile(real, []byte("foo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newGrepCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"foo", real, filepath.Join(dir, "gone")})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "path not found") {
		t.Fatalf("err = %v, want path not found", err)
	}
}

// TestGrepUnreadablePathFails defends the other os.Stat failure: a path that
// exists in name but cannot be statted (here, a regular file used as a
// directory) must fail too, not be skipped.
func TestGrepUnreadablePathFails(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(file, []byte("foo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := newGrepCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"foo", filepath.Join(file, "child")})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected an error for a path under a regular file")
	}
}
