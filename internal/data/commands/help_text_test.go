package commands

import (
	"strings"
	"testing"
)

// TestCommandHelpStatesTheTOONDefault defends epic 11 T6: when TOON became
// the default output (epic 11 T1), git-changes and plan-type still told
// callers their default was plain text ("Default: Multi-line text ...",
// "Default: Human-readable text ..."). Help that names the wrong default
// makes an agent pass flags it does not need, or misread the TOON it gets.
// Every "Default:" line in a command's long help must name TOON, and the
// help must say how to get text (--plain).
func TestCommandHelpStatesTheTOONDefault(t *testing.T) {
	for _, name := range []string{"git-changes", "plan-type"} {
		t.Run(name, func(t *testing.T) {
			root := NewRootCmd()
			cmd, _, err := root.Find([]string{name})
			if err != nil {
				t.Fatalf("Find(%s): %v", name, err)
			}
			sawDefault := false
			for _, line := range strings.Split(cmd.Long, "\n") {
				if !strings.HasPrefix(strings.TrimSpace(line), "Default:") {
					continue
				}
				sawDefault = true
				if !strings.Contains(line, "TOON") {
					t.Errorf("%s help line %q names a default that is not TOON", name, line)
				}
			}
			if !sawDefault {
				t.Errorf("%s help has no Default: line; the output modes must say what a bare run prints", name)
			}
			if !strings.Contains(cmd.Long, "--plain") {
				t.Errorf("%s help does not name --plain, the way to get text output", name)
			}
		})
	}
}
