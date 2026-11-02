package commands

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// runRootBoth drives the real command tree with stdout and stderr captured
// together, so a test can see help text wherever cobra chose to write it.
func runRootBoth(t *testing.T, args ...string) string {
	t.Helper()
	buf := new(bytes.Buffer)
	RootCmd.SetOut(buf)
	RootCmd.SetErr(buf)
	RootCmd.SetArgs(args)
	t.Cleanup(func() {
		ResetOutputModes()
		RootCmd.SetOut(nil)
		RootCmd.SetErr(nil)
		RootCmd.SetArgs(nil)
		// cobra leaves --help set after a run that passed it, so the next
		// in-process run would print help again whatever its arguments.
		if f := RootCmd.Flags().Lookup("help"); f != nil {
			_ = f.Value.Set("false")
			f.Changed = false
		}
	})
	ResetOutputModes()
	if err := RootCmd.Execute(); err != nil {
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
func TestRootNoArgsShowsHomeView(t *testing.T) {
	home := fakeExecutable(t)

	out := runRootBoth(t)

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want exactly a bin line and a description line, got %d lines:\n%s", len(lines), out)
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
// silent home view and exit 0.
func TestRootUnknownCommandStillFails(t *testing.T) {
	fakeExecutable(t)
	RootCmd.SetOut(new(bytes.Buffer))
	RootCmd.SetErr(new(bytes.Buffer))
	RootCmd.SetArgs([]string{"no-such-command"})
	t.Cleanup(func() {
		RootCmd.SetOut(nil)
		RootCmd.SetErr(nil)
		RootCmd.SetArgs(nil)
	})
	err := RootCmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("want an unknown command error, got %v", err)
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
