package commands

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/samestrin/llm-data-axi/v2/pkg/output"
	"github.com/spf13/cobra"
)

// setupHookCommand is the bare command a SessionStart hook runs when llm-data
// is on PATH. The hook's stdout is added to the session's context, so it is
// the bare run: the home view.
const setupHookCommand = "llm-data"

// openCodePluginMarker opens every plugin file setup writes. A file at the
// plugin's path without it is the user's own, and is never overwritten.
const openCodePluginMarker = "// Managed by `llm-data setup`"

// openCodePluginTemplate is the OpenCode plugin, with %s standing for the
// command as a JSON string literal. Its shape follows the reference plugin in
// kunchenguid/axi packages/axi-sdk-js/src/hooks.ts: one default export that
// OpenCode 2 loads through id and setup(ctx), adding to the session's
// "context" hook, and OpenCode 1 (checked on 1.19.5) through server(input),
// adding to experimental.chat.system.transform. Either way the home view goes
// into the system context once per session, and no tool is added. It runs the
// command without a shell, so the command is unquoted. A failed run adds an
// error line naming the failure, not nothing: an empty context would read as
// a home view with nothing in it.
const openCodePluginTemplate = openCodePluginMarker + `; a later run rewrites this file.
// Adds the llm-data home view to OpenCode's system context. No tool.
import { execFile } from "node:child_process";

const command = %s;

function homeView(directory) {
  return new Promise((resolve) => {
    execFile(command, [], { cwd: directory || process.cwd(), timeout: 10000 }, (err, stdout, stderr) => {
      if (err) {
        const reason = String(stderr || stdout || err.message).trim() || err.message;
        resolve("error: llm-data home view failed: " + reason);
        return;
      }
      resolve(String(stdout).trim());
    });
  });
}

function contextFor(directory) {
  const sessions = new Map();
  return async (sessionID) => {
    const key = sessionID ?? "";
    if (!sessions.has(key)) sessions.set(key, homeView(directory));
    return sessions.get(key);
  };
}

export default {
  id: "llm-data",
  // OpenCode 2.
  async setup(ctx) {
    const context = contextFor(ctx.location.directory);
    await ctx.session.hook("context", async (event) => {
      const text = await context(event.sessionID);
      if (text) event.system.push({ type: "text", text });
    });
  },
  // OpenCode 1.
  async server({ directory }) {
    const context = contextFor(directory);
    return {
      "experimental.chat.system.transform": async (input, output) => {
        const text = await context(input.sessionID);
        if (text) output.system.push(text);
      },
    };
  },
};
`

// setupLookPath and setupExecutable are how setup sees the running binary;
// tests replace them so the hook command does not depend on the test machine.
var (
	setupLookPath   = exec.LookPath
	setupExecutable = os.Executable
)

// resolveHookCommand is the command every target's hook runs: the bare
// `llm-data` when PATH finds it, otherwise this binary's absolute path,
// shell-quoted when it needs to be. A hook nobody can run is an error, not a
// success.
func resolveHookCommand() (string, error) {
	if _, err := setupLookPath(setupHookCommand); err == nil {
		return setupHookCommand, nil
	}
	exe, err := setupExecutable()
	if err != nil || exe == "" || !filepath.IsAbs(exe) {
		return "", fmt.Errorf("llm-data is not on PATH and the path of this binary is unknown; add llm-data to PATH and run setup again")
	}
	return shellQuote(exe), nil
}

// shellQuote single-quotes s when a shell would otherwise split or expand it.
func shellQuote(s string) string {
	if strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/._-+", r))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// shellUnquote reverses shellQuote, for a hook run without a shell.
func shellUnquote(s string) string {
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return strings.ReplaceAll(s[1:len(s)-1], `'\''`, "'")
	}
	return s
}

// isSetupHookCommand reports whether cmd is a hook command setup writes: the
// bare `llm-data`, or an absolute path (quoted or not) to a binary named
// llm-data. Anything else, such as llm-data with arguments, belongs to the
// user. Every target uses it to find a stale command to repair.
func isSetupHookCommand(cmd string) bool {
	if cmd == setupHookCommand {
		return true
	}
	cmd = shellUnquote(cmd)
	base := filepath.Base(cmd)
	return filepath.IsAbs(cmd) && (base == "llm-data" || base == "llm-data.exe")
}

