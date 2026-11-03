package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeRunner answers by output format, so a test controls both documents
// without exec'ing anything.
func fakeRunner(toon, json string, err error) Runner {
	return func(dir string, args []string) ([]byte, error) {
		if err != nil {
			return nil, err
		}
		for _, a := range args {
			if a == "--axi" {
				return []byte(toon), nil
			}
		}
		return []byte(json), nil
	}
}

var oneRepo = []Repo{{Name: "r", Dir: "."}}
var oneCmd = []Command{{Name: "stats", Args: []string{"stats", "--path", "."}}}

// Defends the core of the task: each row carries the bytes of both formats
// and tokens as bytes/4, measured on the same command.
func TestMeasureCountsBytesAndTokensPerFormat(t *testing.T) {
	rows, err := measure(fakeRunner("abcdefgh", "abcdefghijklmnop", nil), oneRepo, oneCmd)
	if err != nil {
		t.Fatalf("measure: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	r := rows[0]
	if r.TOONBytes != 8 || r.JSONBytes != 16 {
		t.Fatalf("bytes = toon %d json %d, want 8 and 16", r.TOONBytes, r.JSONBytes)
	}
	if r.TOONTokens() != 2 || r.JSONTokens() != 4 {
		t.Fatalf("tokens = toon %d json %d, want 2 and 4", r.TOONTokens(), r.JSONTokens())
	}
}

// Defends against both formats being asked for in one run: the runner must
// see --axi on one call and --json on the other, never both.
func TestMeasurePassesOneFormatFlagPerRun(t *testing.T) {
	var calls [][]string
	run := func(dir string, args []string) ([]byte, error) {
		calls = append(calls, args)
		return []byte(strings.Join(args, " ")), nil
	}
	if _, err := measure(run, oneRepo, oneCmd); err != nil {
		t.Fatalf("measure: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("got %d calls, want 2", len(calls))
	}
	var sawAXI, sawJSON bool
	for _, c := range calls {
		j := strings.Join(c, " ")
		hasAXI, hasJSON := strings.Contains(j, "--axi"), strings.Contains(j, "--json")
		if hasAXI == hasJSON {
			t.Fatalf("call %q must carry exactly one of --axi and --json", j)
		}
		sawAXI = sawAXI || hasAXI
		sawJSON = sawJSON || hasJSON
	}
	if !sawAXI || !sawJSON {
		t.Fatalf("want one --axi and one --json call, got %v", calls)
	}
}

// Defends the "Done when": a command that fails yields an error, not a row.
func TestMeasureFailsOnCommandError(t *testing.T) {
	// Only --axi fails, so a run that drops the TOON error and goes on to
	// measure JSON cannot pass.
	failAXI := func(dir string, args []string) ([]byte, error) {
		if args[len(args)-1] == "--axi" {
			return nil, errors.New("exit status 1")
		}
		return []byte("{}"), nil
	}
	rows, err := measure(failAXI, oneRepo, oneCmd)
	if err == nil {
		t.Fatal("measure returned no error for a failing command")
	}
	if rows != nil {
		t.Fatalf("measure returned rows alongside an error: %v", rows)
	}
}

// Defends against an empty document being measured as a 0-byte win.
func TestMeasureFailsOnEmptyOutput(t *testing.T) {
	if _, err := measure(fakeRunner("", "{}", nil), oneRepo, oneCmd); err == nil {
		t.Fatal("measure accepted an empty TOON document")
	}
	if _, err := measure(fakeRunner("a: 1", "  \n", nil), oneRepo, oneCmd); err == nil {
		t.Fatal("measure accepted a blank JSON document")
	}
}

// Defends against the bug this repo already shipped once: --axi answered
// with JSON. Identical documents would read as a 0% saving, not a failure.
func TestMeasureFailsWhenAXIReturnsTheJSONDocument(t *testing.T) {
	_, err := measure(fakeRunner(`{"a":1}`, `{"a":1}`, nil), oneRepo, oneCmd)
	if err == nil || !strings.Contains(err.Error(), "--axi") {
		t.Fatalf("want an error naming --axi, got %v", err)
	}
}

// Defends the table shape RESULTS.md promises: a bytes and a tokens column
// for each format, one row per repo and command, and a total.
func TestRenderTable(t *testing.T) {
	rows := []Row{
		{Repo: "a", Command: "stats", TOONBytes: 40, JSONBytes: 100},
		{Repo: "b", Command: "tree", TOONBytes: 60, JSONBytes: 100},
	}
	got := renderTable(rows)
	want := tableHeader +
		"| a | `stats` | 100 | 25 | 40 | 10 | 60.0% |\n" +
		"| b | `tree` | 100 | 25 | 60 | 15 | 40.0% |\n" +
		"| **total** |  | 200 | 50 | 100 | 25 | 50.0% |\n"
	if got != want {
		t.Fatalf("table mismatch\n got:\n%s\nwant:\n%s", got, want)
	}
}

// Defends the "Done when" that a failing command exits non-zero and prints
// no row, through the real exec path with a binary that always fails.
func TestRunExitsNonZeroWhenTheBinaryFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as the binary")
	}
	bin := filepath.Join(t.TempDir(), "llm-data")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho boom >&2\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := run([]string{"-bin", bin, t.TempDir(), t.TempDir()}, &out, &errOut)
	if code == 0 {
		t.Fatal("run exited 0 for a failing command")
	}
	if strings.Contains(out.String(), "|") {
		t.Fatalf("run printed a table row for a failing command:\n%s", out.String())
	}
	if !strings.Contains(errOut.String(), "boom") {
		t.Fatalf("stderr does not carry the command's own error: %q", errOut.String())
	}
}

// Defends the happy path through exec: a binary answering both formats
// produces the table on stdout and writes it to -o.
func TestRunPrintsAndWritesTheTable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as the binary")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "llm-data")
	script := "#!/bin/sh\nfor a in \"$@\"; do [ \"$a\" = --axi ] && { echo 'a: 1'; exit 0; }; done\necho '{\"a\": 1}'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	outFile := filepath.Join(dir, "RESULTS.md")
	var out, errOut bytes.Buffer
	if code := run([]string{"-bin", bin, "-o", outFile, dir, dir}, &out, &errOut); code != 0 {
		t.Fatalf("run exited %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), tableHeader) {
		t.Fatalf("stdout has no table:\n%s", out.String())
	}
	if n := strings.Count(out.String(), "| `"); n != 2*len(commands) {
		t.Fatalf("got %d command rows, want %d", n, 2*len(commands))
	}
	written, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("-o file not written: %v", err)
	}
	if !strings.Contains(string(written), out.String()) {
		t.Fatal("-o file does not hold the printed table")
	}
}

