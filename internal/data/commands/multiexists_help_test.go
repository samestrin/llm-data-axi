package commands

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The expected steps are spelled out here rather than read from the
// production values, so a change to what an agent is told to do next is a
// deliberate edit in two places and not a silent one in one.
var wantMultiexistsMissingHelp = []string{
	"Check the spelling and case of each missing path",
	"Relative paths resolve from the working directory; pass an absolute path or run from the directory that holds them",
	"Run `llm-data tree --path <dir>` to list what a directory holds",
}

const multiexistsNoFailHint = "Add --no-fail to report missing paths as a result with exit 0"

// multiexistsHelpFixture is a directory holding one file, plus the path of a
// file that is not there.
func multiexistsHelpFixture(t *testing.T) (present, missing string) {
	t.Helper()
	dir := t.TempDir()
	present = filepath.Join(dir, "here.txt")
	if err := os.WriteFile(present, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return present, filepath.Join(dir, "gone.txt")
}

// Defends a failing multiexists run handing an agent a way forward: the error
// listed the missing paths but not what to try next, in any encoding. The
// help rides in the error document's fields, as git-changes' not-a-repository
// error does, and the failing run also offers --no-fail. Covers a partial
// miss too: any missing path is the dead end, not only all of them.
func TestMultiexistsMissingErrorCarriesHelp(t *testing.T) {
	present, missing := multiexistsHelpFixture(t)
	want := append(append([]string(nil), wantMultiexistsMissingHelp...), multiexistsNoFailHint)

	for _, paths := range [][]string{{missing}, {present, missing}} {
		t.Run(strings.Join(paths, ","), func(t *testing.T) {
			stdout, _, code := runGrepCLI(t, append(append([]string{"multiexists"}, paths...), "--json")...)
			if code != 1 {
				t.Fatalf("exit %d, want 1\n%s", code, stdout)
			}
			help, ok := jsonHelp(t, stdout)
			if !ok || !reflect.DeepEqual(help, want) {
				t.Errorf("--json error help = %q (present %v), want %q", help, ok, want)
			}

			stdout, _, code = runGrepCLI(t, append(append([]string{"multiexists"}, paths...), "--axi")...)
			if code != 1 {
				t.Fatalf("--axi exit %d, want 1\n%s", code, stdout)
			}
			assertAXIHelp(t, stdout, want)

			stdout, _, code = runGrepCLI(t, append(append([]string{"multiexists"}, paths...), "--plain")...)
			if code != 1 {
				t.Fatalf("--plain exit %d, want 1\n%s", code, stdout)
			}
			for _, h := range want {
				if !strings.Contains(stdout, "  "+h+"\n") {
					t.Errorf("--plain error missing help line %q:\n%s", h, stdout)
				}
			}
		})
	}
}

// Defends the --no-fail result carrying the same next steps: exit 0 with a
// missing path is still a dead end an agent must act on. The --no-fail hint
// is absent because the caller already passed the flag, and --min must not
// drop the help.
func TestMultiexistsNoFailMissingCarriesHelp(t *testing.T) {
	present, missing := multiexistsHelpFixture(t)

	for _, paths := range [][]string{{missing}, {present, missing}} {
		t.Run(strings.Join(paths, ","), func(t *testing.T) {
			args := append(append([]string{"multiexists"}, paths...), "--no-fail")

			help, ok := jsonHelp(t, runOK(t, append(args, "--json")...))
			if !ok || !reflect.DeepEqual(help, wantMultiexistsMissingHelp) {
				t.Errorf("--json help = %q (present %v), want %q", help, ok, wantMultiexistsMissingHelp)
			}

			help, ok = jsonHelp(t, runOK(t, append(args, "--json", "--min")...))
			if !ok || !reflect.DeepEqual(help, wantMultiexistsMissingHelp) {
				t.Errorf("--json --min help = %q (present %v), want %q", help, ok, wantMultiexistsMissingHelp)
			}

			assertAXIHelp(t, runOK(t, append(args, "--axi")...), wantMultiexistsMissingHelp)
		})
	}
}

// Defends an all-exist run staying clean: next steps there are noise an agent
// pays tokens to read, with or without --no-fail, in either encoding.
func TestMultiexistsAllExistCarriesNoHelp(t *testing.T) {
	present, _ := multiexistsHelpFixture(t)

	for _, extra := range [][]string{nil, {"--no-fail"}} {
		args := append([]string{"multiexists", present}, extra...)
		if help, ok := jsonHelp(t, runOK(t, append(args, "--json")...)); ok {
			t.Errorf("%v --json carried help %q", extra, help)
		}
		if out := runOK(t, append(args, "--axi")...); hasHelpKey(out) {
			t.Errorf("%v --axi carried help:\n%s", extra, out)
		}
	}
}

// Defends the plain --no-fail listing omitting help next steps: help is for
// the structured encodings, as grep, multigrep and the home view do. The
// plain error form does print the steps (TestMultiexistsMissingErrorCarriesHelp).
func TestMultiexistsNoFailPlainTextHasNoHelp(t *testing.T) {
	present, missing := multiexistsHelpFixture(t)
	out := runOK(t, "multiexists", present, missing, "--no-fail", "--plain")
	if hasHelpKey(out) {
		t.Errorf("plain --no-fail listing printed a help key:\n%s", out)
	}
}
