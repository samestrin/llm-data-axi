package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// sweepFixture is the on-disk world every sweep case runs against. The test
// changes into repo, so git-changes (which filters relative to the work
// tree) sees a repository with one clean commit.
type sweepFixture struct {
	root    string // parent of everything below; a git ceiling, so empty is not in a repo
	missing string // never created
	file    string // a regular file with one line in it
	empty   string // an empty directory
	repo    string // a git repository with one commit and an empty sub/
	yaml    string // a YAML file holding a: 1
	blank   string // a zero-byte file
}

// sweepEntry is what the sweep runs for one command, and why it skips any
// case it does not run. Each args func returns nil when the case does not
// apply, and the matching reason must then say why; a case with neither
// fails the test, so a skip is always a decision someone wrote down.
type sweepEntry struct {
	// missing names a path that does not exist where the command takes one.
	missing       func(fx sweepFixture) []string
	missingReason string
	// wrongType names a file where the command needs a directory, or a
	// directory where it needs a file.
	wrongType       func(fx sweepFixture) []string
	wrongTypeReason string
	// empty is a valid input with nothing in it to report.
	empty func(fx sweepFixture) []string
	// emptyKeys are top-level keys TOON and --json must carry on the empty
	// run, each as an empty list or 0. Nil needs emptyKeysReason.
	emptyKeys       []string
	emptyKeysReason string
	// plainCount is the line --plain prints for the empty count. "" needs
	// plainReason: the command prints no count line in --plain.
	plainCount  string
	plainReason string
}

func fixedArgs(a ...string) func(sweepFixture) []string {
	return func(sweepFixture) []string { return a }
}

