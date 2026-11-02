package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// The expected entries are spelled out here rather than read from the
// production variables, so a change to what an agent is told to do next is a
// deliberate edit in two places and not a silent one in one.

var wantHomeHelp = []string{
	"Run `llm-data --help` to list every command",
	"Run `llm-data <command> --help` for one command's flags",
}

const (
	helpIgnoreCase     = "Add -i to match case-insensitively"
	helpGrepGitignore  = "Add --no-gitignore to also search ignored and hidden files"
	helpGrepMultigrep  = "Run `llm-data multigrep --path <dir> --keywords <a,b>` to search several literal keywords at once"
	helpMultigrepExt   = "Drop --extensions to search every file type"
	helpMultigrepExcl  = "Add --no-exclude to also search node_modules and other default-excluded directories"
	helpMultigrepRegex = "Run `llm-data grep <regex> <path>` to search by regular expression"
)

// jsonHelp decodes a --json payload and returns its help list, failing the
// test if the payload is not JSON. present reports whether the key was there
// at all, so "no help" and "an empty help list" are told apart.
func jsonHelp(t *testing.T, out string) (help []string, present bool) {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	raw, ok := m["help"]
	if !ok {
		return nil, false
	}
	if err := json.Unmarshal(raw, &help); err != nil {
		t.Fatalf("help is not a list of strings: %v\n%s", err, raw)
	}
	return help, true
}

// assertAXIHelp checks a TOON payload carries a help block of exactly want.
func assertAXIHelp(t *testing.T, out string, want []string) {
	t.Helper()
	header := "help[" + strconv.Itoa(len(want)) + "]:"
	if !strings.Contains(out, header) {
		t.Errorf("--axi output has no %q block:\n%s", header, out)
	}
	for _, h := range want {
		if !strings.Contains(out, h) {
			t.Errorf("--axi help missing %q:\n%s", h, out)
		}
	}
	if strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("--axi output is JSON, not TOON:\n%s", out)
	}
}

// Defends AXI principle "contextual disclosure" on the home view: a bare run
// that says where the binary is but not what to do next leaves an agent to
// guess at --help.
func TestHomeViewCarriesHelp(t *testing.T) {
	fakeExecutable(t)

	help, ok := jsonHelp(t, runRootBoth(t, "--json"))
	if !ok || !reflect.DeepEqual(help, wantHomeHelp) {
		t.Errorf("--json home help = %q (present %v), want %q", help, ok, wantHomeHelp)
	}

	assertAXIHelp(t, runRootBoth(t, "--axi"), wantHomeHelp)
}

// Defends the plain home view staying two lines: help is for the structured
// encodings the task names, and TestRootNoArgsShowsHomeView pins the text form.
func TestHomeViewPlainTextHasNoHelp(t *testing.T) {
	fakeExecutable(t)
	if out := runRootBoth(t); strings.Contains(out, "help") {
		t.Errorf("plain home view printed help:\n%s", out)
	}
}

// grepFixture is a directory holding one file whose only content is "hello".
func grepFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// runOK runs llm-data in a child process, so each command's own flag globals
// start fresh, and fails the test on a non-zero exit.
func runOK(t *testing.T, args ...string) string {
	t.Helper()
	stdout, stderr, code := runGrepCLI(t, args...)
	if code != 0 {
		t.Fatalf("%v exited %d\nstdout:\n%s\nstderr:\n%s", args, code, stdout, stderr)
	}
	return stdout
}

// Defends a zero-match grep handing an agent a way forward: an empty result
// with nothing else in it reads as a dead end, when the likely causes are case,
// .gitignore filtering, or the wrong tool.
func TestGrepZeroMatchCarriesHelp(t *testing.T) {
	dir := grepFixture(t)
	want := []string{helpIgnoreCase, helpGrepGitignore, helpGrepMultigrep}

	help, ok := jsonHelp(t, runOK(t, "grep", "absent", dir, "--json"))
	if !ok || !reflect.DeepEqual(help, want) {
		t.Errorf("--json help = %q (present %v), want %q", help, ok, want)
	}
	assertAXIHelp(t, runOK(t, "grep", "absent", dir, "--axi"), want)
}

