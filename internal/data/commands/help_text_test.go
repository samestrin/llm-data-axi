package commands

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestCommandHelpStatesTheTOONDefault defends epic 11 T6: when TOON became
// the default output (epic 11 T1), git-changes and plan-type still told
// callers their default was plain text ("Default: Multi-line text ...",
// "Default: Human-readable text ..."). Help that names the wrong default
// makes an agent pass flags it does not need, or misread the TOON it gets.
// Every "Default:" line in a command's long help must name TOON, and the
// help must say how to get text (--plain). plan-type left for cadence in
// epic 11.2 T3; git-changes keeps the defense.
func TestCommandHelpStatesTheTOONDefault(t *testing.T) {
	for _, name := range []string{"git-changes"} {
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
// plan-type left for cadence in epic 11.2 T3.
func TestMinFlagUsageMatchesTheRootPromise(t *testing.T) {
	for _, name := range []string{"git-changes", "git-context"} {
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

// TestNoSubcommandMinUsageDescribesTOON defends the EST-6854 AC5 rerun-2
// finding: after git-changes, plan-type and git-context were fixed, stats,
// grep, catfiles, tree, template, multiexists, math and the yaml
// subcommands still described their local --min as "Output in
// minimal/token-optimized format", "Minimal output - just the value" or
// "Minimal output (just value)", while the root PersistentPreRunE clears
// --min under TOON (root.go:130-139) and `math 1+1 --min` prints the same
// as `math 1+1`. Every --min line in every subcommand's help must say it has
// no effect on TOON, so no help line promises a change the default output
// never makes.
func TestNoSubcommandMinUsageDescribesTOON(t *testing.T) {
	root := NewRootCmd()
	checked := 0
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if f := c.LocalNonPersistentFlags().Lookup("min"); f != nil {
			checked++
			if !strings.Contains(f.Usage, "no effect on TOON") {
				t.Errorf("%s --min usage %q does not say it has no effect on TOON; under the TOON default --min is cleared", c.CommandPath(), f.Usage)
			}
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(root)
	if checked < 10 {
		t.Fatalf("checked only %d local --min flags; the walk is not reaching the subcommands", checked)
	}
}

// TestLongHelpMinExamplesNameAFormat defends the second half of the same
// finding: yaml get and yaml multiget documented "--min: value (just the
// value)" and gave `yaml get --file config.yaml helper.llm --min` as an
// example, but that run prints TOON key and value, because --min is cleared
// under the TOON default. An example that uses --min must also pass --plain or
// --json, and a "--min:" entry in an output-modes list must say what it does
// to TOON (nothing).
func TestLongHelpMinExamplesNameAFormat(t *testing.T) {
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		lines := strings.Split(c.Long, "\n")
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(trimmed, "--min:"):
				entry := trimmed
				for j := i + 1; j < len(lines); j++ {
					next := strings.TrimSpace(lines[j])
					if next == "" || strings.HasPrefix(next, "--") || strings.HasSuffix(next, ":") {
						break
					}
					entry += " " + next
				}
				if !strings.Contains(entry, "TOON") {
					t.Errorf("%s help entry %q does not say --min has no effect on TOON", c.CommandPath(), entry)
				}
			case strings.Contains(" "+trimmed+" ", " --min ") && !strings.HasPrefix(trimmed, "--"):
				if !strings.Contains(trimmed, "--plain") && !strings.Contains(trimmed, "--json") {
					t.Errorf("%s help example %q uses --min without --plain or --json; under the TOON default it changes nothing", c.CommandPath(), trimmed)
				}
			}
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(NewRootCmd())
}