// sweepTable has one entry per command NewRootCmd registers. A command added
// to the tree with no entry here fails TestHonestySweep, so a new command
// cannot ship without being asked the same three questions.
var sweepTable = map[string]sweepEntry{
	"catfiles": {
		missing:         func(fx sweepFixture) []string { return []string{"catfiles", fx.missing} },
		wrongTypeReason: "takes files and directories alike, so no type is wrong",
		empty:           func(fx sweepFixture) []string { return []string{"catfiles", fx.empty} },
		emptyKeys:       []string{"files", "file_count"},
		plainCount:      "FILES: 0",
	},
	"count": {
		missing:         func(fx sweepFixture) []string { return []string{"count", "--mode", "files", "--path", fx.missing} },
		wrongTypeReason: "counts lines or checkboxes in a single file too, so a file path is valid",
		empty:           func(fx sweepFixture) []string { return []string{"count", "--mode", "files", "--path", fx.empty} },
		emptyKeys:       []string{"count"},
		plainCount:      "COUNT: 0",
	},
	"detect": {
		missing:         func(fx sweepFixture) []string { return []string{"detect", "--path", fx.missing} },
		wrongType:       func(fx sweepFixture) []string { return []string{"detect", "--path", fx.file} },
		empty:           func(fx sweepFixture) []string { return []string{"detect", "--path", fx.empty} },
		emptyKeysReason: "reports a classification (stack: unknown), not a list or a count",
		plainReason:     "prints the classification fields, no count line",
	},
	"discover-tests": {
		missing:    func(fx sweepFixture) []string { return []string{"discover-tests", "--path", fx.missing} },
		wrongType:  func(fx sweepFixture) []string { return []string{"discover-tests", "--path", fx.file} },
		empty:      func(fx sweepFixture) []string { return []string{"discover-tests", "--path", fx.empty} },
		emptyKeys:  []string{"unit_test_count", "e2e_test_count", "total_test_count"},
		plainCount: "TOTAL_TEST_COUNT: 0",
	},
	"git-changes": {
		// Relative: --path filters the work tree of the current directory.
		missing:         fixedArgs("git-changes", "--path", "nope"),
		wrongTypeReason: "--path is a prefix filter, so a file is as valid as a directory",
		empty:           fixedArgs("git-changes", "--path", "sub"),
		emptyKeys:       []string{"count", "files"},
		plainCount:      "COUNT: 0",
	},
	"git-context": {
		missing:   func(fx sweepFixture) []string { return []string{"git-context", "--path", fx.missing} },
		wrongType: func(fx sweepFixture) []string { return []string{"git-context", "--path", fx.file} },
		empty: func(fx sweepFixture) []string {
			return []string{"git-context", "--path", fx.repo, "--since", "2099-01-01"}
		},
		// Known gap, filed as TECH DEBT on EST-9160 rather than fixed here
		// (epic 18 file scope): commits is omitempty in git_context.go, so a
		// run with no commits drops the key in TOON and --json.
		emptyKeysReason: "commits is dropped when empty (git_context.go Commits omitempty); filed as TECH DEBT on EST-9160",
		plainCount:      "COMMITS: 0",
	},
	"grep": {
		missing:         func(fx sweepFixture) []string { return []string{"grep", "zzz", fx.missing} },
		wrongTypeReason: "searches files and directories alike, so no type is wrong",
		empty:           func(fx sweepFixture) []string { return []string{"grep", "zzz", fx.empty} },
		emptyKeys:       []string{"matches", "files", "total_matches"},
		plainCount:      "total: 0",
	},
	"highest": {
		missing:    func(fx sweepFixture) []string { return []string{"highest", "--path", fx.missing} },
		wrongType:  func(fx sweepFixture) []string { return []string{"highest", "--path", fx.file} },
		empty:      func(fx sweepFixture) []string { return []string{"highest", "--path", fx.empty} },
		emptyKeys:  []string{"count"},
		plainCount: "COUNT: 0",
	},
	"json": {
		missingReason:   "the parent of query, swept on its own",
		wrongTypeReason: "the parent of query, swept on its own",
		emptyKeysReason: "the parent of query; a bare run prints help",
		plainReason:     "the parent of query; a bare run prints help",
	},
	"json query": {
		missing:         func(fx sweepFixture) []string { return []string{"json", "query", fx.missing, "a"} },
		wrongType:       func(fx sweepFixture) []string { return []string{"json", "query", fx.empty, "a"} },
		emptyKeysReason: "returns one value; a path that is not there is an error, not an empty result",
		plainReason:     "prints the value, no count line",
	},
	"markdown": {
		missingReason:   "the parent of headers, section, frontmatter and tasks, each swept on its own",
		wrongTypeReason: "the parent of headers, section, frontmatter and tasks, each swept on its own",
		emptyKeysReason: "the parent of headers, section, frontmatter and tasks; a bare run prints help",
		plainReason:     "the parent of headers, section, frontmatter and tasks; a bare run prints help",
	},
	"markdown frontmatter": {
		missing:         func(fx sweepFixture) []string { return []string{"markdown", "frontmatter", fx.missing} },
		wrongType:       func(fx sweepFixture) []string { return []string{"markdown", "frontmatter", fx.empty} },
		emptyKeysReason: "returns one parsed block; a file with no block is an error, not an empty result",
		plainReason:     "prints the raw block, no count line",
	},
	"markdown headers": {
		missing:    func(fx sweepFixture) []string { return []string{"markdown", "headers", fx.missing} },
		wrongType:  func(fx sweepFixture) []string { return []string{"markdown", "headers", fx.empty} },
		empty:      func(fx sweepFixture) []string { return []string{"markdown", "headers", fx.blank} },
		emptyKeys:  []string{"headers", "total"},
		plainCount: "TOTAL: 0",
	},
	"markdown section": {
		missing:         func(fx sweepFixture) []string { return []string{"markdown", "section", fx.missing, "a"} },
		wrongType:       func(fx sweepFixture) []string { return []string{"markdown", "section", fx.empty, "a"} },
		emptyKeysReason: "returns one section; a title that is not there is an error, not an empty result",
		plainReason:     "prints the section text, no count line",
	},
	"markdown tasks": {
		missing:    func(fx sweepFixture) []string { return []string{"markdown", "tasks", fx.missing} },
		wrongType:  func(fx sweepFixture) []string { return []string{"markdown", "tasks", fx.empty} },
		empty:      func(fx sweepFixture) []string { return []string{"markdown", "tasks", fx.blank} },
		emptyKeys:  []string{"tasks", "total"},
		plainCount: "TOTAL: 0",
	},
	"math": {
		missingReason:   "takes an expression, no path",
		wrongTypeReason: "takes an expression, no path",
		empty:           fixedArgs("math", "0"),
		emptyKeysReason: "prints one value, no list or count",
		plainReason:     "prints the result, no count line",
	},
	"multiexists": {
		missing:         func(fx sweepFixture) []string { return []string{"multiexists", fx.missing} },
		wrongTypeReason: "reports whether any path exists, of any type",
		empty:           func(fx sweepFixture) []string { return []string{"multiexists", fx.empty} },
		emptyKeys:       []string{"missing_count"},
		plainCount:      "MISSING_COUNT: 0",
	},
	"multigrep": {
		missing: func(fx sweepFixture) []string {
			return []string{"multigrep", "--keywords", "zzz", "--path", fx.missing}
		},
		wrongTypeReason: "searches a single file as well as a directory",
		empty:           func(fx sweepFixture) []string { return []string{"multigrep", "--keywords", "zzz", "--path", fx.empty} },
		emptyKeys:       []string{"total_matches", "files_searched", "keywords_with_matches"},
		plainCount:      "Total Matches: 0",
	},
	"repo-root": {
		missing:         func(fx sweepFixture) []string { return []string{"repo-root", "--path", fx.missing} },
		wrongType:       func(fx sweepFixture) []string { return []string{"repo-root", "--path", fx.file} },
		empty:           func(fx sweepFixture) []string { return []string{"repo-root", "--path", fx.empty} },
		emptyKeysReason: "reports git_repository: false outside a repository, not a list or a count",
		plainReason:     "prints GIT_REPOSITORY: FALSE, no count line",
	},
	"runtime": {
		missingReason:   "takes timestamps, no path",
		wrongTypeReason: "takes timestamps, no path",
		empty:           fixedArgs("runtime", "--start", "1", "--end", "1"),
		emptyKeysReason: "prints one duration, no list or count",
		plainReason:     "prints the duration, no count line",
	},
	"setup": {
		missing:         func(fx sweepFixture) []string { return []string{"setup", "--dry-run", "--path", fx.missing} },
		wrongType:       func(fx sweepFixture) []string { return []string{"setup", "--dry-run", "--path", fx.file} },
		empty:           func(fx sweepFixture) []string { return []string{"setup", "--dry-run", "--path", fx.empty} },
		emptyKeysReason: "always plans the files it would write; there is no empty result",
		plainReason:     "prints one line per planned file, no count line",
	},
	"stats": {
		missing:    func(fx sweepFixture) []string { return []string{"stats", "--path", fx.missing} },
		wrongType:  func(fx sweepFixture) []string { return []string{"stats", "--path", fx.file} },
		empty:      func(fx sweepFixture) []string { return []string{"stats", "--path", fx.empty} },
		emptyKeys:  []string{"files", "directories", "by_extension"},
		plainCount: "FILES: 0",
	},
	"template": {
		missing:         func(fx sweepFixture) []string { return []string{"template", fx.missing} },
		wrongType:       func(fx sweepFixture) []string { return []string{"template", fx.empty} },
		empty:           func(fx sweepFixture) []string { return []string{"template", fx.file} },
		emptyKeysReason: "renders text, not a list or a count",
		plainReason:     "prints the rendered text, no count line",
	},
	"tree": {
		missing:     func(fx sweepFixture) []string { return []string{"tree", "--path", fx.missing} },
		wrongType:   func(fx sweepFixture) []string { return []string{"tree", "--path", fx.file} },
		empty:       func(fx sweepFixture) []string { return []string{"tree", "--path", fx.empty} },
		emptyKeys:   []string{"entries", "total"},
		plainReason: "prints only the root line on an empty directory, no count line",
	},
	"yaml": {
		missingReason:   "the parent of get, set, multiget and multiset, each swept on its own",
		wrongTypeReason: "the parent of get, set, multiget and multiset, each swept on its own",
		emptyKeysReason: "the parent of get, set, multiget and multiset; a bare run prints help",
		plainReason:     "the parent of get, set, multiget and multiset; a bare run prints help",
	},
	"yaml get": {
		missing:         func(fx sweepFixture) []string { return []string{"yaml", "get", "--file", fx.missing, "a"} },
		wrongType:       func(fx sweepFixture) []string { return []string{"yaml", "get", "--file", fx.empty, "a"} },
		empty:           func(fx sweepFixture) []string { return []string{"yaml", "get", "--file", fx.yaml, "a"} },
		emptyKeysReason: "returns one value; a key that is not there is an error, not an empty result",
		plainReason:     "prints the value, no count line",
	},
	"yaml set": {
		missing:         func(fx sweepFixture) []string { return []string{"yaml", "set", "--file", fx.missing, "a", "1"} },
		wrongType:       func(fx sweepFixture) []string { return []string{"yaml", "set", "--file", fx.empty, "a", "1"} },
		empty:           func(fx sweepFixture) []string { return []string{"yaml", "set", "--file", fx.yaml, "a", "1"} },
		emptyKeysReason: "writes one value, no list or count",
		plainReason:     "confirms the write, no count line",
	},
	"yaml multiget": {
		missing:         func(fx sweepFixture) []string { return []string{"yaml", "multiget", "--file", fx.missing, "a"} },
		wrongType:       func(fx sweepFixture) []string { return []string{"yaml", "multiget", "--file", fx.empty, "a"} },
		empty:           func(fx sweepFixture) []string { return []string{"yaml", "multiget", "--file", fx.yaml, "a"} },
		emptyKeysReason: "returns the keys asked for; a key that is not there is an error, not an empty result",
		plainReason:     "prints key=value lines, no count line",
	},
	"yaml multiset": {
		missing:         func(fx sweepFixture) []string { return []string{"yaml", "multiset", "--file", fx.missing, "a", "1"} },
		wrongType:       func(fx sweepFixture) []string { return []string{"yaml", "multiset", "--file", fx.empty, "a", "1"} },
		empty:           func(fx sweepFixture) []string { return []string{"yaml", "multiset", "--file", fx.yaml, "a", "1"} },
		emptyKeysReason: "writes the values given, no list or count",
		plainReason:     "confirms the write, no count line",
	},
}

