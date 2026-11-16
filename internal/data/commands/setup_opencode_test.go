package commands

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The OpenCode target, per the axi spec (kunchenguid/axi
// .agents/skills/axi/SKILL.md): "use a managed plugin in
// ~/.config/opencode/plugins/ or <repo>/.opencode/plugins/. Prefer ambient
// system-context injection for the home view rather than adding a custom
// tool". Each test also relies on setupEnv's check that the home directory,
// where ~/.config/opencode lives, is left empty.

func openCodePluginPath(project string) string {
	return filepath.Join(project, ".opencode", "plugins", "llm-data.js")
}

var openCodePluginRel = filepath.Join(".opencode", "plugins", "llm-data.js")

// pluginCommand returns the command string literal the plugin runs.
func pluginCommand(t *testing.T, plugin []byte) string {
	t.Helper()
	for _, line := range strings.Split(string(plugin), "\n") {
		if lit, ok := strings.CutPrefix(line, "const command = "); ok {
			var cmd string
			if err := json.Unmarshal([]byte(strings.TrimSuffix(lit, ";")), &cmd); err != nil {
				t.Fatalf("command literal %q is not a JSON string: %v", lit, err)
			}
			return cmd
		}
	}
	t.Fatalf("plugin has no command line:\n%s", plugin)
	return ""
}

// Defends the OpenCode registration itself: --target opencode writes one
// managed plugin that runs llm-data, and no Claude or Codex file, since
// neither was named.
func TestSetupOpenCodeWritesPluginOnce(t *testing.T) {
	project, _ := setupEnv(t)

	out, err := runSetupCmd(t, "--path", project, "--target", "opencode")
	if err != nil {
		t.Fatalf("setup --target opencode: %v", err)
	}
	plugin := readFile(t, openCodePluginPath(project))
	if !strings.HasPrefix(string(plugin), openCodePluginMarker) {
		t.Errorf("plugin does not open with the managed marker:\n%s", plugin)
	}
	if got := pluginCommand(t, plugin); got != "llm-data" {
		t.Errorf("plugin command = %q, want llm-data", got)
	}
	if !strings.Contains(out, "wrote "+openCodePluginRel) {
		t.Errorf("output does not report the plugin:\n%s", out)
	}
	entries, _ := os.ReadDir(filepath.Join(project, ".opencode", "plugins"))
	if len(entries) != 1 {
		t.Errorf(".opencode/plugins has %d entries, want 1", len(entries))
	}
	for _, d := range []string{".claude", ".codex"} {
		if _, err := os.Stat(filepath.Join(project, d)); !os.IsNotExist(err) {
			t.Errorf("--target opencode created %s (stat err %v)", d, err)
		}
	}
}

// Defends idempotency for OpenCode: a second run rewrites nothing, by bytes or
// by mtime, and says nothing was done.
func TestSetupOpenCodeSecondRunChangesNothing(t *testing.T) {
	project, _ := setupEnv(t)
	if _, err := runSetupCmd(t, "--path", project, "--target", "opencode"); err != nil {
		t.Fatalf("first run: %v", err)
	}
	before := snapshot(t, project)
	st, _ := os.Stat(openCodePluginPath(project))

	out, err := runSetupCmd(t, "--path", project, "--target", "opencode")
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
	if st2, _ := os.Stat(openCodePluginPath(project)); !st2.ModTime().Equal(st.ModTime()) {
		t.Errorf("second run rewrote the plugin (mtime changed)")
	}
	if strings.Contains(out, "wrote") || !strings.Contains(out, "unchanged "+openCodePluginRel) || !strings.Contains(out, "already set up") {
		t.Errorf("second run does not say it changed nothing:\n%s", out)
	}
}

// Defends path repair through the T3 helper: after the binary moves, the
// plugin is rewritten to run the new path, and a run after the repair is a
// no-op.
func TestSetupOpenCodeRepairsPluginAfterBinaryMoves(t *testing.T) {
	project, _ := setupEnv(t)

	stubBinary(t, false, "/old/place/llm-data")
	if _, err := runSetupCmd(t, "--path", project, "--target", "opencode"); err != nil {
		t.Fatalf("first run: %v", err)
	}
	stubBinary(t, false, "/new/place/llm-data")
	out, err := runSetupCmd(t, "--path", project, "--target", "opencode")
	if err != nil {
		t.Fatalf("run after the move: %v", err)
	}
	plugin := readFile(t, openCodePluginPath(project))
	if got := pluginCommand(t, plugin); got != "/new/place/llm-data" {
		t.Errorf("plugin command = %q, want /new/place/llm-data", got)
	}
	if strings.Contains(string(plugin), "/old/place") {
		t.Errorf("repaired plugin still names the old path:\n%s", plugin)
	}
	if !strings.Contains(out, "wrote "+openCodePluginRel) {
		t.Errorf("repair run does not report the rewrite:\n%s", out)
	}

	before := snapshot(t, project)
	out, err = runSetupCmd(t, "--path", project, "--target", "opencode")
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

// Defends the command the plugin runs: the T3 helper shell-quotes a path a
// shell would split, but the plugin runs it without a shell, so the quotes
// must come off again or OpenCode would look for a file named with them.
func TestSetupOpenCodePluginRunsTheUnquotedPath(t *testing.T) {
	project, _ := setupEnv(t)
	exe := "/opt/my apps/it's/llm-data"
	stubBinary(t, false, exe)

	out, err := runSetupCmd(t, "--path", project, "--target", "opencode,claude")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if got := pluginCommand(t, readFile(t, openCodePluginPath(project))); got != exe {
		t.Errorf("plugin command = %q, want %q", got, exe)
	}
	if cmds := sessionStartCommands(t, readFile(t, settingsPath(project))); len(cmds) != 1 || cmds[0] != shellQuote(exe) {
		t.Errorf("Claude hook = %v, want the shell-quoted %q", cmds, shellQuote(exe))
	}
	if !strings.Contains(out, "wrote "+openCodePluginRel) {
		t.Errorf("output does not report the plugin:\n%s", out)
	}
}

// Defends the user's own plugin: a file at the plugin's path that setup did
// not write is refused, named in the error, and left byte for byte; nothing
// else is written either, since every file is planned before any is written.
func TestSetupOpenCodeRefusesAPluginItDidNotWrite(t *testing.T) {
	project, _ := setupEnv(t)
	mine := "export const Mine = async () => ({});\n"
	writeProjectFile(t, openCodePluginPath(project), mine)

	_, err := runSetupCmd(t, "--path", project, "--target", "claude,opencode")
	if err == nil || !strings.Contains(err.Error(), "llm-data.js") || !strings.Contains(err.Error(), "not written by setup") {
		t.Fatalf("setup over a user's plugin: err = %v, want a refusal naming the file", err)
	}
	if got := string(readFile(t, openCodePluginPath(project))); got != mine {
		t.Errorf("refused plugin was changed:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(project, ".claude")); !os.IsNotExist(err) {
		t.Errorf("a refused run still wrote .claude (stat err %v)", err)
	}
}

// Defends --dry-run for OpenCode: the plugin is reported as would-write and
// does not exist afterwards; over a stale plugin, the bytes are left alone.
func TestSetupOpenCodeDryRunWritesNothing(t *testing.T) {
	project, _ := setupEnv(t)
	out, err := runSetupCmd(t, "--path", project, "--target", "opencode", "--dry-run")
	if err != nil {
		t.Fatalf("setup --dry-run: %v", err)
	}
	assertEmptyDir(t, project)
	if !strings.Contains(out, "would write "+openCodePluginRel) {
		t.Errorf("dry run does not report the plugin:\n%s", out)
	}

	stubBinary(t, false, "/gone/llm-data")
	if _, err := runSetupCmd(t, "--path", project, "--target", "opencode"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	stubBinary(t, true, "/usr/local/bin/llm-data")
	before := snapshot(t, project)
	out, err = runSetupCmd(t, "--path", project, "--target", "opencode", "--dry-run")
	if err != nil {
		t.Fatalf("setup --dry-run over a stale plugin: %v", err)
	}
	if !strings.Contains(out, "would write "+openCodePluginRel) {
		t.Errorf("dry run over a stale plugin does not report it:\n%s", out)
	}
	for p, b := range snapshot(t, project) {
		if before[p] != b {
			t.Errorf("dry run changed %s", p)
		}
	}
}

// Defends "other targets untouched": --target opencode leaves stale Claude
// and Codex files alone, the other targets leave a stale plugin alone, and
// naming all three registers all three with the same command.
func TestSetupOpenCodeLeavesOtherTargetsUntouched(t *testing.T) {
	staleHook := `{"hooks": {"SessionStart": [{"hooks": [{"type": "command", "command": "/gone/llm-data"}]}]}}`

	t.Run("opencode leaves claude and codex", func(t *testing.T) {
		project, _ := setupEnv(t)
		writeSettings(t, project, staleHook)
		writeProjectFile(t, codexHooksPath(project), staleHook)
		before := snapshot(t, project)
		out, err := runSetupCmd(t, "--path", project, "--target", "opencode")
		if err != nil {
			t.Fatalf("setup: %v", err)
		}
		after := snapshot(t, project)
		for p, b := range before {
			if after[p] != b {
				t.Errorf("--target opencode changed %s", p)
			}
		}
		if _, err := os.Stat(skillPath(project)); !os.IsNotExist(err) {
			t.Errorf("--target opencode wrote the Claude skill")
		}
		if strings.Contains(out, ".claude") || strings.Contains(out, ".codex") {
			t.Errorf("--target opencode reports another target's file:\n%s", out)
		}
	})

	t.Run("claude and codex leave opencode", func(t *testing.T) {
		project, _ := setupEnv(t)
		stubBinary(t, false, "/gone/llm-data")
		if _, err := runSetupCmd(t, "--path", project, "--target", "opencode"); err != nil {
			t.Fatalf("setup: %v", err)
		}
		stale := string(readFile(t, openCodePluginPath(project)))
		stubBinary(t, true, "/usr/local/bin/llm-data")
		out, err := runSetupCmd(t, "--path", project, "--target", "claude,codex")
		if err != nil {
			t.Fatalf("setup: %v", err)
		}
		if got := string(readFile(t, openCodePluginPath(project))); got != stale {
			t.Errorf("--target claude,codex changed the OpenCode plugin:\n%s", got)
		}
		if strings.Contains(out, ".opencode") {
			t.Errorf("--target claude,codex reports the OpenCode plugin:\n%s", out)
		}
	})

	t.Run("all three", func(t *testing.T) {
		project, _ := setupEnv(t)
		stubBinary(t, false, "/opt/llm-data")
		out, err := runSetupCmd(t, "--path", project, "--target", "opencode,codex,claude", "--json")
		if err != nil {
			t.Fatalf("setup: %v", err)
		}
		var res SetupResult
		if err := json.Unmarshal([]byte(out), &res); err != nil {
			t.Fatalf("--json output is not JSON: %v\n%s", err, out)
		}
		if strings.Join(res.Targets, ",") != "claude,codex,opencode" || len(res.Files) != 5 {
			t.Errorf("JSON targets=%v files=%v, want [claude codex opencode] and 5 files", res.Targets, res.Files)
		}
		if got := pluginCommand(t, readFile(t, openCodePluginPath(project))); got != "/opt/llm-data" {
			t.Errorf("plugin command = %q, want /opt/llm-data", got)
		}
	})
}

// Defends the symlink guard for the new directory: a .opencode linked
// elsewhere, such as to ~/.config/opencode, would make a project setup write
// a user-wide plugin.
func TestSetupOpenCodeRefusesASymlinkOutOfTheProject(t *testing.T) {
	project, _ := setupEnv(t)
	outside := evalTemp(t)
	if err := os.Symlink(outside, filepath.Join(project, ".opencode")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	_, err := runSetupCmd(t, "--path", project, "--target", "opencode")
	if err == nil || !strings.Contains(err.Error(), "outside the project") {
		t.Fatalf("setup wrote through a symlink out of the project: %v", err)
	}
	assertEmptyDir(t, outside)
}

// openCodeHarness loads the plugin the way each OpenCode generation does and
// prints, as JSON, what it added to the system prompt. OpenCode 1 calls
// default.server({directory}) and then the experimental.chat.system.transform
// hook; OpenCode 2 calls default.setup(ctx) and then the session "context"
// hook. It also prints the hook names and whether any tool was registered.
const openCodeHarness = `
import plugin from %q;
const dir = process.argv[2];
const v1 = await plugin.server({ directory: dir });
const out1 = { system: [] };
await v1["experimental.chat.system.transform"]({ sessionID: "s" }, out1);
const hooks = {};
await plugin.setup({
  location: { directory: dir },
  session: { hook: async (name, fn) => { hooks[name] = fn; } },
});
const event = { sessionID: "s", system: [] };
await hooks.context(event);
console.log(JSON.stringify({
  id: plugin.id,
  v1Keys: Object.keys(v1),
  v1: out1.system,
  v2Hooks: Object.keys(hooks),
  v2: event.system.map((p) => p.text),
}));
`

// Defends that the plugin does what the spec asks under a real JavaScript
// runtime: the home view of the project lands in the system context under
// both OpenCode entrypoints, no tool is added, and a home view that fails is
// reported as an error in the context rather than silently dropped. The fake
// llm-data prints its working directory, which proves the plugin runs it in
// the project. Skips only when node is not installed; CI's runner has it.
func TestOpenCodePluginInjectsTheHomeView(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}

	run := func(t *testing.T, script string) map[string]any {
		t.Helper()
		project, _ := setupEnv(t)
		bin := filepath.Join(evalTemp(t), "llm-data")
		writeProjectFile(t, bin, script)
		if err := os.Chmod(bin, 0o755); err != nil {
			t.Fatal(err)
		}
		stubBinary(t, false, bin)
		if _, err := runSetupCmd(t, "--path", project, "--target", "opencode"); err != nil {
			t.Fatalf("setup: %v", err)
		}
		harness := filepath.Join(evalTemp(t), "harness.mjs")
		writeProjectFile(t, harness, strings.Replace(openCodeHarness, "%q", string(mustJSON(t, "file://"+openCodePluginPath(project))), 1))
		out, err := exec.Command(node, harness, project).CombinedOutput()
		if err != nil {
			t.Fatalf("node: %v\n%s", err, out)
		}
		var got map[string]any
		if err := json.Unmarshal(out, &got); err != nil {
			t.Fatalf("harness output is not JSON: %v\n%s", err, out)
		}
		got["project"] = project
		return got
	}

	t.Run("home view", func(t *testing.T) {
		got := run(t, "#!/bin/sh\necho \"home view of $(pwd)\"\n")
		want := "home view of " + got["project"].(string)
		for _, k := range []string{"v1", "v2"} {
			parts, _ := got[k].([]any)
			if len(parts) != 1 || !strings.Contains(parts[0].(string), want) {
				t.Errorf("%s system context = %v, want one part containing %q", k, got[k], want)
			}
		}
		if keys, _ := got["v1Keys"].([]any); len(keys) != 1 || keys[0] != "experimental.chat.system.transform" {
			t.Errorf("OpenCode 1 hooks = %v, want only experimental.chat.system.transform (no tool)", got["v1Keys"])
		}
		if hooks, _ := got["v2Hooks"].([]any); len(hooks) != 1 || hooks[0] != "context" {
			t.Errorf("OpenCode 2 hooks = %v, want only context (no tool)", got["v2Hooks"])
		}
		if got["id"] != "llm-data" {
			t.Errorf("plugin id = %v, want llm-data", got["id"])
		}
	})

	t.Run("failed home view", func(t *testing.T) {
		got := run(t, "#!/bin/sh\necho 'boom' >&2\nexit 3\n")
		for _, k := range []string{"v1", "v2"} {
			parts, _ := got[k].([]any)
			if len(parts) != 1 || !strings.Contains(parts[0].(string), "error") || !strings.Contains(parts[0].(string), "boom") {
				t.Errorf("%s system context = %v, want one error part naming the failure", k, got[k])
			}
		}
	})
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
