package commands

import (
	"bytes"
	"encoding/json"
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

// runGrepCLI runs llm-data with args in a child process and returns its stdout,
// its stderr and its exit code.
func runGrepCLI(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	c := exec.Command(os.Args[0], "-test.run=^TestGrepCLIHelperProcess$")
	c.Env = append(os.Environ(), grepCLIArgsEnv+"="+strings.Join(args, "\n"))
	stdout := new(bytes.Buffer)
	c.Stdout = stdout
	stderr := new(bytes.Buffer)
	c.Stderr = stderr
	err := c.Run()
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		t.Fatalf("run %v: %v", args, err)
	}
	return stdout.String(), stderr.String(), c.ProcessState.ExitCode()
}

// TestGrepMissingPathFailsInEveryMode defends against grep skipping a path that
// does not exist. It used to `continue` past the failed os.Stat, so
// `grep foo /nonexistent --axi` printed `pattern: foo` and exited 0: a wrong
// path read exactly like a search that found nothing.
func TestGrepMissingPathFailsInEveryMode(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")

	// Plain mode writes the error to stderr; the structured modes keep it on
	// stdout so a consumer reads one stream.
	modes := []struct {
		name     string
		flag     []string
		onStderr bool
	}{
		{"plain", nil, true},
		{"json", []string{"--json"}, false},
		{"axi", []string{"--axi"}, false},
	}
	for _, m := range modes {
		t.Run(m.name, func(t *testing.T) {
			args := append([]string{"grep", "foo", missing}, m.flag...)
			stdout, stderr, code := runGrepCLI(t, args...)
			if code == 0 {
				t.Errorf("exit 0 for a missing path; stdout:\n%s", stdout)
			}
			msg := stdout
			if m.onStderr {
				msg = stderr
			}
			if !strings.Contains(msg, "path not found") || !strings.Contains(msg, missing) {
				t.Errorf("error does not name the missing path:\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
			}
			if strings.Contains(stdout, "pattern: foo") || strings.Contains(stdout, `"pattern": "foo"`) {
				t.Errorf("printed a result as if the search ran:\n%s", stdout)
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

// TestGrepTruncatesLongLinesWithHint defends AXI principle 3 for grep: a
// matching line used to be emitted whole in every mode, so one long line could
// cost hundreds of tokens with no way to tell it was partial. The default now
// cuts the line at a limit and says so (a truncated flag, the full length, a
// next step); --full opts out.
func TestGrepTruncatesLongLinesWithHint(t *testing.T) {
	dir := t.TempDir()
	longLine := "needle" + strings.Repeat("x", 400)
	file := filepath.Join(dir, "long.txt")
	if err := os.WriteFile(file, []byte(longLine+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, mode := range []string{"--json", "--axi"} {
		t.Run(mode, func(t *testing.T) {
			stdout, _, code := runGrepCLI(t, "grep", "needle", file, mode)
			if code != 0 {
				t.Fatalf("exit %d; stdout:\n%s", code, stdout)
			}
			if !strings.Contains(stdout, "truncated") {
				t.Errorf("a cut line did not say it was truncated:\n%s", stdout)
			}
			if !strings.Contains(stdout, "full_length") {
				t.Errorf("no size hint for the cut line:\n%s", stdout)
			}
			if !strings.Contains(stdout, "full") || !strings.Contains(stdout, "--full") {
				t.Errorf("no --full next step offered:\n%s", stdout)
			}
			if strings.Count(stdout, "x") > 220 {
				t.Errorf("the line was emitted nearly whole (%d x's):\n%s",
					strings.Count(stdout, "x"), truncForMsg(stdout))
			}
		})
	}

	t.Run("plain", func(t *testing.T) {
		stdout, _, code := runGrepCLI(t, "grep", "needle", file)
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
		if !strings.Contains(stdout, "...") {
			t.Errorf("a cut plain line has no ellipsis marker:\n%s", truncForMsg(stdout))
		}
	})

	t.Run("full", func(t *testing.T) {
		stdout, _, code := runGrepCLI(t, "grep", "needle", file, "--json", "--full")
		if code != 0 {
			t.Fatalf("exit %d; stdout:\n%s", code, stdout)
		}
		var res struct {
			Matches []struct {
				Text      string `json:"text"`
				Truncated bool   `json:"truncated"`
			} `json:"matches"`
		}
		if err := json.Unmarshal([]byte(stdout), &res); err != nil {
			t.Fatalf("parse: %v\n%s", err, stdout)
		}
		if len(res.Matches) != 1 || len(res.Matches[0].Text) != len(longLine) {
			t.Errorf("--full did not return the whole line: got %d bytes, want %d",
				len(res.Matches[0].Text), len(longLine))
		}
		if res.Matches[0].Truncated {
			t.Error("--full still reported the line as truncated")
		}
	})
}

// TestGrepExplicitZeroTotal defends AXI principles 4 and 5 for grep: a search
// with no matches used to print nothing at all in plain mode and carry no
// count in any mode, so "no matches" and "something went wrong" read the same.
// Every mode must now state the zero.
func TestGrepExplicitZeroTotal(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(file, []byte("foo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, _, code := runGrepCLI(t, "grep", "zzq", file, "--json")
	if code != 0 {
		t.Fatalf("exit %d; stdout:\n%s", code, stdout)
	}
	if !strings.Contains(stdout, `"total_matches": 0`) {
		t.Errorf("--json dropped the zero total:\n%s", stdout)
	}

	stdout, _, code = runGrepCLI(t, "grep", "zzq", file, "--axi")
	if code != 0 {
		t.Fatalf("exit %d; stdout:\n%s", code, stdout)
	}
	if !strings.Contains(stdout, "total_matches: 0") {
		t.Errorf("--axi dropped the zero total:\n%s", stdout)
	}

	stdout, _, code = runGrepCLI(t, "grep", "zzq", file)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(stdout, "total: 0") {
		t.Errorf("plain mode said nothing about zero matches:\n%q", stdout)
	}
}

// TestGrepCountsMatches defends the non-zero side of principle 4: the total
// must be the number of matching lines (or files, with -l), not a field that
// only appears when it is non-zero.
func TestGrepCountsMatches(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("needle\nneedle again\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("nothing here\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, _, code := runGrepCLI(t, "grep", "needle", dir, "--json")
	if code != 0 {
		t.Fatalf("exit %d; stdout:\n%s", code, stdout)
	}
	if !strings.Contains(stdout, `"total_matches": 2`) {
		t.Errorf("wrong or missing total:\n%s", stdout)
	}

	stdout, _, code = runGrepCLI(t, "grep", "needle", dir, "-l", "--json")
	if code != 0 {
		t.Fatalf("exit %d; stdout:\n%s", code, stdout)
	}
	if !strings.Contains(stdout, `"total_matches": 1`) {
		t.Errorf("-l must count files with matches, not lines:\n%s", stdout)
	}
}

// truncForMsg keeps a failure message readable when the payload is one huge
// line: the point of the assertion is the shape, not 400 bytes of 'x'.
func truncForMsg(s string) string {
	if len(s) > 300 {
		return s[:300] + "..."
	}
	return s
}