// setupTarget is one agent setup can register llm-data with. plan computes
// the target's files without writing any.
type setupTarget struct {
	name string
	plan func(dir, hook string) ([]setupPlan, error)
}

// setupTargets lists every target in the order setup plans them.
var setupTargets = []setupTarget{
	{name: "claude", plan: planClaude},
	{name: "codex", plan: planCodex},
	{name: "opencode", plan: planOpenCode},
}

func setupTargetNames() []string {
	names := make([]string, len(setupTargets))
	for i, t := range setupTargets {
		names[i] = t.name
	}
	return names
}

// targetFlag is --target: a comma-separated or repeated list of targets. The
// default is claude alone; naming targets replaces it. An unknown name fails
// in Set, so cobra reports it as a usage error before anything runs.
type targetFlag struct {
	names    []string
	explicit bool
}

func (f *targetFlag) String() string { return strings.Join(f.names, ",") }
func (f *targetFlag) Type() string   { return "targets" }

func (f *targetFlag) Set(v string) error {
	if !f.explicit {
		f.names, f.explicit = nil, true
	}
	for _, name := range strings.Split(v, ",") {
		name = strings.TrimSpace(name)
		known := false
		for _, t := range setupTargetNames() {
			known = known || t == name
		}
		if !known {
			return fmt.Errorf("unknown target %q (known: %s)", name, strings.Join(setupTargetNames(), ", "))
		}
		if !slices.Contains(f.names, name) {
			f.names = append(f.names, name)
		}
	}
	return nil
}

// setupSkillContent is the Agent Skill setup writes. It has a committed twin
// at skills/llm-data/SKILL.md, installable with
// `npx skills add samestrin/llm-data-axi --skill llm-data`;
// TestCommittedSkillMatchesSetup fails if the two differ by a byte, so edit
// both together.
const setupSkillContent = `---
name: llm-data
description: Deterministic file, search and project-analysis commands. Use to search code (grep, multigrep), count or list files (count, tree, catfiles), read and edit YAML, inspect git changes, or detect a project's stack, when you need exact results instead of guesses.
---

# llm-data

Run ` + "`llm-data --help`" + ` for the full command list. Output is token-dense TOON by default; add ` + "`--plain`" + ` for text or ` + "`--json`" + ` for JSON.

- ` + "`llm-data grep <pattern> <path>...`" + ` and ` + "`llm-data multigrep`" + ` search files; a missing path is an error, not "no matches".
- ` + "`llm-data tree`" + `, ` + "`llm-data count`" + ` and ` + "`llm-data catfiles`" + ` list, count and read files.
- ` + "`llm-data yaml get|set|multiget|multiset`" + ` reads and edits YAML in place.
- ` + "`llm-data git-changes`" + `, ` + "`llm-data git-context`" + ` and ` + "`llm-data repo-root`" + ` describe the repository.
- ` + "`llm-data detect`" + ` and ` + "`llm-data project-components`" + ` describe the project's stack and layout.
`

// SetupFile is one file setup manages and what happened to it.
type SetupFile struct {
	Path   string `json:"path"`   // relative to Dir
	Status string `json:"status"` // wrote, would_write or unchanged
}

// SetupResult is setup's output.
type SetupResult struct {
	Dir     string      `json:"dir"`
	Targets []string    `json:"targets"`
	Hook    string      `json:"hook"`
	DryRun  bool        `json:"dry_run,omitempty"`
	Changed bool        `json:"changed"`
	Files   []SetupFile `json:"files"`
}