// Defends against a single-repo run passing as the benchmark the task asks for.
// The binary is one that would succeed, so only the repo count can fail it.
func TestRunRefusesFewerThanTwoRepos(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as the binary")
	}
	bin := filepath.Join(t.TempDir(), "llm-data")
	script := "#!/bin/sh\nfor a in \"$@\"; do [ \"$a\" = --axi ] && { echo 'a: 1'; exit 0; }; done\necho '{\"a\": 1}'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"-bin", bin, t.TempDir()}, &out, &errOut); code == 0 {
		t.Fatal("run accepted one repository")
	}
	if !strings.Contains(errOut.String(), "at least two") {
		t.Fatalf("stderr does not say why: %q", errOut.String())
	}
}

// Defends the committed result against drifting from the program: RESULTS.md
// must carry the table the program renders, header included.
func TestCommittedResultsCarryTheTable(t *testing.T) {
	b, err := os.ReadFile("RESULTS.md")
	if err != nil {
		t.Fatalf("RESULTS.md: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, tableHeader) {
		t.Fatal("RESULTS.md does not carry the table header")
	}
	// tree, stats, grep and multigrep print absolute paths, so their byte
	// counts depend on the checkout location; the report must say so.
	if !strings.Contains(s, "`tree`, `stats`, `grep` and `multigrep` print absolute paths") {
		t.Fatal("RESULTS.md does not name the commands whose bytes depend on the checkout path")
	}
	for _, c := range commands {
		if !strings.Contains(s, "| `"+c.Name+"` |") {
			t.Fatalf("RESULTS.md has no row for %s", c.Name)
		}
	}
}

// Defends the whole path against the real binary: with no -bin the program
// builds ./cmd/llm-data from the module root (the test runs in benchmarks/,
// so a cwd-relative build would fail here) and every command answers both
// formats with different, non-empty documents.
func TestRunBuildsAndMeasuresTheRealBinary(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{t.TempDir(), t.TempDir()}, &out, &errOut); code != 0 {
		t.Fatalf("run exited %d: %s", code, errOut.String())
	}
	if n := strings.Count(out.String(), "| `"); n != 2*len(commands) {
		t.Fatalf("got %d command rows, want %d:\n%s", n, 2*len(commands), out.String())
	}
}

// Defends the default repositories: this module and the cobra version go.mod
// pins, both resolved to directories that exist.
func TestDefaultRepos(t *testing.T) {
	repos, err := defaultRepos()
	if err != nil {
		t.Fatalf("defaultRepos: %v", err)
	}
	if len(repos) != 2 || repos[0].Name != "llm-data-axi" || repos[1].Name != "spf13/cobra@v1.8.0" {
		t.Fatalf("unexpected repos: %+v", repos)
	}
	for _, r := range repos {
		if fi, err := os.Stat(r.Dir); err != nil || !fi.IsDir() {
			t.Fatalf("%s: %s is not a directory", r.Name, r.Dir)
		}
	}
}

// Defends against a mistyped repo path being measured as an empty repository.
func TestRunRejectsAMissingRepoDir(t *testing.T) {
	var out, errOut bytes.Buffer
	missing := filepath.Join(t.TempDir(), "nope")
	if code := run([]string{"-bin", "/nonexistent", t.TempDir(), missing}, &out, &errOut); code == 0 {
		t.Fatal("run accepted a missing directory")
	}
	if !strings.Contains(errOut.String(), "not a directory") {
		t.Fatalf("stderr does not name the bad path: %q", errOut.String())
	}
}

// Defends against an unknown flag being ignored.
func TestRunRejectsAnUnknownFlag(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"-nope"}, &out, &errOut); code == 0 {
		t.Fatal("run accepted an unknown flag")
	}
}