// sweepFormats are the three output modes the sweep asks in, as rows of
// errorFormats so the error readers are the ones errors_stdout_test.go uses.
func sweepFormats(t *testing.T) []errorFormat {
	t.Helper()
	var out []errorFormat
	for _, f := range errorFormats {
		switch f.name {
		case "default", "json", "plain":
			out = append(out, f)
		}
	}
	if len(out) != 3 {
		t.Fatalf("errorFormats lost a sweep format: have %d of default, json, plain", len(out))
	}
	return out
}

// registeredCommands walks the tree NewRootCmd builds and returns the path of
// every command a caller can run, minus the root itself. Hidden commands are
// the moved stubs, which always fail and are covered by their own tests.
func registeredCommands() []string {
	var names []string
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			if sub.Hidden || sub.Name() == "help" || sub.Name() == "completion" {
				continue
			}
			if sub.Runnable() {
				names = append(names, strings.TrimPrefix(sub.CommandPath(), "llm-data "))
			}
			walk(sub)
		}
	}
	walk(NewRootCmd())
	sort.Strings(names)
	return names
}

// sweepTableProblems names each registered command with no table entry, each
// entry for a command that is not registered, and each case that neither runs
// nor says why it does not.
func sweepTableProblems(registered []string, table map[string]sweepEntry) []string {
	var problems []string
	seen := map[string]bool{}
	for _, name := range registered {
		seen[name] = true
		e, ok := table[name]
		if !ok {
			problems = append(problems, name+": registered in NewRootCmd but missing from sweepTable")
			continue
		}
		if e.missing == nil && e.missingReason == "" {
			problems = append(problems, name+": no nonexistent-path case and no reason")
		}
		if e.wrongType == nil && e.wrongTypeReason == "" {
			problems = append(problems, name+": no wrong-type-path case and no reason")
		}
		if e.emptyKeys == nil && e.emptyKeysReason == "" {
			problems = append(problems, name+": no empty-result keys and no reason")
		}
		if e.plainCount == "" && e.plainReason == "" {
			problems = append(problems, name+": no --plain count line and no reason")
		}
		if e.empty == nil && (e.emptyKeys != nil || e.plainCount != "") {
			problems = append(problems, name+": empty-result assertions with no empty-result run")
		}
	}
	for name := range table {
		if !seen[name] {
			problems = append(problems, name+": in sweepTable but not registered in NewRootCmd")
		}
	}
	sort.Strings(problems)
	return problems
}