func newSetupCmd() *cobra.Command {
	var (
		path    string
		dryRun  bool
		jsonOut bool
		minOut  bool
		targets = targetFlag{names: []string{"claude"}}
	)
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Register llm-data with Claude Code, Codex or OpenCode in this project",
		Long: `Register llm-data with each --target agent in this project.

  claude  (the default) a SessionStart hook in .claude/settings.json and an
          Agent Skill at .claude/skills/llm-data/SKILL.md
  codex   a SessionStart hook in .codex/hooks.json and [features] hooks = true
          in .codex/config.toml, without which Codex ignores hooks.json
  opencode  a plugin at .opencode/plugins/llm-data.js that adds the home view
          to OpenCode's system context (it adds no tool)

The settings files are merged: other keys, hooks, comments and tables are
kept. The OpenCode plugin is setup's own file and is rewritten whole; a file at its path
that setup did not write is refused.

The hook (and the plugin) runs the bare llm-data when PATH finds it, otherwise this binary's
absolute path. A hook setup wrote earlier that no longer matches, such as the
old path after the binary moved, is repaired in place.

Only files inside the project directory are written; the user-wide ~/.claude,
~/.codex and ~/.config/opencode are never touched, and running setup in the
home directory itself is refused.
Safe to run again: a second run changes no bytes and says so. Never prompts.

Examples:
  llm-data setup
  llm-data setup --dry-run
  llm-data setup --target claude
  llm-data setup --target claude,codex
  llm-data setup --target claude,codex,opencode
  llm-data setup --path /path/to/project`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := runSetup(path, targets.names, dryRun)
			if err != nil {
				return err
			}
			return output.New(jsonOut, minOut, cmd.OutOrStdout()).Print(res, printSetupText)
		},
	}
	cmd.Flags().StringVar(&path, "path", ".", "Project directory")
	cmd.Flags().Var(&targets, "target", "Agents to register with, comma-separated or repeated ("+strings.Join(setupTargetNames(), ", ")+")")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be written without writing")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&minOut, "min", false, "Minimal --json or --plain output (no effect on TOON)")
	return cmd
}

// setupPlan is one planned file write.
type setupPlan struct {
	rel     string
	content []byte
	changed bool
}

// runSetup plans every target's files before writing any, so a settings file
// that cannot be merged fails the command without leaving a half-finished
// setup.
func runSetup(path string, targets []string, dryRun bool) (SetupResult, error) {
	dir, err := setupProjectDir(path)
	if err != nil {
		return SetupResult{}, err
	}
	hook, err := resolveHookCommand()
	if err != nil {
		return SetupResult{}, err
	}

	res := SetupResult{Dir: dir, Hook: hook, DryRun: dryRun}
	var plans []setupPlan
	for _, t := range setupTargets {
		if !slices.Contains(targets, t.name) {
			continue
		}
		p, err := t.plan(dir, hook)
		if err != nil {
			return SetupResult{}, err
		}
		plans = append(plans, p...)
		res.Targets = append(res.Targets, t.name)
	}

	for _, p := range plans {
		status := "unchanged"
		switch {
		case !p.changed:
		case dryRun:
			status = "would_write"
		default:
			full := filepath.Join(dir, p.rel)
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				return SetupResult{}, err
			}
			if err := os.WriteFile(full, p.content, 0o644); err != nil {
				return SetupResult{}, fmt.Errorf("write %s: %w", p.rel, err)
			}
			status = "wrote"
		}
		res.Changed = res.Changed || p.changed
		res.Files = append(res.Files, SetupFile{Path: p.rel, Status: status})
	}
	return res, nil
}

// readProjectFile reads rel under dir after refusing a path a symlink would
// send outside it. A missing file reads as nil.
func readProjectFile(dir, rel string) ([]byte, error) {
	if err := checkInsideProject(dir, filepath.Join(dir, rel)); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read %s: %w", rel, err)
	}
	return b, nil
}

// planClaude plans Claude Code's two files: the SessionStart hook in
// .claude/settings.json and the Agent Skill.
func planClaude(dir, hook string) ([]setupPlan, error) {
	settingsRel := filepath.Join(".claude", "settings.json")
	skillRel := filepath.Join(".claude", "skills", "llm-data", "SKILL.md")

	raw, err := readProjectFile(dir, settingsRel)
	if err != nil {
		return nil, err
	}
	existingSkill, err := readProjectFile(dir, skillRel)
	if err != nil {
		return nil, err
	}
	settings, err := planSettings(filepath.Join(dir, settingsRel), raw, hook)
	if err != nil {
		return nil, err
	}
	settings.rel = settingsRel
	skill := setupPlan{rel: skillRel, content: []byte(setupSkillContent)}
	skill.changed = !bytes.Equal(existingSkill, skill.content)
	return []setupPlan{settings, skill}, nil
}

