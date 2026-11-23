package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// versionedFake is a fake llm-data that answers --version too.
func versionedFake(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "llm-data")
	script := "#!/bin/sh\n[ \"$1\" = --version ] && { echo 'llm-data version 9.9.9'; exit 0; }\nfor a in \"$@\"; do [ \"$a\" = --axi ] && { echo 'a: 1'; exit 0; }; done\necho '{\"a\": 1}'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

// gitRepo makes a repository with one commit and returns it and the commit.
func gitRepo(t *testing.T) (dir, commit string) {
	t.Helper()
	dir = t.TempDir()
	git := func(args ...string) string {
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-q", "-m", "one")
	return dir, git("rev-parse", "HEAD")
}

// Defends RESULTS.md as a reproducible record: the committed table was made
// from an uncommitted tree before v2.0.1 and could not be reproduced at any
// commit, because the file never said which binary or which commit produced
// it. The report names the binary's version and each repository's commit.
func TestReportNamesTheBinaryVersionAndEachRepositoryCommit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as the binary")
	}
	repo, commit := gitRepo(t)
	plain := t.TempDir()
	out := filepath.Join(t.TempDir(), "RESULTS.md")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-bin", versionedFake(t), "-o", out, repo, plain}, &stdout, &stderr); code != 0 {
		t.Fatalf("run exited %d: %s", code, stderr.String())
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{"Measured with `llm-data version 9.9.9`", "at commit " + commit, "not a git checkout"} {
		if !strings.Contains(s, want) {
			t.Errorf("report does not contain %q:\n%s", want, s)
		}
	}
}

// Defends against a table nobody can reproduce: with uncommitted changes in
// a measured repository the commit names a tree that was not measured, so
// writing the report is refused and nothing is written.
func TestReportIsRefusedFromACheckoutWithUncommittedChanges(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as the binary")
	}
	repo, _ := gitRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "b.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "RESULTS.md")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-bin", versionedFake(t), "-o", out, repo, t.TempDir()}, &stdout, &stderr); code == 0 {
		t.Fatal("run wrote a report from a checkout with uncommitted changes")
	}
	if !strings.Contains(stderr.String(), "uncommitted") {
		t.Errorf("stderr does not say why: %q", stderr.String())
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("report written anyway (stat err %v)", err)
	}
}

// Defends the committed RESULTS.md: it names the binary and the commit of
// this repository it was measured at.
func TestCommittedResultsNameTheBinaryAndTheCommit(t *testing.T) {
	b, err := os.ReadFile("RESULTS.md")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, "Measured with `llm-data") || !strings.Contains(s, "- llm-data-axi at commit ") {
		t.Errorf("RESULTS.md does not name the binary version and the llm-data-axi commit")
	}
}
