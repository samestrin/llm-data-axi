package commands

import (
	"os"
	"os/exec"
	"testing"
)

// gitAt runs a git command in dir with a fixed author/committer date.
//
// Ported from knowledge_audit_drift_test.go, which does not travel to this
// repo. Several tests here would not compile without it.
func gitAt(t *testing.T, dir, date string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(),
		"GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date,
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
	)
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// contains reports whether slice holds item.
//
// Ported from partition_work_test.go, which does not travel to this repo.
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
