package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// The Codex target, per the axi spec (kunchenguid/axi
// .agents/skills/axi/SKILL.md): "use native hooks in ... <repo>/.codex/hooks.json,
// and ensure [features].hooks = true in config.toml. Prefer SessionStart for
// ambient context via stdout". Each test also relies on setupEnv's check that
// the home directory, where ~/.codex lives, is left empty.

func codexHooksPath(project string) string {
	return filepath.Join(project, ".codex", "hooks.json")
}

func codexConfigPath(project string) string {
	return filepath.Join(project, ".codex", "config.toml")
}

// codexFeaturesHooks decodes config.toml and returns features.hooks.
func codexFeaturesHooks(t *testing.T, raw []byte) any {
	t.Helper()
	var cfg map[string]any
	if _, err := toml.Decode(string(raw), &cfg); err != nil {
		t.Fatalf("config.toml is not valid TOML: %v\n%s", err, raw)
	}
	features, _ := cfg["features"].(map[string]any)
	return features["hooks"]
}

func writeProjectFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Defends the Codex registration itself: --target codex writes the
// SessionStart hook Codex runs and turns on the hooks feature, without which
// Codex ignores hooks.json and the registration would be a success that does
// nothing. It writes no Claude file, since claude was not named.
func TestSetupCodexWritesHookAndFeature(t *testing.T) {
	project, _ := setupEnv(t)

	out, err := runSetupCmd(t, "--path", project, "--target", "codex")
	if err != nil {
		t.Fatalf("setup --target codex: %v", err)
	}
	if cmds := sessionStartCommands(t, readFile(t, codexHooksPath(project))); len(cmds) != 1 || cmds[0] != "llm-data" {
		t.Errorf("hooks.json SessionStart commands = %v, want [llm-data]", cmds)
	}
	if got := codexFeaturesHooks(t, readFile(t, codexConfigPath(project))); got != true {
		t.Errorf("config.toml features.hooks = %v, want true", got)
	}
	for _, want := range []string{
		"wrote " + filepath.Join(".codex", "hooks.json"),
		"wrote " + filepath.Join(".codex", "config.toml"),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not report %q:\n%s", want, out)
		}
	}
	if _, err := os.Stat(filepath.Join(project, ".claude")); !os.IsNotExist(err) {
		t.Errorf("--target codex created .claude (stat err %v)", err)
	}
}

// Defends idempotency for Codex: a second run rewrites neither file, by bytes
// or by mtime, and says nothing was done.
func TestSetupCodexSecondRunChangesNothing(t *testing.T) {
	project, _ := setupEnv(t)
	if _, err := runSetupCmd(t, "--path", project, "--target", "codex"); err != nil {
		t.Fatalf("first run: %v", err)
	}
	before := snapshot(t, project)
	hooksSt, _ := os.Stat(codexHooksPath(project))
	cfgSt, _ := os.Stat(codexConfigPath(project))

	out, err := runSetupCmd(t, "--path", project, "--target", "codex")
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
	if st, _ := os.Stat(codexHooksPath(project)); !st.ModTime().Equal(hooksSt.ModTime()) {
		t.Errorf("second run rewrote hooks.json (mtime changed)")
	}
	if st, _ := os.Stat(codexConfigPath(project)); !st.ModTime().Equal(cfgSt.ModTime()) {
		t.Errorf("second run rewrote config.toml (mtime changed)")
	}
	if strings.Contains(out, "wrote") || !strings.Contains(out, "already set up") {
		t.Errorf("second run does not say it changed nothing:\n%s", out)
	}
}

