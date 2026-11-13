package commands

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/iotest"
	"unicode/utf8"
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

// TestGrepTruncationKeepsUTF8Valid defends the 200-byte cut against splitting
// a multi-byte rune: truncate sliced at a raw byte index, so a line whose
// 200th byte fell inside "é" emitted half a rune, which --json turned into
// U+FFFD and --axi wrote out as invalid UTF-8.
func TestGrepTruncationKeepsUTF8Valid(t *testing.T) {
	dir := t.TempDir()
	// 199 ASCII bytes, then 2-byte runes: the first "é" spans bytes 199-200.
	longLine := "needle" + strings.Repeat("x", 193) + strings.Repeat("é", 50)
	file := filepath.Join(dir, "utf8.txt")
	if err := os.WriteFile(file, []byte(longLine+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, _, code := runGrepCLI(t, "grep", "needle", file, "--json")
	if code != 0 {
		t.Fatalf("exit %d; stdout:\n%s", code, stdout)
	}
	var res struct {
		Matches []struct {
			Text       string `json:"text"`
			FullLength int    `json:"full_length"`
		} `json:"matches"`
	}
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatalf("parse: %v\n%s", err, stdout)
	}
	if len(res.Matches) != 1 {
		t.Fatalf("expected one match:\n%s", stdout)
	}
	text := res.Matches[0].Text
	if !utf8.ValidString(text) || strings.ContainsRune(text, utf8.RuneError) {
		t.Errorf("cut split a rune: %q", text)
	}
	if !strings.HasSuffix(text, "...") {
		t.Errorf("cut line lost its ... marker: %q", text)
	}
	if res.Matches[0].FullLength != len(longLine) {
		t.Errorf("full_length = %d, want %d", res.Matches[0].FullLength, len(longLine))
	}

	stdout, _, code = runGrepCLI(t, "grep", "needle", file, "--axi")
	if code != 0 {
		t.Fatalf("exit %d; stdout:\n%s", code, stdout)
	}
	if !utf8.ValidString(stdout) {
		t.Errorf("--axi emitted invalid UTF-8:\n%q", stdout)
	}
}

// TestTruncateCutsOnRuneBoundary defends truncate itself: the cut must land
// on a rune start at or below maxLen, never inside a multi-byte rune.
func TestTruncateCutsOnRuneBoundary(t *testing.T) {
	for _, tt := range []struct {
		in   string
		max  int
		want string
	}{
		{"abcé", 4, "abc..."},     // "é" is bytes 3-4: the cut at 4 splits it
		{"abcéd", 5, "abcé..."},   // the cut falls after "é"
		{"日本語", 4, "日..."},        // 3-byte runes: back off to byte 3
		{"short", 10, "short"},    // under the limit: untouched
		{"exactly", 7, "exactly"}, // at the limit: untouched
	} {
		got := truncate(tt.in, tt.max)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.in, tt.max, got, tt.want)
		}
		if !utf8.ValidString(got) {
			t.Errorf("truncate(%q, %d) = %q is not valid UTF-8", tt.in, tt.max, got)
		}
	}
}

// TestTruncateNoRuneStartBeforeCut defends the back-off loop's stop at 0: a
// line whose bytes up to the cut are all UTF-8 continuation bytes (Latin-1 or
// binary content) has no rune start to back off to, and an unbounded loop
// indexed s[-1] and panicked. Such a line yields just the marker.
func TestTruncateNoRuneStartBeforeCut(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("truncate panicked on continuation-only bytes: %v", r)
		}
	}()
	got := truncate(strings.Repeat("\x80", 300), 200)
	if got != "..." {
		t.Errorf("truncate = %q, want %q", got, "...")
	}
	if !utf8.ValidString(got) {
		t.Errorf("truncate = %q is not valid UTF-8", got)
	}
}

