package commands

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/samestrin/llm-data-axi/pkg/output"
)

// runCLI drives run exactly as main does, with both streams captured apart so
// a test can say which one an error landed on.
func runCLI(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	t.Cleanup(func() {
		GlobalAXIOutput, GlobalJSONOutput, GlobalMinOutput = false, false, false
		output.SetDefaultAXI(false)
	})
	var out, errOut bytes.Buffer
	code = run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

// grepFlags are flags grep accepts, local and inherited. The unknown-flag
// error must list them so a caller can correct the call without a --help
// round trip.
var grepFlags = []string{"--ignore-case", "--line-number", "--files-with-matches", "--full", "--axi", "--json"}

// Defends the AXI usage-error contract: flag parsing failed before
// PersistentPreRun published the mode, so `grep --nope --axi` printed a plain
// "Error:" line on stderr and an agent reading stdout as TOON saw nothing at
// all. The mode flag is read in every position relative to the bad one.
func TestUnknownFlagUnderAXIIsTOONOnStdout(t *testing.T) {
	for _, args := range [][]string{
		{"grep", "--nope", "--axi"},
		{"grep", "--axi", "--nope"},
		{"--axi", "grep", "--nope"},
	} {
		code, stdout, stderr := runCLI(t, args...)
		if code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
		if stderr != "" {
			t.Errorf("%v: stderr must be empty, got %q", args, stderr)
		}
		if !strings.HasPrefix(stdout, "error: true\n") {
			t.Errorf("%v: want a TOON error on stdout, got %q", args, stdout)
		}
		if !strings.Contains(stdout, "unknown flag: --nope") {
			t.Errorf("%v: error must name --nope, got %q", args, stdout)
		}
		for _, f := range grepFlags {
			if !strings.Contains(stdout, f) {
				t.Errorf("%v: error must list grep's flag %s, got %q", args, f, stdout)
			}
		}
	}
}

// Defends the same contract under --json. grep declares its own local --json
// that shadows the persistent one, so a fix that only read GlobalJSONOutput
// would still print plain text here.
func TestUnknownFlagUnderJSONIsJSONOnStdout(t *testing.T) {
	for _, args := range [][]string{
		{"grep", "--nope", "--json"},
		{"--json", "grep", "--nope"},
	} {
		code, stdout, stderr := runCLI(t, args...)
		if code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
		if stderr != "" {
			t.Errorf("%v: stderr must be empty, got %q", args, stderr)
		}
		var got struct {
			Error   bool   `json:"error"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal([]byte(stdout), &got); err != nil {
			t.Fatalf("%v: stdout is not JSON: %v\n%s", args, err, stdout)
		}
		if !got.Error || !strings.Contains(got.Message, "unknown flag: --nope") {
			t.Errorf("%v: want an error naming --nope, got %+v", args, got)
		}
		for _, f := range grepFlags {
			if !strings.Contains(got.Message, f) {
				t.Errorf("%v: message must list grep's flag %s, got %q", args, f, got.Message)
			}
		}
	}
}

// Pins plain mode: with no structured flag a usage error stays on stderr,
// stdout stays empty, and the exit code is still 2.
func TestUnknownFlagPlainStaysOnStderr(t *testing.T) {
	code, stdout, stderr := runCLI(t, "grep", "--nope")
	if code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	if stdout != "" {
		t.Errorf("stdout must be empty in plain mode, got %q", stdout)
	}
	if !strings.HasPrefix(stderr, "Error: unknown flag: --nope") {
		t.Errorf("want the plain error on stderr, got %q", stderr)
	}
	if !strings.Contains(stderr, "--ignore-case") {
		t.Errorf("plain error must list grep's flags too, got %q", stderr)
	}
}

// Defends an unknown shorthand: its error is not "unknown flag: --", and it
// is the case where listing the shorthands is what helps.
func TestUnknownShorthandListsFlagsUnderAXI(t *testing.T) {
	code, stdout, stderr := runCLI(t, "grep", "-Z", "--axi")
	if code != 2 || stderr != "" {
		t.Errorf("exit %d stderr %q, want 2 and empty", code, stderr)
	}
	if !strings.Contains(stdout, "unknown shorthand flag: 'Z'") || !strings.Contains(stdout, "-i") {
		t.Errorf("want the shorthand named and grep's flags listed, got %q", stdout)
	}
}

// Defends usage errors that never reach a command's flag parsing: an unknown
// command fails in cobra's Find, before any flag is parsed at all.
func TestUnknownCommandUnderAXIIsTOONOnStdout(t *testing.T) {
	code, stdout, stderr := runCLI(t, "no-such-command", "--axi")
	if code != 2 || stderr != "" {
		t.Errorf("exit %d stderr %q, want 2 and empty", code, stderr)
	}
	if !strings.HasPrefix(stdout, "error: true\n") || !strings.Contains(stdout, `unknown command \"no-such-command\"`) {
		t.Errorf("want a TOON unknown-command error, got %q", stdout)
	}
}

// Defends the exit-code split: a runtime failure is still 1, not swept into
// 2 by the usage-error path, and still structured on stdout.
func TestRuntimeErrorUnderAXIExitsOne(t *testing.T) {
	code, stdout, stderr := runCLI(t, "grep", "foo", "/no/such/path/llm-data", "--axi")
	if code != 1 || stderr != "" {
		t.Errorf("exit %d stderr %q, want 1 and empty", code, stderr)
	}
	if !strings.Contains(stdout, "path not found") {
		t.Errorf("want the runtime error on stdout, got %q", stdout)
	}
}

// Defends against reading a flag VALUE as a mode flag: here "--axi" is the
// pattern after the terminator, not a request for TOON.
func TestModeFlagAfterTerminatorIsNotAMode(t *testing.T) {
	code, stdout, stderr := runCLI(t, "grep", "--nope", "--", "--axi")
	if code != 2 || stdout != "" {
		t.Errorf("exit %d stdout %q, want 2 and empty", code, stdout)
	}
	if !strings.HasPrefix(stderr, "Error: unknown flag: --nope") {
		t.Errorf("want the plain error on stderr, got %q", stderr)
	}
}

// Defends the guard in flagError: only an unknown flag gets the flag list. A
// missing value already names the flag the caller used, so its cobra wording
// must pass through unchanged, still structured on stdout and still exit 2.
func TestMissingFlagValueKeepsCobraMessage(t *testing.T) {
	code, stdout, stderr := runCLI(t, "yaml", "get", "--axi", "--file")
	if code != 2 || stderr != "" {
		t.Errorf("exit %d stderr %q, want 2 and empty", code, stderr)
	}
	if stdout != "error: true\nmessage: \"flag needs an argument: --file\"\n" {
		t.Errorf("want cobra's message unchanged in TOON, got %q", stdout)
	}
}

// Defends the hidden-flag skip in flagError: the root's -V is bound through a
// hidden "version-upper" flag, an implementation detail that must not be
// offered to a caller as a flag to type.
func TestUnknownFlagListOmitsHiddenFlags(t *testing.T) {
	code, stdout, _ := runCLI(t, "--nope", "--axi")
	if code != 2 || !strings.Contains(stdout, "-v/--version") {
		t.Fatalf("exit %d, want 2 and the root's flags listed, got %q", code, stdout)
	}
	if strings.Contains(stdout, "version-upper") {
		t.Errorf("hidden flag listed: %q", stdout)
	}
}