// planCodex plans Codex's two files: the SessionStart hook in
// .codex/hooks.json, which has the shape of Claude Code's settings and is
// merged the same way, and [features] hooks = true in .codex/config.toml,
// without which Codex does not run hooks.json.
func planCodex(dir, hook string) ([]setupPlan, error) {
	hooksRel := filepath.Join(".codex", "hooks.json")
	configRel := filepath.Join(".codex", "config.toml")

	raw, err := readProjectFile(dir, hooksRel)
	if err != nil {
		return nil, err
	}
	rawConfig, err := readProjectFile(dir, configRel)
	if err != nil {
		return nil, err
	}
	hooks, err := planSettings(filepath.Join(dir, hooksRel), raw, hook)
	if err != nil {
		return nil, err
	}
	hooks.rel = hooksRel
	config, err := planCodexConfig(filepath.Join(dir, configRel), rawConfig)
	if err != nil {
		return nil, err
	}
	config.rel = configRel
	return []setupPlan{hooks, config}, nil
}

// planOpenCode plans OpenCode's one file, the plugin in .opencode/plugins/.
// setup owns the whole file, so path repair is a rewrite: a plugin carrying
// the marker whose bytes differ, such as one naming a binary that has since
// moved, is replaced. A file without the marker is refused and left as it is.
func planOpenCode(dir, hook string) ([]setupPlan, error) {
	rel := filepath.Join(".opencode", "plugins", "llm-data.js")
	existing, err := readProjectFile(dir, rel)
	if err != nil {
		return nil, err
	}
	if existing != nil && !bytes.HasPrefix(existing, []byte(openCodePluginMarker)) {
		return nil, fmt.Errorf("%s was not written by setup; refusing to overwrite it; move it aside and run setup again", filepath.Join(dir, rel))
	}
	command, err := json.Marshal(shellUnquote(hook))
	if err != nil {
		return nil, err
	}
	want := []byte(fmt.Sprintf(openCodePluginTemplate, command))
	return []setupPlan{{rel: rel, content: want, changed: !bytes.Equal(existing, want)}}, nil
}

var (
	tomlTableHeaderRE = regexp.MustCompile(`^\s*\[`)
	tomlFeaturesRE    = regexp.MustCompile(`^\s*\[\s*features\s*\]\s*(#.*)?$`)
	tomlHooksKeyRE    = regexp.MustCompile(`^\s*hooks\s*=`)
	tomlHooksBoolRE   = regexp.MustCompile(`^(\s*hooks\s*=\s*)(?:true|false)(\s*(?:#.*)?)$`)
)

// planCodexConfig computes config.toml with features.hooks = true, without
// writing. It edits lines rather than re-encoding, so comments, key order and
// formatting survive, then decodes the result and refuses unless the only
// change is features.hooks = true. A config that already has it is left
// byte-for-byte alone. raw is nil when the file does not exist.
func planCodexConfig(path string, raw []byte) (setupPlan, error) {
	before := map[string]any{}
	if _, err := toml.Decode(string(raw), &before); err != nil {
		return setupPlan{}, fmt.Errorf("%s is not valid TOML: %w; refusing to overwrite", path, err)
	}
	features, hasFeatures := before["features"]
	if fm, ok := features.(map[string]any); ok && fm["hooks"] == true {
		return setupPlan{content: raw}, nil
	}

	lines := strings.SplitAfter(string(raw), "\n")
	header := slices.IndexFunc(lines, func(l string) bool {
		return tomlFeaturesRE.MatchString(strings.TrimRight(l, "\r\n"))
	})
	var out string
	switch {
	case header >= 0:
		end := len(lines)
		for j := header + 1; j < len(lines); j++ {
			if tomlTableHeaderRE.MatchString(lines[j]) {
				end = j
				break
			}
		}
		eol := "\n"
		if strings.HasSuffix(lines[header], "\r\n") {
			eol = "\r\n"
		}
		if j := slices.IndexFunc(lines[header+1:end], tomlHooksKeyRE.MatchString); j >= 0 {
			line := lines[header+1+j]
			body := strings.TrimRight(line, "\r\n")
			if m := tomlHooksBoolRE.FindStringSubmatch(body); m != nil {
				body = m[1] + "true" + m[2]
			} else {
				body = "hooks = true"
			}
			lines[header+1+j] = body + line[len(strings.TrimRight(line, "\r\n")):]
		} else {
			if !strings.HasSuffix(lines[header], "\n") {
				lines[header] += eol
			}
			lines = slices.Insert(lines, header+1, "hooks = true"+eol)
		}
		out = strings.Join(lines, "")
	case hasFeatures:
		return setupPlan{}, fmt.Errorf("%s sets features outside a [features] table; refusing to overwrite; add hooks = true to it by hand", path)
	default:
		out = string(raw)
		if out != "" && !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		if out != "" {
			out += "\n"
		}
		out += "[features]\nhooks = true\n"
	}

	after := map[string]any{}
	if _, err := toml.Decode(out, &after); err != nil {
		return setupPlan{}, fmt.Errorf("%s: cannot add [features] hooks = true by line edit: %w; refusing to overwrite; add it by hand", path, err)
	}
	fm, _ := after["features"].(map[string]any)
	ok := fm["hooks"] == true
	delete(fm, "hooks")
	if !hasFeatures && len(fm) == 0 {
		delete(after, "features")
	}
	if bf, isMap := before["features"].(map[string]any); isMap {
		delete(bf, "hooks")
	}
	if !ok || !reflect.DeepEqual(before, after) {
		return setupPlan{}, fmt.Errorf("%s: cannot add [features] hooks = true without changing other keys; refusing to overwrite; add it by hand", path)
	}
	return setupPlan{content: []byte(out), changed: true}, nil
}

