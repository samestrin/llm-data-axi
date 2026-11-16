package commands

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// fakeGitOnPath makes PATH hold only a git that prints stderr and exits with
// code, or no git at all when stderr is empty and code is 0. It is how a test
// reaches a git failure on demand.
func fakeGitOnPath(t *testing.T, stderr string, code int) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake git is a shell script")
	}
	dir := t.TempDir()
	if stderr != "" || code != 0 {
		script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' '%s' >&2\nexit %d\n", stderr, code)
		if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
}

func runGitFailureCmd(cmd *cobra.Command, args ...string) (string, error) {
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

// TestRepoRootSaysNotARepositoryOnlyWhenGitDoes defends repo-root against
// labelling every git failure "not a git repository" with exit 0: a missing
// or broken git read as an ordinary directory outside a repository. Only
// git's own not-a-repository answer earns that label; any other failure exits
// non-zero with its own reason.
func TestRepoRootSaysNotARepositoryOnlyWhenGitDoes(t *testing.T) {
	t.Cleanup(func() { repoRootPath, repoRootValidate, repoRootJSON = ".", false, false })
	dir := t.TempDir()

	fakeGitOnPath(t, "fatal: not a git repository (or any of the parent directories): .git", 128)
	out, err := runGitFailureCmd(newRepoRootCmd(), "--path", dir)
	if err != nil {
		t.Fatalf("not a repository must still exit 0, got %v", err)
	}
	if !strings.Contains(out, "GIT_REPOSITORY: FALSE") {
		t.Errorf("want the not-a-repository answer:\n%s", out)
	}

	for name, tc := range map[string]struct {
		stderr string
		code   int
		reason string
	}{
		"git missing": {"", 0, "git not found in PATH"},
		"git broken":  {"fatal: detected dubious ownership in repository at '/x'", 128, "dubious ownership"},
	} {
		fakeGitOnPath(t, tc.stderr, tc.code)
		out, err := runGitFailureCmd(newRepoRootCmd(), "--path", dir)
		if err == nil {
			t.Errorf("%s: exit 0, want a failure:\n%s", name, out)
			continue
		}
		if !strings.Contains(err.Error(), tc.reason) {
			t.Errorf("%s: err = %q, want it to carry %q", name, err, tc.reason)
		}
		if strings.Contains(err.Error()+out, "not a git repository") {
			t.Errorf("%s: labelled not a git repository:\n%s%v", name, out, err)
		}
	}
}

// TestGitChangesPreCheckKeepsGitsReason defends the same mislabel in
// git-changes: its pre-check turned every git failure into "not a git
// repository". Only git's own answer says that now; a broken git reports why.
func TestGitChangesPreCheckKeepsGitsReason(t *testing.T) {
	t.Cleanup(func() { gitChangesPath = "" })
	gitChangesPath = ""

	for name, tc := range map[string]struct {
		stderr  string
		code    int
		reason  string
		notRepo bool
	}{
		"not a repository": {"fatal: not a git repository (or any of the parent directories): .git", 128, "not a git repository", true},
		"git missing":      {"", 0, "git not found in PATH", false},
		"git broken":       {"fatal: detected dubious ownership in repository at '/x'", 128, "dubious ownership", false},
	} {
		fakeGitOnPath(t, tc.stderr, tc.code)
		out, err := runGitFailureCmd(newGitChangesCmd())
		if err == nil {
			t.Errorf("%s: exit 0, want a failure:\n%s", name, out)
			continue
		}
		if !strings.Contains(err.Error(), tc.reason) {
			t.Errorf("%s: err = %q, want it to carry %q", name, err, tc.reason)
		}
		if !tc.notRepo && strings.Contains(err.Error(), "not a git repository") {
			t.Errorf("%s: labelled not a git repository: %v", name, err)
		}
	}
}

// TestRepoRootOutsideARepositoryIsAFactNotAnError defends against repo-root
// exiting 0 while printing `error:` outside a repository: plain printed
// `ERROR: not a git repository`, --json an "error" key and --axi
// `error: not a git repository`, all with exit 0, so a caller reading the
// payload saw a failure and a caller reading the exit code saw a success.
// Exit 0 for "no repository" is pinned by
// TestRepoRootSaysNotARepositoryOnlyWhenGitDoes, so the answer is now a
// non-error fact, git_repository: false, in every format.
func TestRepoRootOutsideARepositoryIsAFactNotAnError(t *testing.T) {
	dir := t.TempDir()
	fakeGitOnPath(t, "fatal: not a git repository (or any of the parent directories): .git", 128)

	for _, tc := range []struct {
		mode []string
		want string
	}{
		{[]string{"--plain"}, "GIT_REPOSITORY: FALSE"},
		{nil, "git_repository: false"},
		{[]string{"--json"}, `"git_repository": false`},
		{[]string{"--json", "--min"}, `"git_repository":false`},
		{[]string{"--axi"}, "git_repository: false"},
	} {
		args := append([]string{"repo-root", "--path", dir}, tc.mode...)
		code, stdout, stderr := runCLI(t, args...)
		if code != 0 {
			t.Errorf("%v: exit %d, want 0\nstdout=%q stderr=%q", args, code, stdout, stderr)
		}
		if !strings.Contains(stdout, tc.want) {
			t.Errorf("%v: want %q in:\n%s", args, tc.want, stdout)
		}
		// Matched as keys, not the bare word: the temp dir's own path holds
		// this test's name.
		for _, bad := range []string{`"error"`, `"err"`, "error:", "ERROR:", "not a git repository"} {
			if strings.Contains(stdout+stderr, bad) {
				t.Errorf("%v: an exit-0 answer must not carry %q:\nstdout=%q stderr=%q", args, bad, stdout, stderr)
			}
		}
	}
}

// TestRepoRootInsideARepositorySaysSo is the other side: inside a
// repository the structured formats state git_repository: true beside the
// root, so the field is a fact a caller can read either way.
func TestRepoRootInsideARepositorySaysSo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	for _, tc := range []struct {
		mode []string
		want []string
	}{
		{[]string{"--plain"}, []string{"ROOT: " + dir}},
		{nil, []string{"git_repository: true", dir}},
		{[]string{"--json"}, []string{`"git_repository": true`, `"root": "` + dir + `"`}},
		{[]string{"--axi"}, []string{"git_repository: true", dir}},
	} {
		args := append([]string{"repo-root", "--path", dir}, tc.mode...)
		code, stdout, stderr := runCLI(t, args...)
		if code != 0 {
			t.Errorf("%v: exit %d\nstdout=%q stderr=%q", args, code, stdout, stderr)
		}
		for _, w := range tc.want {
			if !strings.Contains(stdout, w) {
				t.Errorf("%v: want %q in:\n%s", args, w, stdout)
			}
		}
	}
}