// badPathProblems says what is wrong with a run that was given a bad path:
// it must exit 1 with an error document on stdout in the run's format and
// nothing on stderr. Empty means the run was honest.
func badPathProblems(f errorFormat, code int, stdout, stderr string) []string {
	var problems []string
	if code != 1 {
		problems = append(problems, fmt.Sprintf("exit %d, want 1", code))
	}
	if stderr != "" {
		problems = append(problems, fmt.Sprintf("stderr must be empty, got %q", stderr))
	}
	if _, ok := f.message(stdout); !ok {
		problems = append(problems, fmt.Sprintf("stdout is not a %s error document: %q", f.name, stdout))
	}
	return problems
}

// emptyResultProblems says what is wrong with a run on a valid input that has
// nothing to report: it must exit 0 with nothing on stderr, and carry each
// key in keys as an empty list or 0 (TOON and --json), or the plainCount line
// (--plain, when the command prints one).
func emptyResultProblems(f errorFormat, keys []string, plainCount string, code int, stdout, stderr string) []string {
	var problems []string
	if code != 0 {
		problems = append(problems, fmt.Sprintf("exit %d, want 0: %q", code, stdout))
	}
	if stderr != "" {
		problems = append(problems, fmt.Sprintf("stderr must be empty, got %q", stderr))
	}
	if strings.TrimSpace(stdout) == "" {
		problems = append(problems, "stdout is empty")
	}
	switch f.name {
	case "json":
		var doc map[string]any
		if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
			problems = append(problems, fmt.Sprintf("stdout is not a JSON object: %q", stdout))
			break
		}
		for _, k := range keys {
			if !jsonEmpty(doc, k) {
				problems = append(problems, fmt.Sprintf("--json must carry %s as [] or 0: %q", k, stdout))
			}
		}
	case "default":
		for _, k := range keys {
			if !toonEmpty(stdout, k) {
				problems = append(problems, fmt.Sprintf("TOON must carry %s as [0] or 0: %q", k, stdout))
			}
		}
	case "plain":
		if plainCount != "" && !hasLine(stdout, plainCount) {
			problems = append(problems, fmt.Sprintf("--plain must print the line %q: %q", plainCount, stdout))
		}
	}
	return problems
}

