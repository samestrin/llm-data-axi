package commands

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/samestrin/llm-data-axi/v2/internal/data/mcpserver"
	"github.com/spf13/cobra"
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

// readmeUsageRow captures the command a `## Usage` table row documents:
// the backticked first cell, e.g. "| `git-changes` | ... |". The header row
// and the separator row carry no backticks and do not match.
var readmeUsageRow = regexp.MustCompile("(?m)^\\| `([a-z][a-z-]*)` \\|")

// readmeUsageCommands is the set of commands the README `## Usage` table
// names. Only that section is read: a backticked name under any other
// heading (Configuration, examples elsewhere) is not a Usage row.
func readmeUsageCommands(readme string) map[string]bool {
	named := map[string]bool{}
	const heading = "\n## Usage\n"
	start := strings.Index(readme, heading)
	if start < 0 {
		return named
	}
	section := readme[start+len(heading):]
	if end := strings.Index(section, "\n## "); end >= 0 {
		section = section[:end]
	}
	for _, m := range readmeUsageRow.FindAllStringSubmatch(section, -1) {
		named[m[1]] = true
	}
	return named
}

// Defends epic 19 T2: the README `## Usage` table once omitted `setup` and
// nothing tied it to the command tree, so a command could be added (or
// renamed) and the table an agent reads would silently drift. Every visible
// command in NewRootCmd must have a row, and every row must name a visible
// command: a stale row advertises something that no longer exists, the same
// bug as a missing one. Hidden moved stubs are not commands and are skipped.
func TestReadmeUsageNamesEveryCommand(t *testing.T) {
	restoreOutputMode(t)
	raw, err := os.ReadFile(filepath.Join(repoRootDir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	named := readmeUsageCommands(string(raw))
	if len(named) == 0 {
		t.Fatal("found no command rows in the README `## Usage` table; the parser is broken")
	}
	visible := map[string]bool{}
	for _, cmd := range NewRootCmd().Commands() {
		if cmd.Hidden {
			continue // moved stubs fail naming cadence; they are not commands
		}
		name := cmd.Name()
		visible[name] = true
		if !named[name] {
			t.Errorf("command %s has no row in the README `## Usage` table; add one", name)
		}
	}
	for name := range named {
		if !visible[name] {
			t.Errorf("README `## Usage` names %q, which is not a visible command in the root tree; remove or fix the row", name)
		}
	}
}

// Defends the README extractor both ways: it must read the real table rows
// in a known-good Usage section and stop at the next heading, so a name
// under Configuration is not counted as a documented command, and a README
// with no Usage section yields nothing (which the drift test reports as a
// broken parser, not as "every command is missing").
func TestReadmeUsageCommandsReadsTableNotOtherSections(t *testing.T) {
	good := "# llm-data\n\n## Usage\n\n" +
		"| Command | What it does |\n|---|---|\n" +
		"| `grep` | Search for pattern in files |\n" +
		"| `git-changes` | Count and list git working tree changes |\n" +
		"| `yaml` | Manage YAML configuration files |\n\n" +
		"## Configuration\n\n| Name | Default |\n|---|---|\n" +
		"| `repo-root` | not a command row here |\n"
	named := readmeUsageCommands(good)
	for _, want := range []string{"grep", "git-changes", "yaml"} {
		if !named[want] {
			t.Errorf("extractor missed %q in a known-good Usage table", want)
		}
	}
	if named["repo-root"] {
		t.Errorf("extractor counted a Configuration row as a Usage command")
	}
	if len(named) != 3 {
		t.Errorf("extractor found %d commands, want exactly 3: %v", len(named), named)
	}
	if len(readmeUsageCommands("# llm-data\n\n## Configuration\n")) != 0 {
		t.Error("extractor found commands in a README with no Usage section")
	}
}

// expectedMCPToolNames derives the tool set the MCP server must register
// from the command tree itself, not a hand-written list: every visible
// command except `setup` (the installer that registers the CLI; an MCP
// caller already has the binary), a command with visible subcommands
// contributes one tool per subcommand (`yaml` -> yaml_get, yaml_set,
// yaml_multiget, yaml_multiset), and dashes become underscores because MCP
// tool names use them (git-changes -> llm_data_git_changes).
func expectedMCPToolNames(root *cobra.Command) map[string]bool {
	want := map[string]bool{}
	add := func(parts ...string) {
		want[mcpserver.ToolPrefix+strings.ReplaceAll(strings.Join(parts, "_"), "-", "_")] = true
	}
	for _, cmd := range root.Commands() {
		if cmd.Hidden || cmd.Name() == "setup" {
			continue
		}
		subs := 0
		for _, sub := range cmd.Commands() {
			if sub.Hidden {
				continue
			}
			subs++
			add(cmd.Name(), sub.Name())
		}
		if subs == 0 {
			add(cmd.Name())
		}
	}
	return want
}

// Defends epic 19 T2: TestGetToolDefinitionsRegistersExactlyThePortedSet
// pins the tools against a hand-written list, so a command added to the
// tree without a tool (or a tool left behind after a command moves) passes
// both that test and the list. Here the command tree is the expectation:
// the registered tools must be exactly the visible commands except `setup`,
// with `yaml` split one tool per subcommand. A missing tool, an extra tool
// or a duplicate registration each fail.
func TestMCPToolsMatchCommandTree(t *testing.T) {
	restoreOutputMode(t)
	want := expectedMCPToolNames(NewRootCmd())
	if len(want) == 0 {
		t.Fatal("derived no expected tools from the command tree; the mapping is broken")
	}
	got := map[string]bool{}
	for _, tool := range mcpserver.GetToolDefinitions() {
		if got[tool.Name] {
			t.Errorf("tool %s is registered twice", tool.Name)
		}
		got[tool.Name] = true
	}
	for name := range want {
		if !got[name] {
			t.Errorf("the command tree expects tool %s; GetToolDefinitions does not register it", name)
		}
	}
	for name := range got {
		if !want[name] {
			t.Errorf("tool %s has no visible command behind it in the root tree; remove it or add the command", name)
		}
	}
}

// Defends the tree-to-tool mapping both ways, so the drift test above cannot
// pass on a mapping that drops the setup skip, the subcommand split or the
// dash conversion: a known-good miniature tree must yield exactly the tools
// the real server's naming rules produce, and argument words in Use, hidden
// stubs and `setup` must not leak into tool names.
func TestExpectedMCPToolNamesMapsTreeNotNames(t *testing.T) {
	root := &cobra.Command{Use: "llm-data"}
	root.AddCommand(&cobra.Command{Use: "git-changes"})
	root.AddCommand(&cobra.Command{Use: "setup"})
	root.AddCommand(&cobra.Command{Use: "plan-type", Hidden: true})
	yaml := &cobra.Command{Use: "yaml"}
	yaml.AddCommand(&cobra.Command{Use: "get KEY"}, &cobra.Command{Use: "set KEY VALUE"})
	root.AddCommand(yaml)

	want := expectedMCPToolNames(root)
	for _, name := range []string{"llm_data_git_changes", "llm_data_yaml_get", "llm_data_yaml_set"} {
		if !want[name] {
			t.Errorf("mapping missed %q for a known-good tree", name)
		}
	}
	for _, name := range []string{"llm_data_setup", "llm_data_plan_type", "llm_data_yaml", "llm_data_yaml_get_key"} {
		if want[name] {
			t.Errorf("mapping produced %q, which the naming rules exclude", name)
		}
	}
	if len(want) != 3 {
		t.Errorf("mapping produced %d tools, want exactly 3: %v", len(want), want)
	}
}
