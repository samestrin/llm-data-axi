package commands

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// Every test here runs setup against real files in t.TempDir(), with HOME
// pointed at a second, separate temp dir. setup is the one command in this
// repository that writes outside an explicit target the caller named in full,
// and the file it writes has a twin under the home directory that changes what
// every Claude Code session on the machine does. So each test also checks the
// home directory is still empty afterwards.

// setupEnv returns a project dir and a guarded HOME, both with symlinks
// resolved (AGENTS.md: never mix a real path with a raw t.TempDir() path).
func setupEnv(t *testing.T) (project, home string) {
	t.Helper()
	project = evalTemp(t)
	home = evalTemp(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Cleanup(func() { assertEmptyDir(t, home) })
	return project, home
}

func evalTemp(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func assertEmptyDir(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	if len(entries) != 0 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("setup touched the home directory %s: %v", dir, names)
	}
}

// runSetupCmd drives a fresh setup command and returns stdout and the error.
func runSetupCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newSetupCmd()
	out := new(bytes.Buffer)
	cmd.SetOut(out)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetIn(strings.NewReader("")) // a prompt would read EOF, not hang
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

// snapshot maps every regular file under dir to its bytes.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			files[p] = string(b)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func settingsPath(project string) string {
	return filepath.Join(project, ".claude", "settings.json")
}

func skillPath(project string) string {
	return filepath.Join(project, ".claude", "skills", "llm-data", "SKILL.md")
}

