package commands

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/samestrin/llm-data-axi/pkg/output"
	"github.com/spf13/cobra"
)

// setupHookCommand is what Claude Code runs at session start. Its stdout is
// added to the session's context, so it is the bare run: the home view.
const setupHookCommand = "llm-data"

const setupSkillContent = `---
name: llm-data
description: Deterministic file, search and project-analysis commands. Use to search code (grep, multigrep), count or list files (count, tree, catfiles), read and edit YAML, inspect git changes, or detect a project's stack, when you need exact results instead of guesses.
---

# llm-data

Run ` + "`llm-data --help`" + ` for the full command list. Add ` + "`--axi`" + ` for token-dense TOON output, or ` + "`--json`" + ` for JSON.

- ` + "`llm-data grep <pattern> <path>...`" + ` and ` + "`llm-data multigrep`" + ` search files; a missing path is an error, not "no matches".
- ` + "`llm-data tree`" + `, ` + "`llm-data count`" + ` and ` + "`llm-data catfiles`" + ` list, count and read files.
- ` + "`llm-data yaml get|set|list`" + ` reads and edits YAML in place.
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
	)
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Register llm-data with Claude Code in this project",
		Long: `Write a SessionStart hook into .claude/settings.json (merged: every other
key and hook is kept) and an Agent Skill at .claude/skills/llm-data/SKILL.md.

Only files inside the project directory are written; the user-wide ~/.claude
is never touched, and running setup in the home directory itself is refused.
Safe to run again: a second run changes no bytes and says so. Never prompts.

Examples:
  llm-data setup
  llm-data setup --dry-run
  llm-data setup --path /path/to/project`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := runSetup(path, dryRun)
			if err != nil {
				return err
			}
			return output.New(jsonOut, minOut, cmd.OutOrStdout()).Print(res, printSetupText)
		},
	}
	cmd.Flags().StringVar(&path, "path", ".", "Project directory")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be written without writing")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&minOut, "min", false, "Output in minimal/token-optimized format")
	return cmd
}

// setupPlan is one planned file write.
type setupPlan struct {
	rel     string
	content []byte
	changed bool
}

// runSetup plans both files before writing either, so a settings file that
// cannot be merged fails the command without leaving a half-finished setup.
func runSetup(path string, dryRun bool) (SetupResult, error) {
	dir, err := setupProjectDir(path)
	if err != nil {
		return SetupResult{}, err
	}

	settingsRel := filepath.Join(".claude", "settings.json")
	skillRel := filepath.Join(".claude", "skills", "llm-data", "SKILL.md")
	for _, rel := range []string{settingsRel, skillRel} {
		if err := checkInsideProject(dir, filepath.Join(dir, rel)); err != nil {
			return SetupResult{}, err
		}
	}

	settings, err := planSettings(filepath.Join(dir, settingsRel))
	if err != nil {
		return SetupResult{}, err
	}
	settings.rel = settingsRel

	existing, err := os.ReadFile(filepath.Join(dir, skillRel))
	if err != nil && !os.IsNotExist(err) {
		return SetupResult{}, fmt.Errorf("read %s: %w", skillRel, err)
	}
	skill := setupPlan{rel: skillRel, content: []byte(setupSkillContent)}
	skill.changed = !bytes.Equal(existing, skill.content)

	res := SetupResult{Dir: dir, DryRun: dryRun}
	for _, p := range []setupPlan{settings, skill} {
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

// setupProjectDir resolves path to an existing directory that is not the home
// directory. In the home directory, .claude/settings.json is the user-wide
// settings file, which is out of scope.
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
			return "", fmt.Errorf("%s is the home directory; setup writes project settings only, never ~/.claude", abs)
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
// to change at all. When the hook is already registered, changed is false and
// the file is left byte-for-byte alone.
func planSettings(path string) (setupPlan, error) {
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return setupPlan{}, fmt.Errorf("read %s: %w", path, err)
	}
	var root jsonObject
	if err == nil {
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
	if hasSessionStartCommand(groups, setupHookCommand) {
		return setupPlan{content: raw}, nil
	}

	type hook struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	}
	group, _ := json.Marshal(map[string][]hook{"hooks": {{Type: "command", Command: setupHookCommand}}})
	groups = append(groups, group)
	list, _ := json.Marshal(groups)
	hooks.set("SessionStart", list)
	root.set("hooks", hooks.encode())

	var out bytes.Buffer
	if err := json.Indent(&out, root.encode(), "", "  "); err != nil {
		return setupPlan{}, err
	}
	out.WriteByte('\n')
	return setupPlan{content: out.Bytes(), changed: true}, nil
}

func hasSessionStartCommand(groups []json.RawMessage, command string) bool {
	for _, g := range groups {
		var group struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		}
		if json.Unmarshal(g, &group) != nil {
			continue
		}
		for _, h := range group.Hooks {
			if h.Command == command {
				return true
			}
		}
	}
	return false
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
	switch {
	case !r.Changed:
		fmt.Fprintf(w, "nothing to do: llm-data is already set up in %s\n", r.Dir)
	case r.DryRun:
		fmt.Fprintf(w, "dry run: nothing written in %s\n", r.Dir)
	default:
		fmt.Fprintf(w, "set up llm-data in %s\n", r.Dir)
	}
}
