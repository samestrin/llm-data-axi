package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// gitDirTree is a directory with a source file and a .git directory holding
// text and binary files, as a real repository does.
func gitDirTree(t *testing.T) string {
	t.Helper()
	dir := realTempDir(t)
	files := map[string]string{
		"main.go":           "package main // needle\n",
		".git/config":       "[core] needle\n",
		".git/index":        "DIRC\x00\x00needle",
		".git/refs/heads/x": "needle\n",
	}
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// Defends grep and catfiles against walking .git: a search of a repository
// listed .git/index and every pack in binary_skipped and matched git's own
// files. .git is skipped unless --no-gitignore asks for ignored files too.
func TestWalksSkipTheGitDirectoryByDefault(t *testing.T) {
	dir := gitDirTree(t)

	code, stdout, _ := runCLI(t, "grep", "needle", dir, "--json")
	if code != 0 {
		t.Fatalf("grep exit %d: %s", code, stdout)
	}
	var g GrepResult
	if err := json.Unmarshal([]byte(stdout), &g); err != nil {
		t.Fatalf("%v: %s", err, stdout)
	}
	if g.TotalMatches != 1 || len(g.BinarySkipped) != 0 || strings.Contains(stdout, ".git") {
		t.Errorf("grep searched .git: %s", stdout)
	}

	code, stdout, _ = runCLI(t, "grep", "needle", dir, "--json", "--no-gitignore")
	if code != 0 || !strings.Contains(stdout, ".git") {
		t.Errorf("grep --no-gitignore must search .git too, exit %d: %s", code, stdout)
	}

	code, stdout, _ = runCLI(t, "catfiles", dir, "--json")
	if code != 0 {
		t.Fatalf("catfiles exit %d: %s", code, stdout)
	}
	var c CatfilesResult
	if err := json.Unmarshal([]byte(stdout), &c); err != nil {
		t.Fatalf("%v: %s", err, stdout)
	}
	if c.FileCount != 1 || strings.Contains(stdout, ".git") {
		t.Errorf("catfiles read .git: %s", truncForMsg(stdout))
	}

	// Naming the .git directory itself is asking for it.
	code, stdout, _ = runCLI(t, "grep", "needle", filepath.Join(dir, ".git"), "--json")
	if code != 0 || !strings.Contains(stdout, "config") {
		t.Errorf("grep on .git itself must search it, exit %d: %s", code, stdout)
	}
}