// sessionStartCommands returns every hook command under hooks.SessionStart.
func sessionStartCommands(t *testing.T, raw []byte) []string {
	t.Helper()
	var s struct {
		Hooks struct {
			SessionStart []struct {
				Hooks []struct {
					Type    string `json:"type"`
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"SessionStart"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("settings.json is not valid JSON: %v\n%s", err, raw)
	}
	var cmds []string
	for _, g := range s.Hooks.SessionStart {
		for _, h := range g.Hooks {
			if h.Type != "command" {
				t.Errorf("hook type = %q, want command", h.Type)
			}
			cmds = append(cmds, h.Command)
		}
	}
	return cmds
}

// Defends the command's whole purpose: both files exist afterwards with the
// content Claude Code needs, and the output names each path it wrote.
func TestSetupWritesHookAndSkill(t *testing.T) {
	project, _ := setupEnv(t)

	out, err := runSetupCmd(t, "--path", project)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	cmds := sessionStartCommands(t, readFile(t, settingsPath(project)))
	if len(cmds) != 1 || cmds[0] != setupHookCommand {
		t.Errorf("SessionStart commands = %v, want [%q]", cmds, setupHookCommand)
	}

	skill := string(readFile(t, skillPath(project)))
	if !strings.HasPrefix(skill, "---\nname: llm-data\ndescription: ") {
		t.Errorf("SKILL.md lacks the name/description frontmatter Claude Code needs:\n%s", skill)
	}
	if !strings.Contains(skill, "llm-data grep") {
		t.Errorf("SKILL.md does not tell the agent how to use llm-data:\n%s", skill)
	}

	for _, want := range []string{
		"wrote " + filepath.Join(".claude", "settings.json"),
		"wrote " + filepath.Join(".claude", "skills", "llm-data", "SKILL.md"),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not report %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, project) {
		t.Errorf("output does not name the project directory %s:\n%s", project, out)
	}
}

// Defends idempotency: a second run must not rewrite a single byte (not even
// reformat the JSON), must not append the hook twice, and must say that it did
// nothing rather than print the same "wrote" lines as the first run.
func TestSetupSecondRunChangesNothingAndSaysSo(t *testing.T) {
	project, _ := setupEnv(t)
	if _, err := runSetupCmd(t, "--path", project); err != nil {
		t.Fatalf("first run: %v", err)
	}
	before := snapshot(t, project)
	stBefore, _ := os.Stat(settingsPath(project))

	out, err := runSetupCmd(t, "--path", project)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}

	after := snapshot(t, project)
	if len(after) != len(before) {
		t.Errorf("second run changed the file set: %d -> %d files", len(before), len(after))
	}
	for p, b := range before {
		if after[p] != b {
			t.Errorf("second run changed %s", p)
		}
	}
	stAfter, _ := os.Stat(settingsPath(project))
	if !stAfter.ModTime().Equal(stBefore.ModTime()) {
		t.Errorf("second run rewrote settings.json (mtime changed) even though its bytes did not")
	}
	if strings.Contains(out, "wrote") {
		t.Errorf("second run claims to have written something:\n%s", out)
	}
	if !strings.Contains(out, "unchanged "+filepath.Join(".claude", "settings.json")) ||
		!strings.Contains(out, "already set up") {
		t.Errorf("second run does not say it changed nothing:\n%s", out)
	}
}

// Defends "merging, never overwriting other keys": an existing settings file
// keeps every key, its own SessionStart hooks, its key order and its string
// bytes; only our hook group is appended.
func TestSetupPreservesExistingSettings(t *testing.T) {
	project, _ := setupEnv(t)
	existing := `{
  "permissions": {"allow": ["Bash(make test:*)"]},
  "model": "opus",
  "env": {"BIG": 12345678901234567890},
  "hooks": {
    "PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "a && b <x>"}]}],
    "SessionStart": [{"hooks": [{"type": "command", "command": "echo hi"}]}]
  }
}
`
	if err := os.MkdirAll(filepath.Dir(settingsPath(project)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath(project), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := runSetupCmd(t, "--path", project); err != nil {
		t.Fatalf("setup: %v", err)
	}
	got := readFile(t, settingsPath(project))

	cmds := sessionStartCommands(t, got)
	if len(cmds) != 2 || cmds[0] != "echo hi" || cmds[1] != setupHookCommand {
		t.Errorf("SessionStart commands = %v, want [echo hi %s]", cmds, setupHookCommand)
	}

	var m map[string]json.RawMessage
	if err := json.Unmarshal(got, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"permissions", "model", "env", "hooks"} {
		if _, ok := m[k]; !ok {
			t.Errorf("key %q was dropped:\n%s", k, got)
		}
	}
	s := string(got)
	for _, want := range []string{
		`"Bash(make test:*)"`,
		`"model": "opus"`,
		`12345678901234567890`, // not rounded through float64
		`"a && b <x>"`,         // not HTML-escaped to \u0026
		`"PreToolUse"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("settings.json lost %s:\n%s", want, s)
		}
	}
	order := []string{`"permissions"`, `"model"`, `"env"`, `"hooks"`}
	last := -1
	for _, k := range order {
		i := strings.Index(s, k)
		if i < last {
			t.Errorf("key order changed; want %v:\n%s", order, s)
			break
		}
		last = i
	}

	// And the merged file is itself stable under a second run.
	before := string(got)
	if _, err := runSetupCmd(t, "--path", project); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if after := string(readFile(t, settingsPath(project))); after != before {
		t.Errorf("second run over merged settings changed it:\n%s\n---\n%s", before, after)
	}
}

// Defends --dry-run: it reports what it would write and writes nothing, not
// even the .claude directory.
func TestSetupDryRunWritesNothing(t *testing.T) {
	project, _ := setupEnv(t)

	out, err := runSetupCmd(t, "--path", project, "--dry-run")
	if err != nil {
		t.Fatalf("setup --dry-run: %v", err)
	}
	assertEmptyDir(t, project)
	if !strings.Contains(out, "would write "+filepath.Join(".claude", "settings.json")) {
		t.Errorf("dry run does not say what it would write:\n%s", out)
	}
	if strings.Contains(out, "wrote ") {
		t.Errorf("dry run claims it wrote something:\n%s", out)
	}
}

// Defends --dry-run over an existing settings file: the file is untouched.
func TestSetupDryRunLeavesExistingSettingsAlone(t *testing.T) {
	project, _ := setupEnv(t)
	existing := []byte(`{"model":"opus"}`)
	if err := os.MkdirAll(filepath.Dir(settingsPath(project)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath(project), existing, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runSetupCmd(t, "--path", project, "--dry-run"); err != nil {
		t.Fatalf("setup --dry-run: %v", err)
	}
	if got := readFile(t, settingsPath(project)); !bytes.Equal(got, existing) {
		t.Errorf("dry run changed settings.json:\n%s", got)
	}
	if _, err := os.Stat(skillPath(project)); !os.IsNotExist(err) {
		t.Errorf("dry run created the skill file (stat err %v)", err)
	}
}

// Defends "~/.claude is out of scope": run in the home directory itself, the
// project's .claude/settings.json IS the user-wide one. setup must refuse and
// write nothing, rather than install a hook into every session on the machine.
func TestSetupRefusesTheHomeDirectory(t *testing.T) {
	_, home := setupEnv(t)

	_, err := runSetupCmd(t, "--path", home)
	if err == nil {
		t.Fatal("setup in the home directory succeeded; want an error")
	}
	if !strings.Contains(err.Error(), "home directory") {
		t.Errorf("error does not say why: %v", err)
	}
	// setupEnv's cleanup asserts home is still empty.
}

// Defends project-local writes against a symlink: a .claude directory that
// links out of the project (commonly to ~/.claude) must not be written
// through.
func TestSetupRefusesASymlinkOutOfTheProject(t *testing.T) {
	project, _ := setupEnv(t)
	outside := evalTemp(t)
	if err := os.Symlink(outside, filepath.Join(project, ".claude")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	_, err := runSetupCmd(t, "--path", project)
	if err == nil {
		t.Fatal("setup wrote through a symlink out of the project; want an error")
	}
	if !strings.Contains(err.Error(), "outside the project") {
		t.Errorf("error does not say why: %v", err)
	}
	assertEmptyDir(t, outside)
}

// Defends against reporting success over a settings file setup cannot merge:
// it must fail, leave the file byte-identical, and not write the skill either
// (no half-finished setup).
func TestSetupRefusesUnmergeableSettings(t *testing.T) {
	cases := map[string]string{
		"not json":             `{"model": `,
		"array":                `[]`,
		"null":                 `null`,
		"empty":                ``,
		"trailing data":        `{} {}`,
		"hooks not object":     `{"hooks": []}`,
		"SessionStart not arr": `{"hooks": {"SessionStart": {}}}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			project, _ := setupEnv(t)
			if err := os.MkdirAll(filepath.Dir(settingsPath(project)), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(settingsPath(project), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := runSetupCmd(t, "--path", project); err == nil {
				t.Fatal("setup succeeded over an unmergeable settings.json")
			}
			if got := string(readFile(t, settingsPath(project))); got != content {
				t.Errorf("settings.json was changed:\n%s", got)
			}
			if _, err := os.Stat(skillPath(project)); !os.IsNotExist(err) {
				t.Errorf("skill was written despite the failure (stat err %v)", err)
			}
		})
	}
}

// Defends a missing --path from being created silently: a typo must fail,
// not set up a fresh directory nobody asked for.
func TestSetupMissingPathFails(t *testing.T) {
	project, _ := setupEnv(t)
	missing := filepath.Join(project, "nope")
	if _, err := runSetupCmd(t, "--path", missing); err == nil {
		t.Fatal("setup on a missing path succeeded")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Errorf("setup created the missing path (stat err %v)", err)
	}
}

// Defends machine-readable output: --json and --axi carry each path and its
// status, so an agent can tell a first run from a no-op without parsing prose.
func TestSetupStructuredOutput(t *testing.T) {
	project, _ := setupEnv(t)

	out, err := runSetupCmd(t, "--path", project, "--json")
	if err != nil {
		t.Fatalf("setup --json: %v", err)
	}
	var res SetupResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("--json output is not JSON: %v\n%s", err, out)
	}
	if !res.Changed || len(res.Files) != 2 || res.Files[0].Status != "wrote" ||
		res.Files[0].Path != filepath.Join(".claude", "settings.json") {
		t.Errorf("first run JSON = %+v", res)
	}

	withAXI(t)
	out, err = runSetupCmd(t, "--path", project)
	if err != nil {
		t.Fatalf("setup --axi: %v", err)
	}
	if strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("--axi emitted JSON:\n%s", out)
	}
	if !strings.Contains(out, "unchanged") || strings.Contains(out, "wrote") {
		t.Errorf("--axi second run does not report unchanged:\n%s", out)
	}
}

// committedSkillPath locates skills/llm-data/SKILL.md from this test file,
// the way tests/mcp_integration finds the module root, so the check does not
// depend on the working directory the test binary was launched from.
func committedSkillPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate this test file")
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(thisFile))))
	return filepath.Join(root, "skills", "llm-data", "SKILL.md")
}

