package commands

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Defends the --strict contract ("Error on undefined variables"): an undefined
// variable printed ERROR on stderr and then `status: success` on stdout with
// exit 0, so a caller reading stdout and the status was told the render
// worked. It must be an error on stdout, exit 1, naming every undefined name.
func TestTemplateStrictUndefinedIsAnError(t *testing.T) {
	dir := t.TempDir()
	tmpl := filepath.Join(dir, "t.txt")
	if err := os.WriteFile(tmpl, []byte("hi {{x}} {{name}} {{y}} {{x}} {{d|ok}}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	code, stdout, _ := runCLI(t, "template", tmpl, "--strict", "--var", "name=Bob")
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if !strings.HasPrefix(stdout, "error: true\n") || strings.Contains(stdout, "success") {
		t.Errorf("want a TOON error and no success on stdout, got %q", stdout)
	}
	if !strings.Contains(stdout, "undefined variables with --strict: x, y") {
		t.Errorf("error must name x and y once each, in order, got %q", stdout)
	}

	code, stdout, _ = runCLI(t, "template", tmpl, "--strict", "--var", "name=Bob", "--json")
	if code != 1 {
		t.Errorf("--json: exit %d, want 1", code)
	}
	var got struct {
		Error     bool     `json:"error"`
		Undefined []string `json:"undefined"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("--json: stdout is not JSON: %v\n%s", err, stdout)
	}
	if !got.Error || strings.Join(got.Undefined, ",") != "x,y" {
		t.Errorf("--json: want error with undefined [x y], got %+v", got)
	}

	// With -o the output file must not be written: a rendered file holding
	// raw placeholders is the false success in another place.
	out := filepath.Join(dir, "out.txt")
	if code, _, _ = runCLI(t, "template", tmpl, "--strict", "-o", out); code != 1 {
		t.Errorf("-o: exit %d, want 1", code)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("-o: output file was written despite undefined variables (stat err %v)", err)
	}

	// Every variable defined or defaulted: --strict still succeeds.
	code, stdout, _ = runCLI(t, "template", tmpl, "--strict", "--var", "name=Bob", "--var", "x=1", "--var", "y=2", "--plain")
	if code != 0 || stdout != "hi 1 Bob 2 1 ok\n" {
		t.Errorf("all defined: exit %d, stdout %q", code, stdout)
	}
}

// Defends catfiles against dropping a file it was asked for: an entry that
// could not be stat'ed (a dangling symlink) vanished without a word and the
// run exited 0, so the caller took the concatenation as the whole directory.
func TestCatfilesStatFailureIsAnError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("alpha\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "b.txt")); err != nil {
		t.Fatal(err)
	}

	code, stdout, _ := runCLI(t, "catfiles", dir, "--json")
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	var got struct {
		Error      bool `json:"error"`
		Unreadable []struct {
			Path   string `json:"path"`
			Reason string `json:"reason"`
		} `json:"unreadable"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if !got.Error || len(got.Unreadable) != 1 || filepath.Base(got.Unreadable[0].Path) != "b.txt" || got.Unreadable[0].Reason == "" {
		t.Errorf("want an error listing b.txt with a reason, got %+v", got)
	}
	if strings.Contains(stdout, "alpha") {
		t.Errorf("a failed run must not print the files it could read as a result: %s", stdout)
	}
}

// Defends catfiles against a file it cannot read: it warned on stderr, left
// the file out and exited 0, so a caller reading stdout saw a complete-looking
// result. Root reads a mode-000 file, so there the case cannot be built.
func TestCatfilesUnreadableFileIsAnError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("alpha\n"), 0644); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(dir, "locked.txt")
	if err := os.WriteFile(locked, []byte("secret\n"), 0000); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(locked); err == nil {
		t.Skip("running as a user that reads mode-000 files")
	}

	code, stdout, _ := runCLI(t, "catfiles", dir)
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if !strings.HasPrefix(stdout, "error: true\n") || !strings.Contains(stdout, "locked.txt") {
		t.Errorf("want a TOON error naming locked.txt on stdout, got %q", stdout)
	}
	if !strings.Contains(stdout, "permission denied") {
		t.Errorf("error must say why the file could not be read, got %q", stdout)
	}
}

// Defends catfiles against an unreadable subdirectory: the walk error was
// swallowed, so everything under it vanished from a run that exited 0.
func TestCatfilesUnreadableSubdirIsAnError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("alpha\n"), 0644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "c.txt"), []byte("gamma\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sub, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(sub, 0755) })
	if _, err := os.ReadDir(sub); err == nil {
		t.Skip("running as a user that reads mode-000 directories")
	}

	code, stdout, _ := runCLI(t, "catfiles", dir)
	if code != 1 || !strings.Contains(stdout, "sub") || !strings.Contains(stdout, "permission denied") {
		t.Errorf("want exit 1 and an error naming sub, got exit %d, %q", code, stdout)
	}
}