// Defends against suggesting a flag the caller already passed: telling an
// agent to add -i when it did is advice it cannot act on.
func TestGrepZeroMatchHelpOmitsFlagsAlreadyGiven(t *testing.T) {
	dir := grepFixture(t)
	want := []string{helpGrepMultigrep}

	help, _ := jsonHelp(t, runOK(t, "grep", "absent", dir, "-i", "--no-gitignore", "--json"))
	if !reflect.DeepEqual(help, want) {
		t.Errorf("--json help = %q, want %q", help, want)
	}
	assertAXIHelp(t, runOK(t, "grep", "absent", dir, "-i", "--no-gitignore", "--axi"), want)
}

// Defends against a misleading hint: .gitignore filtering only applies when
// grep walks a directory, so a file argument must not be told to drop it.
func TestGrepZeroMatchOnFileOmitsGitignoreHint(t *testing.T) {
	file := filepath.Join(grepFixture(t), "a.go")
	help, _ := jsonHelp(t, runOK(t, "grep", "absent", file, "--json"))
	if want := []string{helpIgnoreCase, helpGrepMultigrep}; !reflect.DeepEqual(help, want) {
		t.Errorf("--json help = %q, want %q", help, want)
	}
}

// Defends a run with matches staying clean: next steps there are noise an
// agent pays tokens to read, in either encoding and with -l as well.
func TestGrepWithMatchesCarriesNoHelp(t *testing.T) {
	dir := grepFixture(t)
	for _, extra := range [][]string{nil, {"-l"}} {
		args := append([]string{"grep", "hello", dir}, extra...)
		if help, ok := jsonHelp(t, runOK(t, append(args, "--json")...)); ok {
			t.Errorf("%v --json carried help %q", extra, help)
		}
		if out := runOK(t, append(args, "--axi")...); strings.Contains(out, "help") {
			t.Errorf("%v --axi carried help:\n%s", extra, out)
		}
	}
}

// Defends a zero-match multigrep handing an agent a way forward, for the
// same reason as grep: case, the extension filter and the default directory
// excludes are the usual reasons a keyword is not found.
func TestMultigrepZeroMatchCarriesHelp(t *testing.T) {
	dir := grepFixture(t)
	want := []string{helpIgnoreCase, helpMultigrepExt, helpMultigrepExcl, helpMultigrepRegex}
	args := []string{"multigrep", "--path", dir, "--keywords", "absent,missing", "--extensions", "go"}

	help, ok := jsonHelp(t, runOK(t, append(args, "--json")...))
	if !ok || !reflect.DeepEqual(help, want) {
		t.Errorf("--json help = %q (present %v), want %q", help, ok, want)
	}
	assertAXIHelp(t, runOK(t, append(args, "--axi")...), want)
}

// Defends --min: it renames multigrep's fields, and help must survive it.
func TestMultigrepZeroMatchHelpSurvivesMin(t *testing.T) {
	dir := grepFixture(t)
	help, _ := jsonHelp(t, runOK(t, "multigrep", "--path", dir, "--keywords", "absent", "-i", "--no-exclude", "--json", "--min"))
	if want := []string{helpMultigrepRegex}; !reflect.DeepEqual(help, want) {
		t.Errorf("--json --min help = %q, want %q", help, want)
	}
}

// Defends a multigrep run with matches staying clean, even when only some of
// its keywords matched: the run found something, so it is not a dead end.
func TestMultigrepWithMatchesCarriesNoHelp(t *testing.T) {
	dir := grepFixture(t)
	args := []string{"multigrep", "--path", dir, "--keywords", "hello,absent"}
	if help, ok := jsonHelp(t, runOK(t, append(args, "--json")...)); ok {
		t.Errorf("--json carried help %q", help)
	}
	if out := runOK(t, append(args, "--axi")...); strings.Contains(out, "help") {
		t.Errorf("--axi carried help:\n%s", out)
	}
}
