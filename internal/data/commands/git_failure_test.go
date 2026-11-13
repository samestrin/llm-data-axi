package commands

import (
	"bytes"
	"fmt"
	"os"
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
	if !strings.Contains(out, "ERROR: not a git repository") {
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