// setupProjectDir resolves path to an existing directory that is not the home
// directory. In the home directory, .claude/settings.json and .codex are the
// user-wide settings, which are out of scope.
func setupProjectDir(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}
	dir, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("path not found: %s", abs)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return "", fmt.Errorf("not a directory: %s", abs)
	}
	if home, err := os.UserHomeDir(); err == nil {
		if h, err := filepath.EvalSymlinks(home); err == nil && h == dir {
			return "", fmt.Errorf("%s is the home directory; setup writes project settings only, never ~/.claude or ~/.codex", abs)
		}
	}
	return dir, nil
}

// checkInsideProject refuses a target that a symlink would send outside dir,
// such as a .claude directory linked to ~/.claude. It resolves the deepest
// part of target that exists; the rest does not exist yet and is created as
// plain directories.
func checkInsideProject(dir, target string) error {
	existing, rest := target, ""
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			break
		}
		rest = filepath.Join(filepath.Base(existing), rest)
		existing = parent
	}
	resolved, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", existing, err)
	}
	resolved = filepath.Join(resolved, rest)
	if resolved != dir && !strings.HasPrefix(resolved, dir+string(filepath.Separator)) {
		return fmt.Errorf("%s resolves to %s, outside the project; refusing to write through it", target, resolved)
	}
	return nil
}

// planSettings computes the merged settings without writing. Values are kept
// as the raw JSON they were read as, so key order, number literals and string
// escapes survive; only whitespace is normalised, and only when the file has
// to change at all. When exactly one hook already runs hook, changed is false
// and the file is left byte-for-byte alone. raw is nil when the file does not
// exist.
func planSettings(path string, raw []byte, hook string) (setupPlan, error) {
	var root jsonObject
	var err error
	if raw != nil {
		if root, err = parseJSONObject(raw); err != nil {
			return setupPlan{}, fmt.Errorf("%s: %w; refusing to overwrite", path, err)
		}
	}

	var hooks jsonObject
	if v, ok := root.get("hooks"); ok {
		if hooks, err = parseJSONObject(v); err != nil {
			return setupPlan{}, fmt.Errorf(`%s: "hooks": %w; refusing to overwrite`, path, err)
		}
	}
	var groups []json.RawMessage
	if v, ok := hooks.get("SessionStart"); ok {
		if err := json.Unmarshal(v, &groups); err != nil || groups == nil {
			return setupPlan{}, fmt.Errorf(`%s: "hooks.SessionStart" is not a list; refusing to overwrite`, path)
		}
	}

	groups, found, changed := repairSessionStart(groups, hook)
	if found && !changed {
		return setupPlan{content: raw}, nil
	}
	if !found {
		type entry struct {
			Type    string `json:"type"`
			Command string `json:"command"`
		}
		group, err := json.Marshal(map[string][]entry{"hooks": {{Type: "command", Command: hook}}})
		if err != nil {
			return setupPlan{}, err
		}
		groups = append(groups, group)
	}
	list, err := json.Marshal(groups)
	if err != nil {
		return setupPlan{}, err
	}
	hooks.set("SessionStart", list)
	root.set("hooks", hooks.encode())

	var out bytes.Buffer
	if err := json.Indent(&out, root.encode(), "", "  "); err != nil {
		return setupPlan{}, err
	}
	out.WriteByte('\n')
	return setupPlan{content: out.Bytes(), changed: true}, nil
}