// Defends Codex path repair through the shared helper: after the binary
// moves, the stale hook is rewritten in place, a foreign hook is kept, and a
// run after the repair is a no-op.
func TestSetupCodexRepairsHookAfterBinaryMoves(t *testing.T) {
	project, _ := setupEnv(t)
	writeProjectFile(t, codexHooksPath(project), `{"hooks": {"SessionStart": [{"hooks": [{"type": "command", "command": "echo hi"}]}]}}`)

	stubBinary(t, false, "/old/place/llm-data")
	if _, err := runSetupCmd(t, "--path", project, "--target", "codex"); err != nil {
		t.Fatalf("first run: %v", err)
	}
	stubBinary(t, false, "/new/place/llm-data")
	out, err := runSetupCmd(t, "--path", project, "--target", "codex")
	if err != nil {
		t.Fatalf("run after the move: %v", err)
	}
	cmds := sessionStartCommands(t, readFile(t, codexHooksPath(project)))
	if len(cmds) != 2 || cmds[0] != "echo hi" || cmds[1] != "/new/place/llm-data" {
		t.Errorf("hooks.json SessionStart commands = %v, want [echo hi /new/place/llm-data]", cmds)
	}
	if !strings.Contains(out, "wrote "+filepath.Join(".codex", "hooks.json")) ||
		!strings.Contains(out, "unchanged "+filepath.Join(".codex", "config.toml")) {
		t.Errorf("repair run does not report exactly the hooks.json rewrite:\n%s", out)
	}

	before := snapshot(t, project)
	out, err = runSetupCmd(t, "--path", project, "--target", "codex")
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

// Defends "merging, never overwriting" for config.toml: setup adds or flips
// the one key it needs and keeps every comment, key and table byte for byte.
// A config that already has hooks on is left alone.
func TestSetupCodexMergesConfig(t *testing.T) {
	cases := []struct {
		name, in, want string
		changed        bool
	}{
		{
			name: "no features table",
			in:   "# my config\nmodel = \"o3\"\n\n[profiles.fast]\nmodel = \"o4-mini\"\n",
			want: "# my config\nmodel = \"o3\"\n\n[profiles.fast]\nmodel = \"o4-mini\"\n\n[features]\nhooks = true\n",
		},
		{
			name: "no trailing newline",
			in:   "model = \"o3\"",
			want: "model = \"o3\"\n\n[features]\nhooks = true\n",
		},
		{
			name: "features table without hooks",
			in:   "model = \"o3\"\n[features] # flags\nweb_search = true\n\n[profiles.fast]\nmodel = \"x\"\n",
			want: "model = \"o3\"\n[features] # flags\nhooks = true\nweb_search = true\n\n[profiles.fast]\nmodel = \"x\"\n",
		},
		{
			name: "hooks off",
			in:   "[features]\nweb_search = true\nhooks = false # off for now\n[tui]\nx = 1\n",
			want: "[features]\nweb_search = true\nhooks = true # off for now\n[tui]\nx = 1\n",
		},
		{
			name: "hooks off, CRLF",
			in:   "[features]\r\nhooks = false\r\n",
			want: "[features]\r\nhooks = true\r\n",
		},
		{
			name: "already on",
			in:   "# keep me\n[features]\n  hooks   =   true\n",
			want: "# keep me\n[features]\n  hooks   =   true\n",
		},
		{
			name: "already on as a dotted key",
			in:   "features.hooks = true\n",
			want: "features.hooks = true\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			project, _ := setupEnv(t)
			writeProjectFile(t, codexConfigPath(project), c.in)
			out, err := runSetupCmd(t, "--path", project, "--target", "codex")
			if err != nil {
				t.Fatalf("setup: %v", err)
			}
			got := string(readFile(t, codexConfigPath(project)))
			if got != c.want {
				t.Errorf("config.toml =\n%q\nwant\n%q", got, c.want)
			}
			wantStatus := "wrote "
			if c.in == c.want {
				wantStatus = "unchanged "
			}
			if !strings.Contains(out, wantStatus+filepath.Join(".codex", "config.toml")) {
				t.Errorf("output does not report %q for config.toml:\n%s", wantStatus, out)
			}
		})
	}
}

// Defends against a merge that would damage a config setup cannot read or
// cannot edit by lines: each is refused with an error naming the file, and
// nothing is written, not even hooks.json, since every file is planned before
// any is written.
func TestSetupCodexRefusesUnmergeableConfig(t *testing.T) {
	for name, in := range map[string]string{
		"invalid TOML":             "model = \n",
		"features as a dotted key": "features.web_search = true\n",
		"features inline table":    "features = { web_search = true }\n",
		"features not a table":     "features = 1\n",
		"hooks value spans lines":  "[features]\nhooks = [\n  1,\n]\n",
		"only a features subtable": "[features.x]\ny = 1\n",
		// The line edit lands inside the string and still decodes; only
		// the check of the decoded result catches it.
		"features header inside a string": "notes = \"\"\"\n[features]\n\"\"\"\n[features]\nweb_search = true\n",
	} {
		t.Run(name, func(t *testing.T) {
			project, _ := setupEnv(t)
			writeProjectFile(t, codexConfigPath(project), in)
			_, err := runSetupCmd(t, "--path", project, "--target", "claude,codex")
			if err == nil {
				t.Fatal("setup merged a config it should have refused")
			}
			if !strings.Contains(err.Error(), "config.toml") {
				t.Errorf("error does not name the file: %v", err)
			}
			if got := string(readFile(t, codexConfigPath(project))); got != in {
				t.Errorf("config.toml was changed:\n%s", got)
			}
			files := snapshot(t, project)
			if len(files) != 1 {
				t.Errorf("a refused setup wrote files: %v", files)
			}
		})
	}
}

// Defends hooks.json against the same overwrite the Claude settings merge
// refuses: a file that is not a JSON object stops setup.
func TestSetupCodexRefusesUnmergeableHooks(t *testing.T) {
	project, _ := setupEnv(t)
	writeProjectFile(t, codexHooksPath(project), `[1, 2]`)
	_, err := runSetupCmd(t, "--path", project, "--target", "codex")
	if err == nil || !strings.Contains(err.Error(), "hooks.json") {
		t.Fatalf("setup did not refuse a non-object hooks.json: %v", err)
	}
	if _, err := os.Stat(codexConfigPath(project)); !os.IsNotExist(err) {
		t.Errorf("config.toml was written although hooks.json was refused")
	}
}

