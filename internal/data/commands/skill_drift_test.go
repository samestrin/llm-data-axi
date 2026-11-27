package commands

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// skillOmissions is the per-command record epic 19 T1 asks for: every
// command in the root tree is either named in skills/llm-data/SKILL.md or
// listed here with a one-line reason it is left out. The skill is what an
// agent reads to decide what to ask for, so an omission must be a decision
// with a reason, not an oversight. Hidden moved stubs are not commands and
// are skipped. A command named in the skill must not keep a row here: the
// table would then teach the opposite of the skill.
var skillOmissions = map[string]string{
	"setup": "the installer that writes this skill; an agent reading the skill already has it, and the skill's install line covers a missing binary",
}

// skillNameSpan captures the command a backticked `llm-data <name>` span
// teaches: the first word after llm-data, ended by a space (arguments or
// subcommands follow) or the closing backtick. Flags (--help) and
// placeholders (<command>) are not command names and do not match.
var skillNameSpan = regexp.MustCompile("`llm-data ([a-z][a-z-]*)[` ]")

// skillNamedCommands is the set of commands the skill text names.
// TestSkillNamesOnlyRealCommands already holds every span to a registered
// command; this asks the reverse question, which registered commands the
// spans cover.
func skillNamedCommands(skill string) map[string]bool {
	named := map[string]bool{}
	for _, m := range skillNameSpan.FindAllStringSubmatch(skill, -1) {
		named[m[1]] = true
	}
	return named
}

// Defends epic 19 T1: the skill named 10 of the 18 commands and nothing
// recorded whether the other 8 were left out on purpose, so a command could
// be added and never reach the skill an agent reads. Every visible command
// in NewRootCmd must be named in the committed SKILL.md or carry a one-line
// reason in skillOmissions; a new command with neither fails here. The
// committed file and the setupSkillContent twin are held byte-identical by
// TestCommittedSkillMatchesSetup, so checking one checks both.
func TestSkillNamesEveryCommandOrSaysWhy(t *testing.T) {
	restoreOutputMode(t)
	raw, err := os.ReadFile(filepath.Join(repoRootDir, "skills", "llm-data", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	named := skillNamedCommands(string(raw))
	if len(named) == 0 {
		t.Fatal("found no `llm-data ...` command spans in the committed skill; the parser is broken")
	}
	seen := map[string]bool{}
	for _, cmd := range NewRootCmd().Commands() {
		if cmd.Hidden {
			continue // moved stubs fail naming cadence; they are not commands
		}
		name := cmd.Name()
		seen[name] = true
		reason, omitted := skillOmissions[name]
		switch {
		case named[name] && omitted:
			t.Errorf("%s is named in the skill but skillOmissions still gives a reason to leave it out; delete the row", name)
		case !named[name] && !omitted:
			t.Errorf("command %s is not named in skills/llm-data/SKILL.md and skillOmissions has no reason; add a skill line (and its setupSkillContent twin) or a one-line reason", name)
		case omitted && (strings.TrimSpace(reason) == "" || strings.Contains(reason, "\n")):
			t.Errorf("skillOmissions[%s] must be a one-line reason, got %q", name, reason)
		}
	}
	for name := range skillOmissions {
		if !seen[name] {
			t.Errorf("skillOmissions names %q, which is not a visible command in the root tree; delete the stale row", name)
		}
	}
}

// Defends the extractor both ways, so the drift test above cannot pass on a
// parser that finds nothing or on one that counts flags and placeholders as
// commands: it must name the real commands in known-good skill lines and
// ignore the non-command spans the real skill also carries.
func TestSkillNamedCommandsReadsSpansNotFlags(t *testing.T) {
	good := "Run `llm-data --help` for the list.\n" +
		"- `llm-data grep <pattern> <path>...` and `llm-data multigrep` search files.\n" +
		"- `llm-data yaml get|set|multiget|multiset` reads and edits YAML.\n" +
		"- `llm-data discover-tests` finds the test setup.\n" +
		"Or `go run github.com/samestrin/llm-data-axi/v2/cmd/llm-data@v2.0.2 <command>`."
	named := skillNamedCommands(good)
	for _, want := range []string{"grep", "multigrep", "yaml", "discover-tests"} {
		if !named[want] {
			t.Errorf("extractor missed %q in known-good skill lines", want)
		}
	}
	for _, notCmd := range []string{"--help", "<command>", "go", "run"} {
		if named[notCmd] {
			t.Errorf("extractor counted %q as a command", notCmd)
		}
	}
	if len(named) != 4 {
		t.Errorf("extractor found %d commands, want exactly 4: %v", len(named), named)
	}
}
