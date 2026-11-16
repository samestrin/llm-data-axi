package commands

import (
	"encoding/json"
	"strings"
	"testing"
)

// errorFormat is one output mode an error must be reported in, with how to
// read the message back out of stdout in that mode.
type errorFormat struct {
	name  string
	flags []string
	// message returns the error message stdout carries, or ok=false when stdout
	// is not an error in this format.
	message func(stdout string) (msg string, ok bool)
}

var errorFormats = []errorFormat{
	{"default", nil, toonErrorMessage},
	{"axi", []string{"--axi"}, toonErrorMessage},
	{"json", []string{"--json"}, jsonErrorMessage},
	{"json-min", []string{"--json", "--min"}, jsonMinErrorMessage},
	{"plain", []string{"--plain"}, func(s string) (string, bool) {
		return strings.CutPrefix(s, "Error: ")
	}},
	{"plain-min", []string{"--plain", "--min"}, func(s string) (string, bool) {
		return s, s != "" && !strings.HasPrefix(s, "Error: ")
	}},
}

func toonErrorMessage(s string) (string, bool) {
	return strings.CutPrefix(s, "error: true\n")
}

func jsonErrorMessage(s string) (string, bool) {
	var got struct {
		Error   bool   `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal([]byte(s), &got) != nil || !got.Error {
		return "", false
	}
	return got.Message, true
}

func jsonMinErrorMessage(s string) (string, bool) {
	var got struct {
		Err bool   `json:"err"`
		Msg string `json:"msg"`
	}
	if json.Unmarshal([]byte(s), &got) != nil || !got.Err {
		return "", false
	}
	return got.Msg, true
}

// errorCase is one way a run fails. args builds the argument list around the
// format flags, so the unknown flag can sit before or after them.
type errorCase struct {
	name string
	args func(format []string) []string
	code int
	want string
}

var errorCases = []errorCase{
	{"unknown flag before format", func(f []string) []string {
		return append([]string{"grep", "--nope"}, f...)
	}, 2, "unknown flag: --nope"},
	{"unknown flag after format", func(f []string) []string {
		return append(append([]string{"grep"}, f...), "--nope")
	}, 2, "unknown flag: --nope"},
	{"format before command, unknown flag after", func(f []string) []string {
		return append(append([]string{}, f...), "grep", "--nope")
	}, 2, "unknown flag: --nope"},
	{"unknown command", func(f []string) []string {
		return append([]string{"no-such-command"}, f...)
	}, 2, `unknown command "no-such-command"`},
	{"runtime error", func(f []string) []string {
		return append([]string{"grep", "foo", "/no/such/path/llm-data"}, f...)
	}, 1, "path not found"},
}

// Defends the epic 11 T2 contract: an error is printed on stdout in the format
// the run asked for, whichever format that is, and stderr stays empty. Plain
// mode used to print its "Error:" line on stderr, so an agent reading stdout
// under --plain saw nothing at all and had only the exit code to go on. Exit
// codes are pinned alongside: 2 for a malformed invocation, 1 for a failure.
//
// Each case runs twice: in process through run, and in a child process
// through Execute with the real os.Stdout. The child matters because the
// formatter used to swap os.Stdout for os.Stderr in text mode, which a buffer
// passed to run never triggers.
func TestErrorsGoToStdoutInEveryFormat(t *testing.T) {
	for _, f := range errorFormats {
		for _, c := range errorCases {
			args := c.args(f.flags)
			t.Run(f.name+"/"+c.name, func(t *testing.T) {
				code, stdout, stderr := runCLI(t, args...)
				checkErrorOnStdout(t, "run", f, c, code, stdout, stderr)

				stdout, stderr, code = runGrepCLI(t, args...)
				checkErrorOnStdout(t, "Execute", f, c, code, stdout, stderr)
			})
		}
	}
}

func checkErrorOnStdout(t *testing.T, via string, f errorFormat, c errorCase, code int, stdout, stderr string) {
	t.Helper()
	if code != c.code {
		t.Errorf("%s: exit %d, want %d", via, code, c.code)
	}
	if stderr != "" {
		t.Errorf("%s: stderr must be empty, got %q", via, stderr)
	}
	msg, ok := f.message(stdout)
	if !ok {
		t.Errorf("%s: stdout is not a %s error: %q", via, f.name, stdout)
		return
	}
	// TOON quotes the message and escapes its inner quotes.
	want := c.want
	if f.name == "default" || f.name == "axi" {
		want = strings.ReplaceAll(want, `"`, `\"`)
	}
	if !strings.Contains(msg, want) {
		t.Errorf("%s: error must say %q, got %q", via, c.want, msg)
	}
}
