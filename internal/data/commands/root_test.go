package commands

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// runRootBoth drives a fresh command tree with stdout and stderr captured
// together, so a test can see help text wherever cobra chose to write it.
func runRootBoth(t *testing.T, args ...string) string {
	t.Helper()
	root := NewRootCmd()
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("%v: %v", args, err)
	}
	return buf.String()
}

// fakeExecutable points the home view at a known binary under a known home
// directory, so the "~" substitution is checked against a path the test
// chose rather than wherever the test binary happens to live.
func fakeExecutable(t *testing.T) (home string) {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	orig := executablePath
	executablePath = func() (string, error) {
		return filepath.Join(home, "go", "bin", "llm-data"), nil
	}
	t.Cleanup(func() { executablePath = orig })
	return home
}

// Defends AXI principle "content first": a bare `llm-data` used to print the
// whole cobra help page and exit 0, so an agent spent its first call reading a
// usage dump instead of learning what the tool is and where it is installed.
//
// Run outside any repository, so the view is bin, description and the one
// line saying there is no repository; home_test.go covers the live fields.
func TestRootNoArgsShowsHomeView(t *testing.T) {
	home := fakeExecutable(t)
	outsideAnyRepo(t)

	out := runRootBoth(t)

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("want a bin line, a description line and a repo line, got %d lines:\n%s", len(lines), out)
	}
	if want := "bin: ~/go/bin/llm-data"; lines[0] != want {
		t.Errorf("bin line = %q, want %q", lines[0], want)
	}
	if strings.Contains(out, home) {
		t.Errorf("home directory %q printed in full instead of as ~:\n%s", home, out)
	}
	if want := "description: " + homeDescription; lines[1] != want {
		t.Errorf("description line = %q, want %q", lines[1], want)
	}
	if strings.Count(homeDescription, ". ") != 0 || !strings.HasSuffix(homeDescription, ".") {
		t.Errorf("description is not one sentence: %q", homeDescription)
	}
	for _, helpMarker := range []string{"Usage:", "Available Commands", "Flags:"} {
		if strings.Contains(out, helpMarker) {
			t.Errorf("no-argument run printed help text (%q):\n%s", helpMarker, out)
		}
	}
}

// Defends the home view reaching the structured encodings: a view that only
// existed in plain text would hand a --json or --axi caller prose it asked
// not to get.
func TestRootNoArgsHomeViewEncodings(t *testing.T) {
	fakeExecutable(t)

	var got struct {
		Bin         string `json:"bin"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal([]byte(runRootBoth(t, "--json")), &got); err != nil {
		t.Fatalf("--json home view is not JSON: %v", err)
	}
	if got.Bin != "~/go/bin/llm-data" || got.Description != homeDescription {
		t.Errorf("--json home view = %+v", got)
	}

	axi := runRootBoth(t, "--axi")
	if !strings.Contains(axi, "bin: ~/go/bin/llm-data") || !strings.Contains(axi, "description: ") {
		t.Errorf("--axi home view missing bin or description:\n%s", axi)
	}
	if strings.HasPrefix(strings.TrimSpace(axi), "{") {
		t.Errorf("--axi home view is JSON, not TOON:\n%s", axi)
	}
}

// Defends --help: replacing the bare run must not take help away with it.
func TestRootHelpStillPrintsHelp(t *testing.T) {
	fakeExecutable(t)

	out := runRootBoth(t, "--help")

	for _, helpMarker := range []string{"Usage:", "Available Commands", "Flags:"} {
		if !strings.Contains(out, helpMarker) {
			t.Errorf("--help output missing %q:\n%s", helpMarker, out)
		}
	}
	if strings.Contains(out, "bin: ") {
		t.Errorf("--help printed the home view:\n%s", out)
	}
}

// Defends unknown subcommands: giving the root a Run makes cobra willing to
// treat leftover words as its arguments, which would turn a typo into a
// silent home view and exit 0. With --help as well, an Args validator on the
// root would print help and exit 0 instead.
func TestRootUnknownCommandStillFails(t *testing.T) {
	fakeExecutable(t)
	for _, args := range [][]string{{"no-such-command"}, {"no-such-command", "--help"}} {
		root := NewRootCmd()
		root.SetOut(new(bytes.Buffer))
		root.SetErr(new(bytes.Buffer))
		root.SetArgs(args)
		err := root.Execute()
		if err == nil || !strings.Contains(err.Error(), "unknown command") {
			t.Errorf("%v: want an unknown command error, got %v", args, err)
		}
	}
}

// Defends the "~" substitution against a prefix that is not a path boundary:
// /home/sam must not shorten /home/samx/bin to ~x/bin.
func TestTildeHome(t *testing.T) {
	cases := []struct{ path, home, want string }{
		{"/home/sam/go/bin/llm-data", "/home/sam", "~/go/bin/llm-data"},
		{"/home/sam", "/home/sam", "~"},
		{"/home/samx/bin/llm-data", "/home/sam", "/home/samx/bin/llm-data"},
		{"/usr/local/bin/llm-data", "/home/sam", "/usr/local/bin/llm-data"},
		{"/usr/local/bin/llm-data", "", "/usr/local/bin/llm-data"},
		{"/home/sam/bin/llm-data", "/home/sam/", "~/bin/llm-data"},
	}
	for _, c := range cases {
		if got := tildeHome(c.path, c.home); got != c.want {
			t.Errorf("tildeHome(%q, %q) = %q, want %q", c.path, c.home, got, c.want)
		}
	}
}

// Defends the home view against reporting success without its content: if
// the executable path cannot be resolved, the run must fail rather than print
// an empty bin line and exit 0.
func TestRootNoArgsFailsWhenExecutableUnresolved(t *testing.T) {
	orig := executablePath
	executablePath = func() (string, error) { return "", errors.New("no /proc") }
	t.Cleanup(func() { executablePath = orig })
	root := NewRootCmd()
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(nil)
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "cannot resolve executable path") {
		t.Errorf("want an executable path error, got %v", err)
	}
	if strings.Contains(buf.String(), "bin:") {
		t.Errorf("printed a home view despite the error:\n%s", buf.String())
	}
}

// Defends the per-invocation tree against pflag's Changed bit: PersistentPreRun
// reads Changed, so a bare run after an in-process --json run must print the
// plain view, not JSON.
func TestFreshTreeIgnoresPreviousChangedFlags(t *testing.T) {
	fakeExecutable(t)
	runRootBoth(t, "--json", "--min", "--axi")

	if out := runRootBoth(t); strings.HasPrefix(strings.TrimSpace(out), "{") || !strings.HasPrefix(out, "bin: ") {
		t.Errorf("bare run after --json is not the plain view:\n%s", out)
	}
}