// Defends the committed installable skill (skills/llm-data/SKILL.md, what
// `npx skills add samestrin/llm-data-axi --skill llm-data` installs) against
// drifting from the text setup writes: one byte changed on either side must
// fail here, so an installed skill and a setup-written skill can never teach
// an agent different things. The install path itself needs the network and
// is out of scope (epic 09 advisory); byte agreement is what this repo proves.
func TestCommittedSkillMatchesSetup(t *testing.T) {
	project, _ := setupEnv(t)
	if _, err := runSetupCmd(t, "--path", project); err != nil {
		t.Fatalf("setup: %v", err)
	}
	written := readFile(t, skillPath(project))
	committed := readFile(t, committedSkillPath(t))

	if !bytes.Equal(committed, written) {
		t.Errorf("skills/llm-data/SKILL.md differs from the skill setup writes\ncommitted (%d bytes):\n%s\nsetup wrote (%d bytes):\n%s",
			len(committed), committed, len(written), written)
	}
}

// Defends the front matter that makes the committed file an installable
// skill: the skills CLI reads name and description from it, and a file
// without them installs as nothing.
func TestCommittedSkillFrontMatter(t *testing.T) {
	s := string(readFile(t, committedSkillPath(t)))
	lines := strings.Split(s, "\n")
	if len(lines) < 4 || lines[0] != "---" || lines[1] != "name: llm-data" {
		t.Fatalf("committed SKILL.md must open with --- and name: llm-data front matter:\n%s", s)
	}
	if !strings.HasPrefix(lines[2], "description: ") || strings.TrimSpace(strings.TrimPrefix(lines[2], "description: ")) == "" {
		t.Errorf("committed SKILL.md front matter has no description:\n%s", s)
	}
	closed := false
	for _, l := range lines[3:] {
		if l == "---" {
			closed = true
			break
		}
	}
	if !closed {
		t.Errorf("committed SKILL.md front matter is never closed with ---:\n%s", s)
	}
}

