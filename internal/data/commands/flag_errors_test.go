package commands

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// invalidValueCases are bad values for typed flags, one per parser the
// commands use, with the message the caller must get instead of cobra's
// `invalid argument ... strconv.ParseInt: parsing ...`.
var invalidValueCases = []struct {
	name string
	args []string
	want string
}{
	{"int64", []string{"runtime", "--start", "abc"}, `invalid value "abc" for --start: want an integer`},
	{"int", []string{"tree", "--depth", "1.5"}, `invalid value "1.5" for --depth: want an integer`},
	{"int empty", []string{"tree", "--depth="}, `invalid value "" for --depth: want an integer`},
	{"int out of range", []string{"runtime", "--start", "99999999999999999999"}, `invalid value "99999999999999999999" for --start: integer out of range`},
	{"bool", []string{"grep", "--ignore-case=maybe", "x", "."}, `invalid value "maybe" for --ignore-case: want true or false`},
	{"bool shorthand", []string{"grep", "-i=maybe", "x", "."}, `invalid value "maybe" for --ignore-case: want true or false`},
	// A custom value's own reason is already written for a person, so it stays.
	// The known list is read from setupTargets, so adding a target does not
	// break this case.
	{"custom value", []string{"setup", "--target", "nope", "--dry-run"}, `invalid value "nope" for --target: unknown target "nope" (known: ` + strings.Join(setupTargetNames(), ", ") + `)`},
}

// Defends EST-6633 item 1: a bad flag VALUE came back as
// `invalid argument "abc" for "--start" flag: strconv.ParseInt: parsing "abc":
// invalid syntax`, a Go parser's name where the caller needed what the flag
// takes. It must still be a usage error on stdout in the run's format, exit
// 2, naming the flag and the value.
func TestInvalidFlagValueNamesFlagAndValue(t *testing.T) {
	for _, f := range errorFormats {
		for _, c := range invalidValueCases {
			args := append(append([]string{}, c.args...), f.flags...)
			t.Run(f.name+"/"+c.name, func(t *testing.T) {
				code, stdout, stderr := runCLI(t, args...)
				checkErrorOnStdout(t, "run", f, errorCase{name: c.name, code: 2, want: c.want}, code, stdout, stderr)
				for _, internal := range []string{"strconv", "ParseInt", "ParseBool", "invalid syntax"} {
					if strings.Contains(stdout, internal) {
						t.Errorf("message leaks Go internals (%q): %q", internal, stdout)
					}
				}
			})
		}
	}
}

// Defends the same rewrite for a duration flag, which no command has yet: the
// rewrite is keyed on the flag's type, so the next duration flag must not
// bring `time: invalid duration` back.
func TestInvalidDurationFlagValue(t *testing.T) {
	cmd := &cobra.Command{Use: "probe"}
	cmd.Flags().Duration("wait", time.Second, "")
	cmd.Flags().Float64("ratio", 1, "")
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"--wait", "soon"}, `invalid value "soon" for --wait: want a duration such as 30s or 5m`},
		{[]string{"--ratio", "half"}, `invalid value "half" for --ratio: want a number`},
	} {
		err := flagError(cmd, cmd.ParseFlags(c.args))
		if err == nil || err.Error() != c.want {
			t.Errorf("%v: got %v, want %q", c.args, err, c.want)
		}
	}
}