// TestGrepPlainModePrintsFullHint defends plain mode's next step: the --full
// hint went into Help, which printGrepText never printed, so a cut line ended
// in a bare "..." that read like content with no way to learn about --full.
// The hint must appear when a line was cut and not when none was.
func TestGrepPlainModePrintsFullHint(t *testing.T) {
	dir := t.TempDir()
	long := filepath.Join(dir, "long.txt")
	if err := os.WriteFile(long, []byte("needle"+strings.Repeat("x", 400)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	short := filepath.Join(dir, "short.txt")
	if err := os.WriteFile(short, []byte("needle here\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, _, code := runGrepCLI(t, "grep", "needle", long)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(stdout, "--full") {
		t.Errorf("a cut plain line gave no --full hint:\n%s", truncForMsg(stdout))
	}

	stdout, _, code = runGrepCLI(t, "grep", "needle", short)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if strings.Contains(stdout, "--full") {
		t.Errorf("printed the --full hint with nothing cut:\n%s", stdout)
	}
}

// TestGrepFindsMatchesOnLinesOver64KiB defends grep against a false zero on
// minified or generated files: bufio.Scanner stops at a line over its 64 KiB
// token limit, and scanner.Err() was never checked, so `grep needle min.js
// --full` printed `total: 0` and exited 0 with a real match in the file. A
// line past that size, and every line after it, must still be searched.
func TestGrepFindsMatchesOnLinesOver64KiB(t *testing.T) {
	longLine := "needle" + strings.Repeat("x", 100*1024)
	file := filepath.Join(t.TempDir(), "min.js")
	if err := os.WriteFile(file, []byte(longLine+"\nneedle after\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runGrepCLI(t, "grep", "needle", file, "--json", "--full")
	if code != 0 {
		t.Fatalf("exit %d; stderr:\n%s", code, stderr)
	}
	var res struct {
		Matches []struct {
			Text string `json:"text"`
		} `json:"matches"`
		TotalMatches int `json:"total_matches"`
	}
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatalf("parse: %v\n%s", err, truncForMsg(stdout))
	}
	if res.TotalMatches != 2 || len(res.Matches) != 2 {
		t.Fatalf("total_matches = %d with %d matches, want 2 and 2:\n%s",
			res.TotalMatches, len(res.Matches), truncForMsg(stdout))
	}
	if res.Matches[0].Text != longLine {
		t.Errorf("--full did not return the whole %d-byte line: got %d bytes",
			len(longLine), len(res.Matches[0].Text))
	}
	if res.Matches[1].Text != "needle after" {
		t.Errorf("line after the long one = %q, want %q", res.Matches[1].Text, "needle after")
	}

	stdout, _, code = runGrepCLI(t, "grep", "needle", file)
	if code != 0 || !strings.Contains(stdout, "total: 2") {
		t.Errorf("plain mode: exit %d, want total: 2:\n%s", code, truncForMsg(stdout))
	}
}

// TestGrepLinesKeepScannerLineEndings defends the line reader that replaced
// bufio.Scanner: a CRLF file's lines must not carry the \r, and a last line
// with no newline must still be read, exactly as before.
func TestGrepLinesKeepScannerLineEndings(t *testing.T) {
	var got []string
	err := grepLines(strings.NewReader("one\r\ntwo\nthree"), func(line string) bool {
		got = append(got, line)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"one", "two", "three"}; !slices.Equal(got, want) {
		t.Errorf("lines = %q, want %q", got, want)
	}
}

// TestGrepLinesReportsReadErrors defends against a failed read ending the
// scan quietly: a read error must reach the caller, not look like the end of
// a file with no more matches.
func TestGrepLinesReportsReadErrors(t *testing.T) {
	boom := errors.New("boom")
	r := io.MultiReader(strings.NewReader("needle\n"), iotest.ErrReader(boom))
	n := 0
	err := grepLines(r, func(string) bool { n++; return true })
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want %v", err, boom)
	}
	if n != 1 {
		t.Errorf("read %d lines before the error, want 1", n)
	}
}

// TestGrepSkipsDirectorySymlinksInTree defends grep over an ordinary tree that
// holds a symlink to a directory (pnpm, vendor and venv layouts do).
// filepath.Walk reports such a link as a file, so grep opened it and the first
// read returned EISDIR; once read errors were reported instead of dropped,
// that failed the whole search and hid the real matches. The link is not a
// file to search, so it is skipped and the run returns its matches.
func TestGrepSkipsDirectorySymlinksInTree(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "target"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pkg", "a.txt"), []byte("needle here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target", filepath.Join(dir, "link")); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}

	for _, mode := range [][]string{{"--json"}, nil} {
		stdout, stderr, code := runGrepCLI(t, append([]string{"grep", "needle", dir}, mode...)...)
		if code != 0 {
			t.Fatalf("%v: exit %d; stdout:\n%s\nstderr:\n%s", mode, code, stdout, stderr)
		}
		if !strings.Contains(stdout, "needle here") {
			t.Errorf("%v: the match beside the directory symlink was lost:\n%s", mode, stdout)
		}
	}
}

// TestGrepReadErrorFailsTheRun defends the read-error path in runGrep: a file
// whose read fails part-way must fail the run and name the file once, not end
// the scan quietly as if the file had no more matches.
func TestGrepReadErrorFailsTheRun(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(file, []byte("needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	orig := grepOpen
	t.Cleanup(func() { grepOpen = orig })

	// A bare error gets the file named; an *os.PathError, which is what
	// os.File.Read returns, already names it and must not be wrapped twice.
	for _, readErr := range []error{
		errors.New("boom"),
		&os.PathError{Op: "read", Path: file, Err: errors.New("boom")},
	} {
		grepOpen = func(string) (io.ReadCloser, error) {
			return io.NopCloser(io.MultiReader(strings.NewReader("needle\n"), iotest.ErrReader(readErr))), nil
		}

		cmd := newGrepCmd()
		out := new(bytes.Buffer)
		cmd.SetOut(out)
		cmd.SetErr(new(bytes.Buffer))
		cmd.SetArgs([]string{"needle", file})
		err := cmd.Execute()
		if err == nil {
			t.Fatalf("a failed read reported success:\n%s", out.String())
		}
		if msg := err.Error(); strings.Count(msg, file) != 1 || !strings.Contains(msg, "boom") {
			t.Errorf("err = %q, want it to name %s once and carry the cause", msg, file)
		}
		if strings.Contains(out.String(), "total") {
			t.Errorf("printed a result for a failed search:\n%s", out.String())
		}
	}
}
