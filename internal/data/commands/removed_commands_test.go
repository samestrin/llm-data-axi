package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// movedCommands is the epic 11.2 T3 contract: plan-type, project-components,
// tier-classifier and diff-smell left for cadence. The message is pinned here
// in full because the MCP server must return the same text (see
// TestMovedToolsReturnTheCadenceError in internal/data/mcpserver).
var movedCommands = []struct{ name, replacement string }{
	{"plan-type", "cadence plan type"},
	{"project-components", "cadence plan components"},
	{"tier-classifier", "cadence tier classify"},
	{"diff-smell", "cadence diff smell"},
}

func movedMessage(name, replacement string) string {
	return fmt.Sprintf("command %q has moved to cadence: run %q instead", name, replacement)
}

// Defends epic 11.2 T3: a moved command must fail with an error naming its
// cadence replacement, on stdout in the run's own format. A bare "unknown
// command" tells an agent nothing about where the command went, and exit 0
// or an empty stdout would report success for a call that did not run.
func TestMovedCommandsFailNamingCadence(t *testing.T) {
	for _, c := range movedCommands {
		t.Run(c.name, func(t *testing.T) {
			want := movedMessage(c.name, c.replacement)

			// --help must not advertise the moved name.
			code, help, _ := runCLI(t, "--help")
			if code != 0 || !strings.Contains(help, "Available Commands") {
				t.Fatalf("--help: exit %d, no command list:\n%s", code, help)
			}
			if strings.Contains(help, c.name) {
				t.Errorf("--help still lists %s:\n%s", c.name, help)
			}

			// TOON, the default: one error document on stdout, nothing on stderr.
			code, stdout, stderr := runCLI(t, c.name)
			if code != 2 {
				t.Errorf("%s: exit %d, want 2 (usage):\n%s", c.name, code, stdout)
			}
			if stderr != "" {
				t.Errorf("%s: stderr must be empty, got %q", c.name, stderr)
			}
			if !strings.HasPrefix(stdout, "error: true\n") {
				t.Errorf("%s: want a TOON error on stdout, got:\n%s", c.name, stdout)
			}
			if !strings.Contains(stdout, c.replacement) || !strings.Contains(stdout, "moved_to:") {
				t.Errorf("%s: TOON error must name %q and carry moved_to, got:\n%s", c.name, c.replacement, stdout)
			}

			// --json, after and before the name: the same error as JSON.
			for _, args := range [][]string{{c.name, "--json"}, {"--json", c.name}} {
				code, stdout, stderr = runCLI(t, args...)
				if code != 2 || stderr != "" {
					t.Errorf("%v: exit %d, stderr %q, want 2 and empty", args, code, stderr)
				}
				var doc struct {
					Error   bool   `json:"error"`
					Message string `json:"message"`
					MovedTo string `json:"moved_to"`
				}
				if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
					t.Fatalf("%v: stdout is not JSON: %v\n%s", args, err, stdout)
				}
				if !doc.Error || doc.Message != want || doc.MovedTo != c.replacement {
					t.Errorf("%v: got %+v, want error true, message %q, moved_to %q", args, doc, want, c.replacement)
				}
			}

			// --plain carries the same text.
			code, stdout, _ = runCLI(t, c.name, "--plain")
			if code != 2 || stdout != "Error: "+want+"\n" {
				t.Errorf("%s --plain: exit %d, want 2 and %q, got:\n%s", c.name, code, "Error: "+want, stdout)
			}

			// The flags the old command took must not mask the move with an
			// unknown-flag error or run anything.
			code, stdout, _ = runCLI(t, c.name, "--path", "x", "--json")
			if code != 2 || !strings.Contains(stdout, c.replacement) {
				t.Errorf("%s --path x: exit %d, want 2 naming %q, got:\n%s", c.name, code, c.replacement, stdout)
			}

			// Even the help path names the replacement.
			_, stdout, _ = runCLI(t, "help", c.name)
			if !strings.Contains(stdout, c.replacement) {
				t.Errorf("help %s does not name %q:\n%s", c.name, c.replacement, stdout)
			}
		})
	}
}

// extract-links and extract-relevant call a model and the network, and moved
// to llm-extract-axi. This defends against either one coming back: invoking
// the name must fail as an unknown command rather than run, and --help must
// not advertise it.
func TestExtractCommandsAreNotRegistered(t *testing.T) {
	for _, name := range []string{"extract-links", "extract-relevant"} {
		t.Run(name, func(t *testing.T) {
			root := NewRootCmd()
			restoreOutputMode(t)
			for _, c := range root.Commands() {
				if c.Name() == name {
					t.Fatalf("%s is still registered on the root command", name)
				}
			}

			help := new(bytes.Buffer)
			root.SetOut(help)
			root.SetErr(help)
			root.SetArgs([]string{"--help"})
			if err := root.Execute(); err != nil {
				t.Fatalf("--help: %v", err)
			}
			if !strings.Contains(help.String(), "Available Commands") {
				t.Fatalf("--help printed no command list, cannot check it:\n%s", help.String())
			}
			if strings.Contains(help.String(), name) {
				t.Errorf("--help still lists %s:\n%s", name, help.String())
			}

			root = NewRootCmd()
			restoreOutputMode(t)
			root.SetOut(new(bytes.Buffer))
			root.SetErr(new(bytes.Buffer))
			root.SetArgs([]string{name})
			err := root.Execute()
			if err == nil || !strings.Contains(err.Error(), "unknown command") {
				t.Errorf("running %s: want an unknown command error, got %v", name, err)
			}
		})
	}
}
