package commands

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// leafCommands returns every visible command that runs, below root.
func leafCommands(c *cobra.Command) []*cobra.Command {
	var out []*cobra.Command
	for _, sub := range c.Commands() {
		if sub.Hidden || sub.Name() == "help" || sub.Name() == "completion" {
			continue
		}
		if sub.HasSubCommands() {
			out = append(out, leafCommands(sub)...)
			continue
		}
		out = append(out, sub)
	}
	return out
}

// Defends AXI principle 10 (help shows how to call the command): count,
// detect, stats, catfiles and grep had no examples, so a caller learned the
// call shape by trial and error. Every command's help has an Examples
// section with at least one complete call.
func TestEveryCommandHelpHasExamples(t *testing.T) {
	for _, cmd := range leafCommands(NewRootCmd()) {
		path := strings.TrimPrefix(cmd.CommandPath(), "llm-data ")
		i := strings.Index(cmd.Long, "Examples:")
		if i < 0 {
			t.Errorf("%s: help has no Examples: section", path)
			continue
		}
		if !strings.Contains(cmd.Long[i:], "llm-data "+path) {
			t.Errorf("%s: Examples: section has no `llm-data %s` call", path, path)
		}
	}
}

// Defends the usage line against promising optional arguments that are
// required: `grep [pattern] [paths...]` while grep needs both.
func TestUsageLineMarksRequiredArgumentsRequired(t *testing.T) {
	root := NewRootCmd()
	for name, want := range map[string]string{
		"grep":        "grep <pattern> <path>...",
		"catfiles":    "catfiles <path>...",
		"multiexists": "multiexists <path>...",
	} {
		cmd, _, err := root.Find([]string{name})
		if err != nil {
			t.Fatal(err)
		}
		if cmd.Use != want {
			t.Errorf("%s usage = %q, want %q", name, cmd.Use, want)
		}
	}
}

// Defends the field names in help against the output: count's help said
// "TOTAL: N" while TOON prints count:, and detect's listed STACK,
// PACKAGE_MANAGER and the rest where TOON prints lowercase keys. A caller
// who reads the help and then looks for the field finds nothing.
func TestHelpNamesTheFieldsTheOutputHas(t *testing.T) {
	root := NewRootCmd()
	for name, c := range map[string]struct{ want, not []string }{
		"count":          {[]string{"count", "checked", "unchecked", "partial", "percent"}, []string{"TOTAL:", "CHECKED:"}},
		"detect":         {[]string{"stack", "language", "package_manager", "framework", "has_tests", "pytest_available"}, []string{"STACK:", "PACKAGE_MANAGER:"}},
		"discover-tests": {[]string{"pattern", "test_runner", "unit_test_count"}, []string{"PATTERN:", "TEST_RUNNER:"}},
		"multiexists":    {[]string{"all_exist", "exist_count", "missing_count"}, []string{"ALL_EXIST:", "MISSING_COUNT:"}},
	} {
		cmd, _, err := root.Find([]string{name})
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range c.want {
			if !strings.Contains(cmd.Long, w) {
				t.Errorf("%s help does not name the field %q", name, w)
			}
		}
		for _, n := range c.not {
			if strings.Contains(cmd.Long, n) {
				t.Errorf("%s help still names %q, which the output does not have", name, n)
			}
		}
	}
}
