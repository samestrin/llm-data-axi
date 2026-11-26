package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/samestrin/llm-data-axi/v2/pkg/output"
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
	stubBinary(t, true, "/usr/local/bin/llm-data")
	return project, home
}

// stubBinary fixes what setup sees of the running binary: whether llm-data
// is on PATH, and where the binary itself lives. Without it the hook command
// would depend on the machine running the tests (AGENTS.md: a local number
// and a CI number must not disagree).
func stubBinary(t *testing.T, onPath bool, exe string) {
	t.Helper()
	oldLook, oldExe := setupLookPath, setupExecutable
	t.Cleanup(func() { setupLookPath, setupExecutable = oldLook, oldExe })
	setupLookPath = func(name string) (string, error) {
		if !onPath {
			return "", fmt.Errorf("exec: %q: executable file not found in $PATH", name)
		}
		return exe, nil
	}
	setupExecutable = func() (string, error) {
		if exe == "" {
			return "", fmt.Errorf("executable path unknown")
		}
		return exe, nil
	}
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

// Defends the installed skill against naming commands that do not exist: it
// said `llm-data yaml get|set|list` long after `yaml list` was removed, so
// every agent that installed the skill was taught a command that fails. The
// test runs setup and reads the SKILL.md it writes, and the Cobra registry is
// the oracle: each backticked `llm-data ...` span must resolve, word by word,
// to registered commands, and an a|b|c list of subcommands must name exactly
// the ones registered, so a subcommand added later cannot go unmentioned.
func TestSkillNamesOnlyRealCommands(t *testing.T) {
	project, _ := setupEnv(t)
	if _, err := runSetupCmd(t, "--path", project); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	problems, spans := skillCommandProblems(NewRootCmd(), string(readFile(t, skillPath(project))))
	if spans == 0 {
		t.Fatal("found no `llm-data ...` spans in the installed skill; the parser is broken")
	}
	for _, p := range problems {
		t.Error(p)
	}
}

// Defends TestSkillNamesOnlyRealCommands against passing vacuously: the
// checker must reject the stale `yaml list` it was written for, and a
// subcommand list that leaves a registered subcommand out.
func TestSkillCommandProblemsRejectsStaleReferences(t *testing.T) {
	root := NewRootCmd()
	restoreOutputMode(t)
	for _, text := range []string{
		"Use `llm-data yaml get|set|multiget|multiset|list` here.",
		"Use `llm-data yaml list <file>` here.",
		"Use `llm-data yaml get|set` here.",
		"Use `llm-data nosuchcommand` here.",
	} {
		if problems, _ := skillCommandProblems(root, text); len(problems) == 0 {
			t.Errorf("checker accepted %q", text)
		}
	}
	if problems, spans := skillCommandProblems(root, "Use `llm-data yaml get|set|multiget|multiset` here."); len(problems) != 0 || spans != 1 {
		t.Errorf("checker rejected the real yaml subcommands: %v (spans %d)", problems, spans)
	}
}

// skillCommandProblems checks every backticked `llm-data ...` span in text
// against the commands registered under root, returning one message per
// problem and the number of spans it checked.
func skillCommandProblems(root *cobra.Command, text string) (problems []string, spans int) {
	for i, part := range strings.Split(text, "`") {
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
					problems = append(problems, fmt.Sprintf("skill names %q, but %q has no subcommand %q", part, cmd.CommandPath(), n))
				}
			}
			if len(named) > 1 && len(named) != len(subs) {
				problems = append(problems, fmt.Sprintf("skill lists %q under %q, but it registers %d subcommands", word, cmd.CommandPath(), len(subs)))
			}
			if len(named) > 1 || subs[word] == nil {
				break
			}
			cmd = subs[word]
		}
	}
	return problems, spans
}

