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

// TestMinFlagUsageMatchesTheRootPromise defends the epic 11 T6 review
// finding: git-changes, plan-type and git-context each shadow the root
// --min with a local flag whose usage still promised "count only",
// "type only" or "minimal/token-optimized format". Under the TOON default
// the root PersistentPreRunE clears --min (root.go:127-136), so the flag
// is a documented no-op — a usage line that promises the old behaviour
// is the accepted-and-silently-ignored flag class this repo exists to
// prevent. Each local --min usage must match the root persistent flag's.
func TestMinFlagUsageMatchesTheRootPromise(t *testing.T) {
	for _, name := range []string{"git-changes", "plan-type", "git-context"} {
		t.Run(name, func(t *testing.T) {
			root := NewRootCmd()
			cmd, _, err := root.Find([]string{name})
			if err != nil {
				t.Fatalf("Find(%s): %v", name, err)
			}
			f := cmd.Flags().Lookup("min")
			if f == nil {
				t.Fatalf("%s has no --min flag; this test guards its usage string", name)
			}
			rootMin := root.PersistentFlags().Lookup("min")
			if rootMin == nil {
				t.Fatal("root has no persistent --min flag")
			}
			if f.Usage != rootMin.Usage {
				t.Errorf("%s --min usage %q differs from the root flag's %q; under the TOON default --min is a no-op and the flag line must not promise more", name, f.Usage, rootMin.Usage)
			}
		})
	}
}