// Defends --dry-run for Codex: both files are reported as would-write and
// neither exists afterwards; over a stale hook and hooks = false, the bytes
// are left as they were.
func TestSetupCodexDryRunWritesNothing(t *testing.T) {
	project, _ := setupEnv(t)
	out, err := runSetupCmd(t, "--path", project, "--target", "codex", "--dry-run")
	if err != nil {
		t.Fatalf("setup --dry-run: %v", err)
	}
	assertEmptyDir(t, project)
	for _, want := range []string{
		"would write " + filepath.Join(".codex", "hooks.json"),
		"would write " + filepath.Join(".codex", "config.toml"),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dry run does not report %q:\n%s", want, out)
		}
	}

	stale := `{"hooks": {"SessionStart": [{"hooks": [{"type": "command", "command": "/gone/llm-data"}]}]}}`
	writeProjectFile(t, codexHooksPath(project), stale)
	writeProjectFile(t, codexConfigPath(project), "[features]\nhooks = false\n")
	before := snapshot(t, project)
	if _, err := runSetupCmd(t, "--path", project, "--target", "codex", "--dry-run"); err != nil {
		t.Fatalf("setup --dry-run over stale files: %v", err)
	}
	for p, b := range snapshot(t, project) {
		if before[p] != b {
			t.Errorf("dry run changed %s", p)
		}
	}
}

// Defends "other targets untouched": --target claude leaves an existing,
// stale .codex alone, --target codex leaves an existing, stale .claude alone,
// and naming both registers both with the same hook.
func TestSetupTargetsLeaveOtherTargetsUntouched(t *testing.T) {
	staleHook := `{"hooks": {"SessionStart": [{"hooks": [{"type": "command", "command": "/gone/llm-data"}]}]}}`

	t.Run("claude leaves codex", func(t *testing.T) {
		project, _ := setupEnv(t)
		writeProjectFile(t, codexHooksPath(project), staleHook)
		writeProjectFile(t, codexConfigPath(project), "model = \"o3\"\n")
		out, err := runSetupCmd(t, "--path", project, "--target", "claude")
		if err != nil {
			t.Fatalf("setup: %v", err)
		}
		if got := string(readFile(t, codexHooksPath(project))); got != staleHook {
			t.Errorf("--target claude changed .codex/hooks.json:\n%s", got)
		}
		if got := string(readFile(t, codexConfigPath(project))); got != "model = \"o3\"\n" {
			t.Errorf("--target claude changed .codex/config.toml:\n%s", got)
		}
		if strings.Contains(out, ".codex") {
			t.Errorf("--target claude reports a Codex file:\n%s", out)
		}
	})

	t.Run("codex leaves claude", func(t *testing.T) {
		project, _ := setupEnv(t)
		writeSettings(t, project, staleHook)
		out, err := runSetupCmd(t, "--path", project, "--target", "codex")
		if err != nil {
			t.Fatalf("setup: %v", err)
		}
		if got := string(readFile(t, settingsPath(project))); got != staleHook {
			t.Errorf("--target codex changed .claude/settings.json:\n%s", got)
		}
		if _, err := os.Stat(skillPath(project)); !os.IsNotExist(err) {
			t.Errorf("--target codex wrote the Claude skill")
		}
		if strings.Contains(out, ".claude") {
			t.Errorf("--target codex reports a Claude file:\n%s", out)
		}
	})

	t.Run("both", func(t *testing.T) {
		project, _ := setupEnv(t)
		stubBinary(t, false, "/opt/llm-data")
		out, err := runSetupCmd(t, "--path", project, "--target", "codex", "--target", "claude", "--json")
		if err != nil {
			t.Fatalf("setup: %v", err)
		}
		var res SetupResult
		if err := json.Unmarshal([]byte(out), &res); err != nil {
			t.Fatalf("--json output is not JSON: %v\n%s", err, out)
		}
		if strings.Join(res.Targets, ",") != "claude,codex" || len(res.Files) != 4 {
			t.Errorf("JSON targets=%v files=%v, want [claude codex] and 4 files", res.Targets, res.Files)
		}
		for _, p := range []string{settingsPath(project), codexHooksPath(project)} {
			if cmds := sessionStartCommands(t, readFile(t, p)); len(cmds) != 1 || cmds[0] != "/opt/llm-data" {
				t.Errorf("%s SessionStart commands = %v, want [/opt/llm-data]", p, cmds)
			}
		}
	})
}

// Defends the symlink guard for the new directory: a .codex linked to
// ~/.codex would make a project setup rewrite the user-wide Codex config.
func TestSetupCodexRefusesASymlinkOutOfTheProject(t *testing.T) {
	project, _ := setupEnv(t)
	outside := evalTemp(t)
	if err := os.Symlink(outside, filepath.Join(project, ".codex")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	_, err := runSetupCmd(t, "--path", project, "--target", "codex")
	if err == nil || !strings.Contains(err.Error(), "outside the project") {
		t.Fatalf("setup wrote through a symlink out of the project: %v", err)
	}
	assertEmptyDir(t, outside)
}