// writeSettings seeds .claude/settings.json with content.
func writeSettings(t *testing.T, project, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(settingsPath(project)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath(project), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Defends the absolute-path fallback: a binary that is not on PATH must not
// be registered as a bare `llm-data`, which the session would fail to run
// while setup reported success.
func TestSetupFallsBackToAbsolutePathOffPATH(t *testing.T) {
	project, _ := setupEnv(t)
	stubBinary(t, false, "/opt/tools/llm-data")

	out, err := runSetupCmd(t, "--path", project)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	cmds := sessionStartCommands(t, readFile(t, settingsPath(project)))
	if len(cmds) != 1 || cmds[0] != "/opt/tools/llm-data" {
		t.Errorf("SessionStart commands = %v, want [/opt/tools/llm-data]", cmds)
	}
	if !strings.Contains(out, "hook: /opt/tools/llm-data") {
		t.Errorf("output does not say which hook command it registered:\n%s", out)
	}
}

// Defends repair: after the binary moves, the hook setup wrote earlier points
// at nothing. A second setup must rewrite that one command in place, not add
// a second hook beside it and not leave the stale one, and must keep every
// other hook. A third run then changes nothing.
func TestSetupRepairsHookAfterBinaryMoves(t *testing.T) {
	project, _ := setupEnv(t)
	writeSettings(t, project, `{"hooks": {"SessionStart": [{"hooks": [{"type": "command", "command": "echo hi"}]}]}}`)

	stubBinary(t, false, "/old/place/llm-data")
	if _, err := runSetupCmd(t, "--path", project); err != nil {
		t.Fatalf("first run: %v", err)
	}
	stubBinary(t, false, "/new/place/llm-data")
	out, err := runSetupCmd(t, "--path", project)
	if err != nil {
		t.Fatalf("run after the move: %v", err)
	}
	cmds := sessionStartCommands(t, readFile(t, settingsPath(project)))
	if len(cmds) != 2 || cmds[0] != "echo hi" || cmds[1] != "/new/place/llm-data" {
		t.Errorf("SessionStart commands = %v, want [echo hi /new/place/llm-data]", cmds)
	}
	if !strings.Contains(out, "wrote "+filepath.Join(".claude", "settings.json")) {
		t.Errorf("repair run does not report the rewrite:\n%s", out)
	}

	before := snapshot(t, project)
	out, err = runSetupCmd(t, "--path", project)
	if err != nil {
		t.Fatalf("third run: %v", err)
	}
	if strings.Contains(out, "wrote") {
		t.Errorf("run after the repair claims to have written something:\n%s", out)
	}
	for p, b := range snapshot(t, project) {
		if before[p] != b {
			t.Errorf("run after the repair changed %s", p)
		}
	}
}

// Defends repair in both directions between the bare command and an absolute
// path: llm-data dropping off PATH must turn a bare hook into the absolute
// path, and llm-data arriving on PATH must turn it back.
func TestSetupRepairsBetweenBareAndAbsolute(t *testing.T) {
	project, _ := setupEnv(t)
	if _, err := runSetupCmd(t, "--path", project); err != nil {
		t.Fatalf("on PATH: %v", err)
	}
	stubBinary(t, false, "/home/me/go/bin/llm-data")
	if _, err := runSetupCmd(t, "--path", project); err != nil {
		t.Fatalf("off PATH: %v", err)
	}
	if cmds := sessionStartCommands(t, readFile(t, settingsPath(project))); len(cmds) != 1 || cmds[0] != "/home/me/go/bin/llm-data" {
		t.Errorf("off PATH: SessionStart commands = %v, want [/home/me/go/bin/llm-data]", cmds)
	}
	stubBinary(t, true, "/usr/local/bin/llm-data")
	if _, err := runSetupCmd(t, "--path", project); err != nil {
		t.Fatalf("back on PATH: %v", err)
	}
	if cmds := sessionStartCommands(t, readFile(t, settingsPath(project))); len(cmds) != 1 || cmds[0] != "llm-data" {
		t.Errorf("back on PATH: SessionStart commands = %v, want [llm-data]", cmds)
	}
}

// Defends other people's hooks from "repair": only a command that is exactly
// llm-data (bare, or an absolute path to a binary named llm-data) is ours. A
// hook that runs llm-data with arguments, or another program, is left alone.
func TestSetupRepairLeavesForeignHooksAlone(t *testing.T) {
	project, _ := setupEnv(t)
	writeSettings(t, project, `{"hooks": {"SessionStart": [{"hooks": [
		{"type": "command", "command": "llm-data grep TODO ."},
		{"type": "command", "command": "bin/llm-data"},
		{"type": "command", "command": "./llm-data"},
		{"type": "command", "command": "/usr/bin/llm-data-mcp"},
		{"type": "command", "command": "echo llm-data"}
	]}]}}`)
	stubBinary(t, false, "/opt/llm-data")
	if _, err := runSetupCmd(t, "--path", project); err != nil {
		t.Fatalf("setup: %v", err)
	}
	cmds := sessionStartCommands(t, readFile(t, settingsPath(project)))
	want := []string{"llm-data grep TODO .", "bin/llm-data", "./llm-data", "/usr/bin/llm-data-mcp", "echo llm-data", "/opt/llm-data"}
	if strings.Join(cmds, "\n") != strings.Join(want, "\n") {
		t.Errorf("SessionStart commands = %q, want %q", cmds, want)
	}
}

// Defends against a session that runs the home view twice: two llm-data
// hooks (one stale, one current) collapse to the current one, and a group
// left empty by that is removed rather than kept as an empty hook list.
func TestSetupRepairDropsDuplicateHooks(t *testing.T) {
	project, _ := setupEnv(t)
	writeSettings(t, project, `{"hooks": {"SessionStart": [
		{"hooks": [{"type": "command", "command": "/gone/llm-data"}]},
		{"hooks": [{"type": "command", "command": "echo hi"}, {"type": "command", "command": "llm-data"}]},
		{"hooks": [{"type": "command", "command": "llm-data"}]}
	]}}`)
	if _, err := runSetupCmd(t, "--path", project); err != nil {
		t.Fatalf("setup: %v", err)
	}
	raw := readFile(t, settingsPath(project))
	cmds := sessionStartCommands(t, raw)
	if len(cmds) != 2 || cmds[0] != "llm-data" || cmds[1] != "echo hi" {
		t.Errorf("SessionStart commands = %v, want [llm-data echo hi]", cmds)
	}
	var s struct {
		Hooks struct {
			SessionStart []json.RawMessage `json:"SessionStart"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	if len(s.Hooks.SessionStart) != 2 {
		t.Errorf("SessionStart has %d groups, want 2 (the emptied one removed):\n%s", len(s.Hooks.SessionStart), raw)
	}
}

// Defends a binary path with a space in it: the hook is run by a shell, so an
// unquoted path would split into a command that does not exist. The quoted
// form must also be recognised as ours on the next run.
func TestSetupQuotesAPathWithSpaces(t *testing.T) {
	project, _ := setupEnv(t)
	stubBinary(t, false, "/Users/me/My Tools/llm-data")
	if _, err := runSetupCmd(t, "--path", project); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if cmds := sessionStartCommands(t, readFile(t, settingsPath(project))); len(cmds) != 1 || cmds[0] != "'/Users/me/My Tools/llm-data'" {
		t.Errorf("SessionStart commands = %v, want ['/Users/me/My Tools/llm-data']", cmds)
	}
	out, err := runSetupCmd(t, "--path", project)
	if err != nil || strings.Contains(out, "wrote") {
		t.Errorf("second run over a quoted path was not a no-op (err %v):\n%s", err, out)
	}
	stubBinary(t, false, "/opt/llm-data")
	if _, err := runSetupCmd(t, "--path", project); err != nil {
		t.Fatalf("after move: %v", err)
	}
	if cmds := sessionStartCommands(t, readFile(t, settingsPath(project))); len(cmds) != 1 || cmds[0] != "/opt/llm-data" {
		t.Errorf("quoted stale path was not repaired: %v", cmds)
	}
}

// Defends against registering a relative executable as a hook: the session's
// working directory is not necessarily setup's, so the command may point at
// nothing. Setup must fail and write nothing rather than report success.
func TestSetupFailsWhenExecutablePathIsRelative(t *testing.T) {
	project, _ := setupEnv(t)
	stubBinary(t, false, "bin/llm-data")

	_, err := runSetupCmd(t, "--path", project)
	if err == nil {
		t.Fatal("setup succeeded with a relative executable path")
	}
	assertEmptyDir(t, project)
}

// Defends against registering a hook nobody can run: off PATH and with no
// known path of its own, setup must fail and write nothing.
func TestSetupFailsWhenTheHookCommandCannotBeResolved(t *testing.T) {
	project, _ := setupEnv(t)
	stubBinary(t, false, "")
	_, err := runSetupCmd(t, "--path", project)
	if err == nil {
		t.Fatal("setup succeeded with no runnable hook command")
	}
	if !strings.Contains(err.Error(), "PATH") {
		t.Errorf("error does not say why: %v", err)
	}
	assertEmptyDir(t, project)
}

// Defends --dry-run over a stale hook: it reports the repair it would make
// and leaves the file byte-identical.
func TestSetupDryRunDoesNotRepair(t *testing.T) {
	project, _ := setupEnv(t)
	stale := `{"hooks": {"SessionStart": [{"hooks": [{"type": "command", "command": "/gone/llm-data"}]}]}}`
	writeSettings(t, project, stale)
	out, err := runSetupCmd(t, "--path", project, "--dry-run")
	if err != nil {
		t.Fatalf("setup --dry-run: %v", err)
	}
	if got := string(readFile(t, settingsPath(project))); got != stale {
		t.Errorf("dry run repaired settings.json:\n%s", got)
	}
	if !strings.Contains(out, "would write "+filepath.Join(".claude", "settings.json")) {
		t.Errorf("dry run does not report the repair it would make:\n%s", out)
	}
}

// Defends the target selector: an unknown target is a usage error (exit 2)
// raised before anything is written, never accepted and ignored.
func TestSetupRejectsAnUnknownTarget(t *testing.T) {
	for _, arg := range []string{"vscode", "open-code", "claude,nope", "codex,nope", "opencode,nope", ""} {
		t.Run(arg, func(t *testing.T) {
			project, _ := setupEnv(t)
			_, err := runSetupCmd(t, "--path", project, "--target", arg)
			if err == nil {
				t.Fatalf("--target %q was accepted", arg)
			}
			if !strings.Contains(err.Error(), "--target") || !strings.Contains(err.Error(), "claude") {
				t.Errorf("error does not name the flag and the known targets: %v", err)
			}
			if got := output.ExitCodeFor(err); got != 2 {
				t.Errorf("ExitCodeFor = %d, want 2 (usage error): %v", got, err)
			}
			assertEmptyDir(t, project)
		})
	}
}

// Defends "a bare run changes nothing new": with no --target, setup registers
// Claude Code and nothing else, exactly as an explicit --target claude does;
// the structured output names the targets and the hook command.
func TestSetupDefaultTargetIsClaude(t *testing.T) {
	bare, _ := setupEnv(t)
	explicit := evalTemp(t)

	out, err := runSetupCmd(t, "--path", bare, "--json")
	if err != nil {
		t.Fatalf("bare: %v", err)
	}
	var res SetupResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("--json output is not JSON: %v\n%s", err, out)
	}
	if strings.Join(res.Targets, ",") != "claude" || res.Hook != "llm-data" {
		t.Errorf("bare run JSON targets=%v hook=%q, want [claude] llm-data", res.Targets, res.Hook)
	}
	if _, err := runSetupCmd(t, "--path", explicit, "--target", "claude", "--target", "claude"); err != nil {
		t.Fatalf("--target claude: %v", err)
	}

	rel := func(dir string) map[string]string {
		m := map[string]string{}
		for p, b := range snapshot(t, dir) {
			r, _ := filepath.Rel(dir, p)
			m[r] = b
		}
		return m
	}
	a, b := rel(bare), rel(explicit)
	if len(a) != 2 || len(a) != len(b) {
		t.Fatalf("bare run wrote %d files, --target claude %d; want 2 each", len(a), len(b))
	}
	for p := range a {
		if a[p] != b[p] {
			t.Errorf("%s differs between a bare run and --target claude", p)
		}
	}
}

// Defends the skill text against teaching the pre-epic-11 output contract:
// TOON is the default and --plain gives text, so an agent told to "add --axi
// for TOON" is told something that is no longer how the CLI works.
func TestSkillTextSaysTOONIsTheDefault(t *testing.T) {
	want := "Output is token-dense TOON by default; add `--plain` for text or `--json` for JSON."
	if !strings.Contains(setupSkillContent, want) {
		t.Errorf("skill text does not say %q:\n%s", want, setupSkillContent)
	}
	if strings.Contains(setupSkillContent, "Add `--axi`") {
		t.Errorf("skill text still says TOON needs --axi:\n%s", setupSkillContent)
	}
}