// Defends registration: setup is reachable from the root command (the plan
// keeps registration in setup.go so root.go is not edited).
func TestSetupIsRegistered(t *testing.T) {
	for _, c := range NewRootCmd().Commands() {
		if c.Name() == "setup" {
			return
		}
	}
	t.Error("setup is not registered on the root command")
}

// Defends the skill text against naming commands that do not exist: it said
// `llm-data yaml get|set|list` long after `yaml list` was removed, so every
// agent that installed the skill was taught a command that fails. Each
// backticked `llm-data ...` span must resolve, word by word, to registered
// commands, and an a|b|c list of subcommands must name exactly the ones
// registered, so a subcommand added later cannot go unmentioned either.
func TestSkillNamesOnlyRealCommands(t *testing.T) {
	root := NewRootCmd()
	spans := 0
	for i, part := range strings.Split(setupSkillContent, "`") {
		if i%2 == 0 || !strings.HasPrefix(part, "llm-data ") {
			continue
		}
		spans++
		cmd := root
		for _, word := range strings.Fields(part)[1:] {
			if strings.HasPrefix(word, "-") || strings.HasPrefix(word, "<") {
				break
			}
			subs := map[string]*cobra.Command{}
			for _, c := range cmd.Commands() {
				if c.IsAvailableCommand() {
					subs[c.Name()] = c
				}
			}
			named := strings.Split(word, "|")
			for _, n := range named {
				if subs[n] == nil {
					t.Errorf("skill names %q, but %q has no subcommand %q", part, cmd.CommandPath(), n)
				}
			}
			if len(named) > 1 && len(named) != len(subs) {
				t.Errorf("skill lists %q under %q, but it registers %d subcommands", word, cmd.CommandPath(), len(subs))
			}
			if len(named) > 1 || subs[word] == nil {
				break
			}
			cmd = subs[word]
		}
	}
	if spans == 0 {
		t.Fatal("found no `llm-data ...` spans in the skill text; the parser is broken")
	}
}