// repairSessionStart leaves exactly one setup hook in groups, running hook:
// the first setup hook (see isSetupHookCommand) is rewritten to hook if it
// differs, any later one is removed, and a group that removal empties is
// dropped. found reports whether a setup hook was there at all; every other
// hook and group is kept as it was read.
func repairSessionStart(groups []json.RawMessage, hook string) (out []json.RawMessage, found, changed bool) {
	for _, g := range groups {
		group, err := parseJSONObject(g)
		if err != nil {
			out = append(out, g)
			continue
		}
		v, _ := group.get("hooks")
		var entries []json.RawMessage
		if json.Unmarshal(v, &entries) != nil {
			out = append(out, g)
			continue
		}
		var kept []json.RawMessage
		groupChanged := false
		for _, e := range entries {
			entry, err := parseJSONObject(e)
			var cmd string
			if err == nil {
				c, _ := entry.get("command")
				err = json.Unmarshal(c, &cmd)
			}
			if err != nil || !isSetupHookCommand(cmd) {
				kept = append(kept, e)
				continue
			}
			switch {
			case found:
				groupChanged = true
			case cmd != hook:
				c, _ := json.Marshal(hook)
				entry.set("command", c)
				kept = append(kept, entry.encode())
				groupChanged, found = true, true
			default:
				kept = append(kept, e)
				found = true
			}
		}
		if !groupChanged {
			out = append(out, g)
			continue
		}
		changed = true
		if len(kept) == 0 {
			continue
		}
		list, _ := json.Marshal(kept)
		group.set("hooks", list)
		out = append(out, group.encode())
	}
	return out, found, changed
}

// jsonObject is a JSON object that remembers its key order. encoding/json
// sorts map keys, which would reorder a user's settings file on every merge.
type jsonObject struct {
	keys []string
	vals map[string]json.RawMessage
}

func parseJSONObject(raw []byte) (jsonObject, error) {
	obj := jsonObject{vals: map[string]json.RawMessage{}}
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return obj, errors.New("not a JSON object")
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return obj, errors.New("not a JSON object")
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return obj, fmt.Errorf("invalid JSON: %w", err)
		}
		key := tok.(string) // inside an object, the decoder only yields string keys
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return obj, fmt.Errorf("invalid JSON: %w", err)
		}
		obj.set(key, v)
	}
	if _, err := dec.Token(); err != nil {
		return obj, fmt.Errorf("invalid JSON: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return obj, errors.New("trailing data after the JSON object")
	}
	return obj, nil
}

func (o jsonObject) get(key string) (json.RawMessage, bool) {
	v, ok := o.vals[key]
	return v, ok
}

func (o *jsonObject) set(key string, v json.RawMessage) {
	if o.vals == nil {
		o.vals = map[string]json.RawMessage{}
	}
	if _, ok := o.vals[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = v
}

// encode writes the object compactly; json.Indent lays it out afterwards.
// Keys are re-marshalled without HTML escaping; values are copied verbatim.
func (o jsonObject) encode() json.RawMessage {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(k)
		b.Truncate(b.Len() - 1) // Encode appends a newline
		b.WriteByte(':')
		b.Write(o.vals[k])
	}
	b.WriteByte('}')
	return b.Bytes()
}

func printSetupText(w io.Writer, data interface{}) {
	r := data.(SetupResult)
	for _, f := range r.Files {
		fmt.Fprintf(w, "%s %s\n", strings.ReplaceAll(f.Status, "_", " "), f.Path)
	}
	fmt.Fprintf(w, "hook: %s\n", r.Hook)
	switch {
	case !r.Changed:
		fmt.Fprintf(w, "nothing to do: llm-data is already set up in %s\n", r.Dir)
	case r.DryRun:
		fmt.Fprintf(w, "dry run: nothing written in %s\n", r.Dir)
	default:
		fmt.Fprintf(w, "set up llm-data in %s\n", r.Dir)
	}
}