// Defends multigrep --output-dir: a keyword file that could not be written
// warned on stderr while stdout reported the matches and the run exited 0, so
// a caller went looking for files that do not exist. A directory squatting on
// the keyword file's name fails the write for any user, root included.
func TestMultigrepOutputDirWriteFailureIsAnError(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "a.go"), []byte("needle\nhay\n"), 0644); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := os.Mkdir(filepath.Join(out, "keyword_needle.txt"), 0755); err != nil {
		t.Fatal(err)
	}

	code, stdout, _ := runCLI(t, "multigrep", "--keywords", "needle,hay", "--path", src, "--output-dir", out, "--json")
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	var got struct {
		Error        bool `json:"error"`
		FailedWrites []struct {
			File   string `json:"file"`
			Reason string `json:"reason"`
		} `json:"failed_writes"`
		TotalMatches *int `json:"total_matches"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if !got.Error || len(got.FailedWrites) != 1 || filepath.Base(got.FailedWrites[0].File) != "keyword_needle.txt" || got.FailedWrites[0].Reason == "" {
		t.Errorf("want an error listing keyword_needle.txt with a reason, got %+v", got)
	}
	if got.TotalMatches != nil {
		t.Errorf("a failed run must not report matches as a result: %s", stdout)
	}
}

// Defends the gitignore filter against the unreadable-entry check: a
// directory .gitignore excludes was never part of the output, so failing the
// run because it cannot be read would turn a correct result into an error.
func TestCatfilesIgnoredUnreadableSubdirIsNotAnError(t *testing.T) {
	for _, pattern := range []string{"locked/", "locked"} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(pattern+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("alpha\n"), 0644); err != nil {
			t.Fatal(err)
		}
		locked := filepath.Join(dir, "locked")
		if err := os.Mkdir(locked, 0000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(locked, 0755) })
		if _, err := os.ReadDir(locked); err == nil {
			t.Skip("running as a user that reads mode-000 directories")
		}

		code, stdout, _ := runCLI(t, "catfiles", dir, "--plain")
		if code != 0 || !strings.Contains(stdout, "alpha") {
			t.Errorf("pattern %q: want exit 0 with a.txt, got exit %d, %q", pattern, code, stdout)
		}
	}
}

// Defends the "no files found" guard against hiding an unreadable entry: when
// everything requested is unreadable there are no files, and without the
// guard's unreadable check the run reported `no files found`, which tells the
// caller the directory is empty when it is the read that failed.
func TestCatfilesOnlyUnreadableIsNotNoFilesFound(t *testing.T) {
	dir := t.TempDir()
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0755) })
	if _, err := os.ReadDir(locked); err == nil {
		t.Skip("running as a user that reads mode-000 directories")
	}

	code, stdout, _ := runCLI(t, "catfiles", dir, "--json")
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if strings.Contains(stdout, "no files found") {
		t.Errorf("an unreadable directory was reported as empty: %s", stdout)
	}
	var got struct {
		Unreadable []struct {
			Path string `json:"path"`
		} `json:"unreadable"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if len(got.Unreadable) != 1 || filepath.Base(got.Unreadable[0].Path) != "locked" {
		t.Errorf("want an unreadable list naming locked, got %+v", got)
	}
}

