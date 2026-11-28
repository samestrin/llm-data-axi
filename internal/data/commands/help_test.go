package commands

import (
	"bytes"
	"fmt"
	"io"
	"reflect"
	"testing"

	"github.com/samestrin/llm-data-axi/v2/pkg/output"
)

// emptyHelpResult is the shape a command's result takes once it carries
// emptyHelp: its own fields, plus a help list that only an empty result fills.
type emptyHelpResult struct {
	Count int      `json:"count"`
	Help  []string `json:"help,omitempty"`
}

// printEmptyHelpText is a plain printer written the way every command's is:
// it prints the result's own fields and leaves help to the structured
// encodings, as grep, multigrep and the home view do.
func printEmptyHelpText(w io.Writer, data interface{}) {
	fmt.Fprintf(w, "count: %d\n", data.(emptyHelpResult).Count)
}

var wantEmptyHelp = []string{"Add -i to match case-insensitively", "Run `llm-data grep --help` for flags"}

// printEmptyHelp prints r the way a command does, under one output mode.
func printEmptyHelp(t *testing.T, r emptyHelpResult, axi, jsonOut, minOut bool) string {
	t.Helper()
	output.SetDefaultAXI(axi)
	t.Cleanup(func() { output.SetDefaultAXI(false) })
	var buf bytes.Buffer
	if err := output.New(jsonOut, minOut, &buf).Print(r, printEmptyHelpText); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// Defends epic 22's one rule for next steps: an empty result tells an agent
// what to try, in TOON and --json (and --json --min, which renames keys), and
// a non-empty one carries no help key at all, not an empty list an agent
// pays tokens to read.
func TestEmptyHelpKeyOnlyOnAnEmptyResult(t *testing.T) {
	empty := emptyHelpResult{Help: emptyHelp(true, wantEmptyHelp...)}
	full := emptyHelpResult{Count: 3, Help: emptyHelp(false, wantEmptyHelp...)}

	for _, minOut := range []bool{false, true} {
		help, ok := jsonHelp(t, printEmptyHelp(t, empty, false, true, minOut))
		if !ok || !reflect.DeepEqual(help, wantEmptyHelp) {
			t.Errorf("--json (min %v) empty help = %q (present %v), want %q", minOut, help, ok, wantEmptyHelp)
		}
		if help, ok := jsonHelp(t, printEmptyHelp(t, full, false, true, minOut)); ok {
			t.Errorf("--json (min %v) non-empty result carried help %q", minOut, help)
		}
	}

	assertAXIHelp(t, printEmptyHelp(t, empty, true, false, false), wantEmptyHelp)
	if out := printEmptyHelp(t, full, true, false, false); hasHelpKey(out) {
		t.Errorf("--axi non-empty result carried help:\n%s", out)
	}
}

// Defends --plain staying help-free, as grep, multigrep and the home view
// are: the text form is for a person, and --help is where its next steps are.
func TestEmptyHelpAbsentFromPlain(t *testing.T) {
	empty := emptyHelpResult{Help: emptyHelp(true, wantEmptyHelp...)}
	if out := printEmptyHelp(t, empty, false, false, false); hasHelpKey(out) || out != "count: 0\n" {
		t.Errorf("--plain empty result = %q, want only %q", out, "count: 0\n")
	}
}

// Defends a caller appending its own hint (as grep does for --full) without
// writing into the step list it passed in, which a shared package variable
// such as homeHelp would then carry into every later run.
func TestEmptyHelpReturnsACopy(t *testing.T) {
	steps := []string{"a", "b"}
	got := emptyHelp(true, steps...)
	got[0] = "changed"
	if steps[0] != "a" {
		t.Errorf("emptyHelp shares its caller's slice: steps[0] = %q", steps[0])
	}
}

// Defends "always present on an empty result": a call site that forgets its
// steps would otherwise drop the key through omitempty and the empty result
// would look like it had nothing to say. It fails loudly in that call site's
// own test instead.
func TestEmptyHelpWithNoStepsPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("emptyHelp(true) with no steps returned instead of panicking")
		}
	}()
	emptyHelp(true)
}
