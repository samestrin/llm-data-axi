package moved

import (
	"strings"
	"testing"

	"github.com/samestrin/llm-data-axi/v2/pkg/output"
)

// Defends epic 11.2 T3: every moved name, in both its CLI and MCP spelling,
// resolves to the one error naming its cadence replacement. A name that
// resolves to the wrong replacement would send an agent to a command that
// does not exist.
func TestErrorForNamesTheCadenceReplacement(t *testing.T) {
	for _, c := range []struct{ name, replacement string }{
		{"plan-type", "cadence plan type"},
		{"project-components", "cadence plan components"},
		{"tier-classifier", "cadence tier classify"},
		{"diff-smell", "cadence diff smell"},
	} {
		for _, spelling := range []string{c.name, strings.ReplaceAll(c.name, "-", "_")} {
			err := ErrorFor(spelling)
			if err == nil {
				t.Fatalf("ErrorFor(%q) = nil, want the moved error", spelling)
			}
			want := `command "` + c.name + `" has moved to cadence: run "` + c.replacement + `" instead`
			if err.Error() != want {
				t.Errorf("ErrorFor(%q) = %q, want %q", spelling, err.Error(), want)
			}
		}
	}
}

// The other direction: a name that did not move must not be reported as
// moved, or every unknown command would claim a cadence home it does not have.
func TestErrorForUnknownIsNil(t *testing.T) {
	for _, name := range []string{"", "tree", "yaml_get", "unknown_command", "plan", "cadence"} {
		if err := ErrorFor(name); err != nil {
			t.Errorf("ErrorFor(%q) = %v, want nil", name, err)
		}
	}
}

// A moved call is a usage error (exit 2): retrying it unchanged never works.
// The replacement travels as data next to the message, so a TOON or JSON
// reader does not have to parse it out of the prose.
func TestErrorIsAUsageErrorCarryingMovedTo(t *testing.T) {
	err := ErrorFor("plan-type")
	if got := output.ExitCodeFor(err); got != 2 {
		t.Errorf("ExitCodeFor = %d, want 2", got)
	}
	fields := err.(*Error).ErrorFields()
	if fields["moved_to"] != "cadence plan type" {
		t.Errorf("moved_to = %v, want cadence plan type", fields["moved_to"])
	}
}