// Defends failureReason's fallback: an error that is not an *os.PathError
// must keep its own text, or the unreadable and failed_writes lists carry an
// empty reason.
func TestFailureReasonKeepsPlainErrorText(t *testing.T) {
	if got := failureReason(errors.New("boom")); got != "boom" {
		t.Errorf("failureReason(boom) = %q, want boom", got)
	}
	pathErr := &os.PathError{Op: "open", Path: "/x", Err: errors.New("permission denied")}
	if got := failureReason(pathErr); got != "permission denied" {
		t.Errorf("failureReason(PathError) = %q, want the cause without the path", got)
	}
}

// Defends grep against a file it cannot open: the open error was skipped with
// `continue`, so `total: 0` and exit 0 read the same as a search of that file
// that found nothing. A dangling symlink fails the open for any user, root
// included. The match in the readable file must not be printed as a result.
func TestGrepUnopenableFileIsAnError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("needle\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "b.txt")); err != nil {
		t.Fatal(err)
	}

	code, stdout, _ := runCLI(t, "grep", "needle", dir, "--json")
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	var got struct {
		Error      bool `json:"error"`
		Unreadable []struct {
			Path   string `json:"path"`
			Reason string `json:"reason"`
		} `json:"unreadable"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if !got.Error || len(got.Unreadable) != 1 || filepath.Base(got.Unreadable[0].Path) != "b.txt" || got.Unreadable[0].Reason == "" {
		t.Errorf("want an error listing b.txt with a reason, got %+v", got)
	}
	if strings.Contains(stdout, "total_matches") {
		t.Errorf("a failed run must not report matches as a result: %s", stdout)
	}
}

// Defends grep against a mode-000 file: it was skipped and the run exited 0.
// Root reads a mode-000 file, so there the case cannot be built.
func TestGrepUnreadableFileIsAnError(t *testing.T) {
	dir := t.TempDir()
	locked := filepath.Join(dir, "locked.txt")
	if err := os.WriteFile(locked, []byte("needle\n"), 0000); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(locked); err == nil {
		t.Skip("running as a user that reads mode-000 files")
	}

	code, stdout, _ := runCLI(t, "grep", "needle", dir)
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if !strings.HasPrefix(stdout, "error: true\n") || !strings.Contains(stdout, "locked.txt") || !strings.Contains(stdout, "permission denied") {
		t.Errorf("want a TOON error naming locked.txt and why, got %q", stdout)
	}
}

// Defends grep against an unreadable subdirectory: the walk error returned
// nil, so everything under it vanished from a run that exited 0.
func TestGrepUnreadableSubdirIsAnError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("alpha\n"), 0644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "c.txt"), []byte("needle\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sub, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(sub, 0755) })
	if _, err := os.ReadDir(sub); err == nil {
		t.Skip("running as a user that reads mode-000 directories")
	}

	code, stdout, _ := runCLI(t, "grep", "needle", dir, "--plain")
	if code != 1 || !strings.Contains(stdout, "sub") || !strings.Contains(stdout, "permission denied") {
		t.Errorf("want exit 1 and an error naming sub, got exit %d, %q", code, stdout)
	}
}

// Defends grep's gitignore filter against the unreadable-entry check: an
// ignored directory was never searched, so failing on it would turn a correct
// result into an error.
func TestGrepIgnoredUnreadableSubdirIsNotAnError(t *testing.T) {
	for _, pattern := range []string{"locked/", "locked"} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(pattern+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("needle\n"), 0644); err != nil {
			t.Fatal(err)
		}
		locked := filepath.Join(dir, "locked")
		if err := os.Mkdir(locked, 0000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(locked, 0755) })
		if _, err := os.ReadDir(locked); err == nil {
			t.Skip("running as a user that reads mode-000 directories")
		}

		code, stdout, _ := runCLI(t, "grep", "needle", dir, "--plain")
		if code != 0 || !strings.Contains(stdout, "a.txt:needle") {
			t.Errorf("pattern %q: want exit 0 with the a.txt match, got exit %d, %q", pattern, code, stdout)
		}
	}
}
