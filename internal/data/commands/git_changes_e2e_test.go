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

// A clean tree must emit `"files": []` — present and empty, never null and
// never absent. The `omitempty` tag was deleting the key instead, so a consumer
// iterating `files` hit a case the schema never announced.
//
// What actually delivers this is parseGitStatus initialising Files to
// []string{}, plus the tag.
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
	// Checked on the raw payload, not on the decoded struct: encoding/json
	// gives a nil slice for BOTH an absent key and an explicit null, so a
	// decoded `len(Files) == 0` passes for all three shapes and cannot tell the
	// bug from the fix.
	if !strings.Contains(out, `"files": []`) {
		t.Errorf("a clean tree did not emit an empty files list:\n%s", out)
	}
	got := decodeGitChanges(t, out)
	if got.Count != 0 || len(got.Files) != 0 {
		t.Errorf("a clean tree reported %+v, want count 0 and no files", got)
	}
}

// The same guarantee on the AXI path. EncodeAXI projects through the JSON form
// first, so the struct tag governs both encodings and a fix that only reached
// one of them would be half a fix.
func TestGitChangesAXIEmitsAnEmptyListNotNull(t *testing.T) {
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

	withAXI(t)
	out := runGitChangesCmd(t)

	if strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Fatalf("--axi was ignored; JSON was emitted:\n%s", out)
	}
	if !strings.Contains(out, "files") {
		t.Errorf("the AXI payload dropped the files key entirely:\n%s", out)
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

// Defends epic 18 T3: a --path that names nothing, neither on disk nor in
// git, is an error and not a clean result. It used to report `count: 0` and
// exit 0, the same answer as a real path with no changes, so a typo in the
// filter read as "nothing changed here".
func TestGitChangesRejectsAPathThatIsNowhere(t *testing.T) {
	gitChangesRepo(t)
	for _, f := range errorFormats {
		t.Run(f.name, func(t *testing.T) {
			args := append([]string{"git-changes", "--path", "NOPE"}, f.flags...)
			code, stdout, stderr := runCLI(t, args...)
			if code != 1 {
				t.Errorf("exit %d, want 1 (stdout %q)", code, stdout)
			}
			if stderr != "" {
				t.Errorf("stderr must be empty, got %q", stderr)
			}
			msg, ok := f.message(stdout)
			if !ok {
				t.Fatalf("stdout is not a %s error: %q", f.name, stdout)
			}
			if !strings.Contains(msg, "path not found: NOPE") {
				t.Errorf("message = %q, want it to name path not found: NOPE", msg)
			}
		})
	}
}

// Defends the other side of the T3 check: a directory that exists but holds
// no changes is a real answer, `count: 0`, not an error.
func TestGitChangesQuietPathOnDiskIsAnEmptyResult(t *testing.T) {
	root := gitChangesRepo(t)
	if err := os.Mkdir(filepath.Join(root, "quiet"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, flags := range [][]string{nil, {"--json"}, {"--plain"}} {
		args := append([]string{"git-changes", "--path", "quiet"}, flags...)
		code, stdout, stderr := runCLI(t, args...)
		if code != 0 || stderr != "" {
			t.Errorf("%v: exit %d, stderr %q, want 0 and empty", flags, code, stderr)
		}
		if !strings.Contains(stdout, "count: 0") && !strings.Contains(stdout, `"count": 0`) &&
			!strings.Contains(stdout, "COUNT: 0") {
			t.Errorf("%v: want a zero count, got %q", flags, stdout)
		}
	}
}

// Defends the reason T3 asks git as well as the disk: a tracked file deleted
// from the working tree, or staged for deletion, is gone from disk but is
// still a change under the prefix. A disk-only check would refuse the very
// path whose deletion the caller is asking about.
func TestGitChangesListsADeletedTrackedFile(t *testing.T) {
	root := gitChangesRepo(t)
	if err := os.RemoveAll(filepath.Join(root, "sub")); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"sub", "sub/nested.txt"} {
		got := decodeGitChanges(t, runGitChangesCmd(t, "--json", "--path", p))
		if got.Count != 1 || len(got.Files) != 1 || got.Files[0] != "sub/nested.txt" {
			t.Errorf("--path %s after deleting sub = %+v, want sub/nested.txt", p, got)
		}
	}

	// Staged: `git rm` takes it out of the index too, so only the status
	// itself still knows the path.
	gitAt(t, root, "2026-01-01T00:00:00Z", "rm", "-q", "--cached", "sub/nested.txt")
	got := decodeGitChanges(t, runGitChangesCmd(t, "--json", "--path", "sub"))
	if got.Count != 1 || got.Files[0] != "sub/nested.txt" {
		t.Errorf("--path sub after git rm = %+v, want sub/nested.txt", got)
	}
}
