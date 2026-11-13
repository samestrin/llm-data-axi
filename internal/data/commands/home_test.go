package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// chdirTo makes dir the working directory for the rest of the test.
func chdirTo(t *testing.T, dir string) {
	t.Helper()
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })
}

// outsideAnyRepo moves the test into a fresh directory that git cannot climb
// out of, so the real git answers "not a git repository" whatever repository
// the temp directory happens to sit inside.
func outsideAnyRepo(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	chdirTo(t, dir)
	return dir
}

// fakeGit puts a shell script named git first on PATH, so a test can make git
// fail in a chosen way through the same exec path the home view really uses.
func fakeGit(t *testing.T, script string) {
	t.Helper()
	bin := t.TempDir()
	body := "#!/bin/sh\n" + script + "\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// homeRepo builds a Go repository under home with two untracked files and
// moves the test into a subdirectory of it, so the root has to be found by
// climbing rather than read off the working directory.
func homeRepo(t *testing.T, home string) string {
	t.Helper()
	repo := filepath.Join(home, "src", "proj")
	if err := os.MkdirAll(filepath.Join(repo, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitAt(t, repo, "2026-01-01T00:00:00Z", "init", "-q")
	for _, name := range []string{"go.mod", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(repo, name), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	chdirTo(t, filepath.Join(repo, "sub"))
	return repo
}

// Defends the live home view: inside a repository a bare run reports where the
// repository is, what kind of project it is and how many changes it carries,
// read from the real git and the real detect logic.
func TestHomeViewInsideRepo(t *testing.T) {
	home := fakeExecutable(t)
	homeRepo(t, home)

	out := runRootBoth(t)

	for _, want := range []string{
		"bin: ~/go/bin/llm-data\n",
		"description: " + homeDescription + "\n",
		"repo_root: ~/src/proj\n",
		"project_type: go\n",
		"git_changes: 2\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("home view missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "repo: ") {
		t.Errorf("a healthy repository printed a repo status line:\n%s", out)
	}
}

// Defends the structured encodings carrying the live fields: a --json or
// --axi caller must get the same facts the plain view shows, plus help[].
func TestHomeViewInsideRepoEncodings(t *testing.T) {
	home := fakeExecutable(t)
	homeRepo(t, home)

	var got map[string]any
	if err := json.Unmarshal([]byte(runRootBoth(t, "--json")), &got); err != nil {
		t.Fatalf("--json home view is not JSON: %v", err)
	}
	if got["repo_root"] != "~/src/proj" || got["project_type"] != "go" || got["git_changes"] != float64(2) {
		t.Errorf("--json live fields wrong: %v", got)
	}
	if help, _ := got["help"].([]any); len(help) == 0 {
		t.Errorf("--json home view has no help[]: %v", got)
	}
	if _, ok := got["repo"]; ok {
		t.Errorf("--json healthy repository carries a repo status: %v", got)
	}

	axi := runRootBoth(t, "--axi")
	for _, want := range []string{"repo_root: ~/src/proj", "project_type: go", "git_changes: 2", "help"} {
		if !strings.Contains(axi, want) {
			t.Errorf("--axi home view missing %q:\n%s", want, axi)
		}
	}
}

// Defends a clean working tree reading as zero changes rather than as a
// missing field: zero is an answer, an absent count is not.
func TestHomeViewCleanRepoReportsZero(t *testing.T) {
	home := fakeExecutable(t)
	repo := homeRepo(t, home)
	gitAt(t, repo, "2026-01-01T00:00:00Z", "add", ".")
	gitAt(t, repo, "2026-01-01T00:00:00Z", "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "base")

	if out := runRootBoth(t); !strings.Contains(out, "git_changes: 0\n") {
		t.Errorf("clean tree did not report git_changes: 0:\n%s", out)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(runRootBoth(t, "--json")), &got); err != nil {
		t.Fatal(err)
	}
	if got["git_changes"] != float64(0) {
		t.Errorf("--json clean tree git_changes = %v, want 0", got["git_changes"])
	}
}

// Defends the explicit empty state: outside a repository the view says so in
// words, exits 0, and prints no repository fields at all.
func TestHomeViewNoRepository(t *testing.T) {
	fakeExecutable(t)
	outsideAnyRepo(t)

	out := runRootBoth(t)

	if !strings.Contains(out, "repo: no repository\n") {
		t.Errorf("outside a repository the view did not say so:\n%s", out)
	}
	for _, field := range []string{"repo_root:", "project_type:", "git_changes:", "git unavailable"} {
		if strings.Contains(out, field) {
			t.Errorf("outside a repository the view printed %q:\n%s", field, out)
		}
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(runRootBoth(t, "--json")), &got); err != nil {
		t.Fatal(err)
	}
	if got["repo"] != "no repository" {
		t.Errorf("--json repo = %v, want \"no repository\"", got["repo"])
	}
	if _, ok := got["repo_root"]; ok {
		t.Errorf("--json outside a repository carries repo_root: %v", got)
	}
}

// assertGitUnavailable checks the one rule every git failure shares: it is
// named as git being unavailable, with the reason, and never as "no
// repository", and the run still succeeds.
func assertGitUnavailable(t *testing.T, reason string) {
	t.Helper()
	out := runRootBoth(t)
	if want := "repo: git unavailable: " + reason; !strings.Contains(out, want) {
		t.Errorf("want %q in:\n%s", want, out)
	}
	if strings.Contains(out, "no repository") {
		t.Errorf("a git failure was reported as no repository:\n%s", out)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(runRootBoth(t, "--json")), &got); err != nil {
		t.Fatal(err)
	}
	if s, _ := got["repo"].(string); !strings.HasPrefix(s, "git unavailable: "+reason) {
		t.Errorf("--json repo = %q, want git unavailable: %s", s, reason)
	}
}

// Defends git missing from PATH reading as "git unavailable", not as "no
// repository": the caller may well be inside a repository.
func TestHomeViewGitMissing(t *testing.T) {
	fakeExecutable(t)
	chdirTo(t, t.TempDir())
	t.Setenv("PATH", t.TempDir())

	assertGitUnavailable(t, "git not found in PATH")
}

// Defends a git error other than not-a-repository (here a broken config)
// reading as "git unavailable" with git's own message as the reason. The
// old repo-root handler collapsed every git error into "not a git
// repository"; the home view must not.
func TestHomeViewGitError(t *testing.T) {
	fakeExecutable(t)
	chdirTo(t, t.TempDir())
	fakeGit(t, `echo "fatal: bad config line 1 in file .git/config" >&2; exit 128`)

	assertGitUnavailable(t, "fatal: bad config line 1 in file .git/config")
}

// Defends a hung git: the bare run must come back, within the limit, saying
// git did not answer, rather than hang a session-start hook or guess.
func TestHomeViewGitTimeout(t *testing.T) {
	fakeExecutable(t)
	chdirTo(t, t.TempDir())
	fakeGit(t, `exec sleep 5`)
	orig := homeGitTimeout
	homeGitTimeout = 200 * time.Millisecond
	t.Cleanup(func() { homeGitTimeout = orig })

	start := time.Now()
	assertGitUnavailable(t, "no answer within 200ms")
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("two timed-out runs took %v; the limit was not enforced", elapsed)
	}
}

// Defends the limit itself: the issue fixes it at two seconds.
func TestHomeGitTimeoutIsTwoSeconds(t *testing.T) {
	if homeGitTimeout != 2*time.Second {
		t.Errorf("homeGitTimeout = %v, want 2s", homeGitTimeout)
	}
}

// Defends a failure after the root was found: the root and project type are
// still facts worth printing, but the change count must not be invented, and
// the failure must be named.
func TestHomeViewStatusFailsAfterRoot(t *testing.T) {
	fakeExecutable(t)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	chdirTo(t, dir)
	fakeGit(t, `case "$1" in
rev-parse) pwd ;;
*) echo "fatal: index file corrupt" >&2; exit 128 ;;
esac`)

	out := runRootBoth(t)

	for _, want := range []string{"repo_root: " + dir + "\n", "project_type: go\n", "repo: git unavailable: fatal: index file corrupt\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "git_changes:") {
		t.Errorf("a failed git status still printed a change count:\n%s", out)
	}
}

// Defends a git that fails without a word on stderr (killed by a signal, for
// example): the reason must still be named, never an empty
// "git unavailable: ".
func TestHomeViewGitErrorWithoutStderr(t *testing.T) {
	fakeExecutable(t)
	chdirTo(t, t.TempDir())
	fakeGit(t, `exit 128`)

	assertGitUnavailable(t, "git rev-parse: exit status 128")
}
