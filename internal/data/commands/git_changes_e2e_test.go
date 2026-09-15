package commands

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// gitChangesRepo builds a repo with one committed file, one staged edit, one
// unstaged edit and one untracked file, then chdirs into it. That is the
// smallest fixture that tells the three filters apart.
func gitChangesRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}

	gitAt(t, resolved, "2026-01-01T00:00:00Z", "init", "-q")
	for _, name := range []string{"staged.txt", "unstaged.txt"} {
		if err := os.WriteFile(filepath.Join(resolved, name), []byte("one\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(resolved, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resolved, "sub", "nested.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitAt(t, resolved, "2026-01-01T00:00:00Z", "add", ".")
	gitAt(t, resolved, "2026-01-01T00:00:00Z", "commit", "-q", "-m", "base")

	write := func(rel, body string) {
		if err := os.WriteFile(filepath.Join(resolved, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("staged.txt", "two\n")
	gitAt(t, resolved, "2026-01-01T00:00:00Z", "add", "staged.txt")
	write("unstaged.txt", "two\n")
	write("sub/nested.txt", "two\n")
	write("untracked.txt", "new\n")

	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(resolved); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })
	return resolved
}

// runGitChangesCmd drives the real command tree, so the flag defaults, the
// porcelain call and the output path are all exercised together.
func runGitChangesCmd(t *testing.T, args ...string) string {
	t.Helper()
	t.Cleanup(func() {
		gitChangesPath = ""
		gitChangesIncludeUntracked = true
		gitChangesStagedOnly = false
		gitChangesJSON = false
		gitChangesMin = false
	})

	cmd := newGitChangesCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute %v: %v", args, err)
	}
	return buf.String()
}

func decodeGitChanges(t *testing.T, out string) GitChangesResult {
	t.Helper()
	var got GitChangesResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out)
	}
	return got
}

func TestGitChangesListsEveryChangedFile(t *testing.T) {
	gitChangesRepo(t)
	got := decodeGitChanges(t, runGitChangesCmd(t, "--json"))

	want := map[string]bool{
		"staged.txt": true, "unstaged.txt": true,
		"sub/nested.txt": true, "untracked.txt": true,
	}
	if got.Count != len(want) {
		t.Errorf("Count = %d, want %d (files: %v)", got.Count, len(want), got.Files)
	}
	for _, f := range got.Files {
		if !want[f] {
			t.Errorf("unexpected file reported: %s", f)
		}
		delete(want, f)
	}
	for f := range want {
		t.Errorf("missing file: %s", f)
	}
}

func TestGitChangesExcludesUntrackedOnRequest(t *testing.T) {
	gitChangesRepo(t)
	got := decodeGitChanges(t, runGitChangesCmd(t, "--json", "--include-untracked=false"))

	for _, f := range got.Files {
		if f == "untracked.txt" {
			t.Errorf("--include-untracked=false still reported %s", f)
		}
	}
	if got.Count != 3 {
		t.Errorf("Count = %d, want 3 (files: %v)", got.Count, got.Files)
	}
}

func TestGitChangesStagedOnlyReportsOnlyTheIndex(t *testing.T) {
	gitChangesRepo(t)
	got := decodeGitChanges(t, runGitChangesCmd(t, "--json", "--staged-only"))

	if got.Count != 1 || len(got.Files) != 1 || got.Files[0] != "staged.txt" {
		t.Errorf("--staged-only = %+v, want just staged.txt", got)
	}
}

func TestGitChangesFiltersByPathPrefix(t *testing.T) {
	gitChangesRepo(t)
	got := decodeGitChanges(t, runGitChangesCmd(t, "--json", "--path", "sub"))

	if got.Count != 1 || len(got.Files) != 1 || got.Files[0] != "sub/nested.txt" {
		t.Errorf("--path sub = %+v, want just sub/nested.txt", got)
	}
}

// "." means the repository root, not a literal directory named ".", so it must
// filter nothing out. This is normalizePathFilter's only interesting branch.
func TestGitChangesTreatsDotAsNoFilter(t *testing.T) {
	gitChangesRepo(t)
	unfiltered := decodeGitChanges(t, runGitChangesCmd(t, "--json"))
	dot := decodeGitChanges(t, runGitChangesCmd(t, "--json", "--path", "."))

	if dot.Count != unfiltered.Count {
		t.Errorf("--path . reported %d files, want all %d", dot.Count, unfiltered.Count)
	}
}

func TestGitChangesPathFilterCleansTheInput(t *testing.T) {
	gitChangesRepo(t)
	got := decodeGitChanges(t, runGitChangesCmd(t, "--json", "--path", "./sub/"))

	if got.Count != 1 {
		t.Errorf("--path ./sub/ = %+v, want the same one file as --path sub", got)
	}
}

func TestGitChangesTextOutputNamesTheFiles(t *testing.T) {
	gitChangesRepo(t)
	got := runGitChangesCmd(t)

	if !strings.Contains(got, "staged.txt") {
		t.Errorf("text output does not name the changed files:\n%s", got)
	}
}

func TestGitChangesMinOutputIsJustTheCount(t *testing.T) {
	gitChangesRepo(t)
	got := strings.TrimSpace(runGitChangesCmd(t, "--min"))

	if got != "4" {
		t.Errorf("--min = %q, want \"4\"", got)
	}
}

// A clean tree must never encode files as null. Pinned as the code actually
// behaves: the `omitempty` tag on Files means an empty slice drops the key
// entirely, so the payload is {"count":0}. Note that this makes the nil-to-
// empty-slice normalisation in outputGitChanges unreachable — the key is gone
// either way. That dead code is filed as technical debt rather than fixed
// here, because git_changes_test.go pins the omission as the contract and a
// port is the wrong place to change one.
func TestGitChangesEmitsAnEmptyListNotNull(t *testing.T) {
	dir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	gitAt(t, resolved, "2026-01-01T00:00:00Z", "init", "-q")
	if err := os.WriteFile(filepath.Join(resolved, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitAt(t, resolved, "2026-01-01T00:00:00Z", "add", ".")
	gitAt(t, resolved, "2026-01-01T00:00:00Z", "commit", "-q", "-m", "clean")

	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(resolved); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })

	out := runGitChangesCmd(t, "--json")
	if strings.Contains(out, "null") {
		t.Errorf("a clean tree encoded files as null:\n%s", out)
	}
	got := decodeGitChanges(t, out)
	if got.Count != 0 || len(got.Files) != 0 {
		t.Errorf("a clean tree reported %+v, want count 0 and no files", got)
	}
}

func TestGitChangesRefusesOutsideARepository(t *testing.T) {
	dir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(resolved); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })

	cmd := newGitChangesCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"--json"})
	if err := cmd.Execute(); err == nil {
		t.Error("git-changes succeeded outside a git repository")
	}
}