func jsonEmpty(doc map[string]any, key string) bool {
	v, ok := doc[key]
	if !ok {
		return false
	}
	switch v := v.(type) {
	case []any:
		return len(v) == 0
	case float64:
		return v == 0
	}
	return false
}

// toonEmpty reports whether TOON stdout carries key at the top level as an
// empty list (key[0]: or key[0]{cols}:) or as 0.
func toonEmpty(stdout, key string) bool {
	re := regexp.MustCompile(`^` + regexp.QuoteMeta(key) + `(\[0\](\{[^}]*\})?:\s*|: 0)$`)
	for _, line := range strings.Split(stdout, "\n") {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

func hasLine(stdout, want string) bool {
	for _, line := range strings.Split(stdout, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

// newSweepFixture builds the fixture, changes into its repository, and fixes
// what setup sees of its own binary and home so the sweep does not depend on
// the machine running it.
func newSweepFixture(t *testing.T) sweepFixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fx := sweepFixture{
		root:    root,
		missing: filepath.Join(root, "does-not-exist"),
		file:    filepath.Join(root, "file.txt"),
		empty:   filepath.Join(root, "empty"),
		repo:    filepath.Join(root, "repo"),
		yaml:    filepath.Join(root, "config.yaml"),
		blank:   filepath.Join(root, "blank.md"),
	}
	for _, d := range []string{fx.empty, filepath.Join(fx.repo, "sub")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for path, body := range map[string]string{
		fx.file:                         "hello\n",
		fx.yaml:                         "a: 1\n",
		fx.blank:                        "",
		filepath.Join(fx.repo, "a.txt"): "a\n",
	} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	const date = "2026-01-01T00:00:00Z"
	gitAt(t, fx.repo, date, "init", "-q")
	gitAt(t, fx.repo, date, "add", "a.txt")
	gitAt(t, fx.repo, date, "commit", "-q", "-m", "init")

	// The ceiling keeps git from finding a repository above root, so empty
	// is outside one on any machine.
	t.Setenv("GIT_CEILING_DIRECTORIES", root)
	t.Chdir(fx.repo)
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	stubBinary(t, true, "/usr/local/bin/llm-data")
	return fx
}

// Defends epic 18's one rule, asked of every command instead of the ones
// someone thought to check: a bad path is an error (exit 1, an error document
// on stdout in the run's format, empty stderr), never an empty success; and
// an empty result states its list or count, never drops the key. grep, tree,
// git-changes, detect and discover-tests each failed one of these questions
// before epic 18 T1-T4. The command list is read from NewRootCmd, so a new
// command fails here until it has a sweepTable entry.
func TestHonestySweep(t *testing.T) {
	registered := registeredCommands()
	if len(registered) == 0 {
		t.Fatal("NewRootCmd registers no runnable commands")
	}
	for _, p := range sweepTableProblems(registered, sweepTable) {
		t.Error(p)
	}

	fx := newSweepFixture(t)
	for _, name := range registered {
		e, ok := sweepTable[name]
		if !ok {
			continue
		}
		for _, f := range sweepFormats(t) {
			badCases := []struct {
				label string
				args  func(sweepFixture) []string
			}{
				{"nonexistent path", e.missing},
				{"wrong-type path", e.wrongType},
			}
			for _, bc := range badCases {
				if bc.args == nil {
					continue
				}
				argv := append(bc.args(fx), f.flags...)
				t.Run(name+"/"+f.name+"/"+bc.label, func(t *testing.T) {
					code, stdout, stderr := runCLI(t, argv...)
					for _, p := range badPathProblems(f, code, stdout, stderr) {
						t.Errorf("%v: %s", argv, p)
					}
				})
			}
			if e.empty == nil {
				continue
			}
			argv := append(e.empty(fx), f.flags...)
			t.Run(name+"/"+f.name+"/empty result", func(t *testing.T) {
				code, stdout, stderr := runCLI(t, argv...)
				for _, p := range emptyResultProblems(f, e.emptyKeys, e.plainCount, code, stdout, stderr) {
					t.Errorf("%v: %s", argv, p)
				}
			})
		}
	}
}

// Defends the table guard itself: a command NewRootCmd registers with no
// sweepTable entry must be reported, or "a new command is covered with no
// edit" would quietly mean "a new command is never swept".
func TestHonestySweepTableMissingCommandIsReported(t *testing.T) {
	registered := registeredCommands()
	for _, drop := range []string{"grep", "yaml get"} {
		table := map[string]sweepEntry{}
		for k, v := range sweepTable {
			if k != drop {
				table[k] = v
			}
		}
		problems := sweepTableProblems(registered, table)
		want := drop + ": registered in NewRootCmd but missing from sweepTable"
		if len(problems) != 1 || problems[0] != want {
			t.Errorf("dropping %q: problems = %q, want [%q]", drop, problems, want)
		}
	}
}

// Defends the bad-path half of the sweep itself (epic 18 T6): a command
// that swallows a path error and exits 0 must be reported by
// badPathProblems in every format, whether it prints a success-shaped
// document or a real error document with the wrong exit code. If a helper
// is loosened to accept either fake, this test goes red, so a green sweep
// cannot quietly mean a blind sweep.
func TestHonestySweepBadPathHelperRejectsSwallowedError(t *testing.T) {
	// Success-shaped stdout per format, as an empty run really prints it.
	success := map[string]string{
		"default": "files[0]:\nfile_count: 0\n",
		"json":    "{\"files\":[],\"file_count\":0}\n",
		"plain":   "FILES: 0\n",
	}
	// Real error documents per format: paired with exit 0, only the
	// exit-code check can catch the swallow, so each check is defended
	// on its own.
	errDoc := map[string]string{
		"default": "error: true\nmessage: \"no such path\"\n",
		"json":    "{\"error\":true,\"message\":\"no such path\"}\n",
		"plain":   "Error: no such path\n",
	}
	for _, f := range sweepFormats(t) {
		t.Run(f.name, func(t *testing.T) {
			problems := badPathProblems(f, 0, success[f.name], "")
			if !slices.ContainsFunc(problems, func(p string) bool { return strings.Contains(p, "exit 0, want 1") }) {
				t.Errorf("swallowed error (exit 0, %q): problems = %q, must name the exit code", success[f.name], problems)
			}
			if !slices.ContainsFunc(problems, func(p string) bool { return strings.Contains(p, "error document") }) {
				t.Errorf("swallowed error (exit 0, %q): problems = %q, must name the missing error document", success[f.name], problems)
			}
			problems = badPathProblems(f, 0, errDoc[f.name], "")
			if len(problems) != 1 || !strings.Contains(problems[0], "exit 0, want 1") {
				t.Errorf("error document with exit 0 (%q): problems = %q, want exactly the exit-code problem", errDoc[f.name], problems)
			}
			// Known-good control: an honest error run draws no problems,
			// so this test cannot pass by flagging everything.
			if problems := badPathProblems(f, 1, errDoc[f.name], ""); len(problems) != 0 {
				t.Errorf("honest error run flagged: %q", problems)
			}
		})
	}
}

// Defends the empty-result half of the sweep (epic 18 T6): a run that
// drops an empty list key, or the --plain count line, must be reported by
// emptyResultProblems. Each fake keeps one key and drops another so the
// per-key check is what fires, not a wholesale "stdout is empty". If a
// helper is loosened to accept an omitted key, this test goes red.
func TestHonestySweepEmptyResultHelperRejectsOmittedKey(t *testing.T) {
	keys := []string{"files", "file_count"}
	const plainCount = "FILES: 0"
	omitted := map[string]string{
		"default": "file_count: 0\n",
		"json":    "{\"file_count\":0}\n",
		"plain":   "done\n",
	}
	honest := map[string]string{
		"default": "files[0]:\nfile_count: 0\n",
		"json":    "{\"files\":[],\"file_count\":0}\n",
		"plain":   "FILES: 0\n",
	}
	for _, f := range sweepFormats(t) {
		t.Run(f.name, func(t *testing.T) {
			problems := emptyResultProblems(f, keys, plainCount, 0, omitted[f.name], "")
			want := "files"
			if f.name == "plain" {
				want = plainCount
			}
			if !slices.ContainsFunc(problems, func(p string) bool { return strings.Contains(p, want) }) {
				t.Errorf("omitted key (%q): problems = %q, must name %q", omitted[f.name], problems, want)
			}
			// Known-good control: an honest empty run draws no problems.
			if problems := emptyResultProblems(f, keys, plainCount, 0, honest[f.name], ""); len(problems) != 0 {
				t.Errorf("honest empty run flagged: %q", problems)
			}
		})
	}
}

// Defends the derivation: the sweep must see every visible command, including
// the yaml subcommands, and none of the hidden moved stubs.
func TestHonestySweepCoversTheTree(t *testing.T) {
	got := registeredCommands()
	for _, want := range []string{"grep", "tree", "yaml get", "yaml multiset", "setup"} {
		if !slices.Contains(got, want) {
			t.Errorf("registeredCommands() = %q, missing %q", got, want)
		}
	}
	for _, c := range NewRootCmd().Commands() {
		if c.Hidden && slices.Contains(got, c.Name()) {
			t.Errorf("registeredCommands() includes hidden stub %q", c.Name())
		}
	}
}