// TestGitContextSaysNotARepositoryOnlyWhenGitDoes defends git-context
// against the same collapse repo-root and git-changes had: its pre-check ran
// a bare `git rev-parse --git-dir` and reported every failure, a dubious
// ownership refusal or a broken config included, as "not a git repository".
// Only git's own answer says that now; any other failure exits 1 with git's
// reason.
func TestGitContextSaysNotARepositoryOnlyWhenGitDoes(t *testing.T) {
	dir := t.TempDir()
	for name, tc := range map[string]struct {
		stderr  string
		code    int
		reason  string
		notRepo bool
	}{
		"not a repository": {"fatal: not a git repository (or any of the parent directories): .git", 128, "not a git repository", true},
		"git missing":      {"", 0, "git not found in PATH", false},
		"dubious owner":    {"fatal: detected dubious ownership in repository at '/x'", 128, "dubious ownership", false},
		"broken config":    {"fatal: bad config line 1 in file .git/config", 128, "bad config line 1", false},
	} {
		fakeGitOnPath(t, tc.stderr, tc.code)
		for _, mode := range [][]string{nil, {"--plain"}, {"--json"}, {"--axi"}} {
			args := append([]string{"git-context", "--path", dir}, mode...)
			code, stdout, stderr := runCLI(t, args...)
			if code != 1 {
				t.Errorf("%s %v: exit %d, want 1\nstdout=%q stderr=%q", name, mode, code, stdout, stderr)
			}
			msg := stdout + stderr
			if !strings.Contains(msg, tc.reason) {
				t.Errorf("%s %v: want %q in:\nstdout=%q stderr=%q", name, mode, tc.reason, stdout, stderr)
			}
			if !tc.notRepo && strings.Contains(msg, "not a git repository") {
				t.Errorf("%s %v: labelled not a git repository:\n%s", name, mode, msg)
			}
		}
	}
}