// Defends EST-6633 item 2 (TD 9da97c6d23158c65): the valid flags were only
// prose inside the message, so a caller had to split a sentence to read them.
// They are now a valid_flags list next to the message in TOON and JSON, and
// the message keeps the list for a person reading it.
func TestUnknownFlagValidFlagsIsData(t *testing.T) {
	for _, format := range [][]string{{"--json"}, {"--json", "--min"}} {
		code, stdout, _ := runCLI(t, append([]string{"grep", "--nope"}, format...)...)
		if code != 2 {
			t.Errorf("%v: exit %d, want 2", format, code)
		}
		var got struct {
			Message    string   `json:"message"`
			Msg        string   `json:"msg"`
			ValidFlags []string `json:"valid_flags"`
		}
		if err := json.Unmarshal([]byte(stdout), &got); err != nil {
			t.Fatalf("%v: not JSON: %v\n%s", format, err, stdout)
		}
		for _, want := range []string{"-i/--ignore-case", "--full", "--json", "--axi", "-h/--help"} {
			if !slices.Contains(got.ValidFlags, want) {
				t.Errorf("%v: valid_flags %v is missing %q", format, got.ValidFlags, want)
			}
		}
		if msg := got.Message + got.Msg; !strings.Contains(msg, "valid flags for llm-data grep: ") {
			t.Errorf("%v: message lost its valid flags: %q", format, msg)
		}
	}

	for _, format := range [][]string{nil, {"--axi"}} {
		_, stdout, _ := runCLI(t, append([]string{"grep", "--nope"}, format...)...)
		if !strings.Contains(stdout, "\nvalid_flags[") || !strings.Contains(stdout, "-i/--ignore-case") {
			t.Errorf("%v: TOON error has no valid_flags list:\n%s", format, stdout)
		}
	}

	// Plain text has no fields; the message carries the list and nothing else
	// is printed.
	_, stdout, _ := runCLI(t, "grep", "--nope", "--plain")
	if strings.Contains(stdout, "valid_flags") || !strings.Contains(stdout, "valid flags for llm-data grep: ") {
		t.Errorf("--plain error: %q", stdout)
	}
}

// Defends the hidden-flag skip in flagError (TD 68ea2020ae7bf987): no command
// had a hidden flag, so nothing showed a hidden one stays out of both the
// message and valid_flags.
func TestUnknownFlagOmitsHiddenFlags(t *testing.T) {
	cmd := &cobra.Command{Use: "probe"}
	cmd.Flags().Bool("shown", false, "")
	cmd.Flags().Bool("secret", false, "")
	_ = cmd.Flags().MarkHidden("secret")

	err := flagError(cmd, cmd.ParseFlags([]string{"--nope"}))
	var fe *unknownFlagError
	if !errors.As(err, &fe) {
		t.Fatalf("want an unknownFlagError, got %T: %v", err, err)
	}
	if !slices.Contains(fe.valid, "--shown") {
		t.Errorf("valid_flags %v is missing --shown", fe.valid)
	}
	if slices.Contains(fe.valid, "--secret") || strings.Contains(err.Error(), "secret") {
		t.Errorf("hidden flag listed: %v / %q", fe.valid, err.Error())
	}
}

// Defends TD 68ea2020ae7bf987: -V needed a long name, so the hidden
// --version-upper was an accepted spelling nothing documented. -V still prints
// the version; --version-upper is an unknown flag; and -V under a subcommand,
// where -v is unknown too, is still rejected.
func TestVersionUpperIsNotASpelling(t *testing.T) {
	stampVersion(t, "1.1.0")
	for _, args := range [][]string{{"-V"}, {"--json", "-V"}, {"-V", "--plain"}} {
		code, stdout, _ := runCLI(t, args...)
		if code != 0 || stdout != "1.1.0\n" {
			t.Errorf("%v: exit %d, stdout %q; want the version", args, code, stdout)
		}
	}
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"--version-upper"}, "unknown flag: --version-upper"},
		{[]string{"--version-upper=true"}, "unknown flag: --version-upper"},
		{[]string{"grep", "-V", "x", "."}, "unknown shorthand flag: 'V' in -V"},
	} {
		code, stdout, _ := runCLI(t, append(c.args, "--json")...)
		if code != 2 || !strings.Contains(stdout, c.want) {
			t.Errorf("%v: exit %d, stdout %q; want exit 2 and %q", c.args, code, stdout, c.want)
		}
	}
	// A -V after "--" is an argument, not a flag, and is left alone.
	if got := versionShorthand(NewRootCmd(), []string{"math", "--", "-V"}); !slices.Equal(got, []string{"math", "--", "-V"}) {
		t.Errorf("rewrote a subcommand's argument: %v", got)
	}
	if got := versionShorthand(NewRootCmd(), []string{"--", "-V"}); !slices.Equal(got, []string{"--", "-V"}) {
		t.Errorf("rewrote -V after --: %v", got)
	}
}
