package commands

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/llm-data-axi/pkg/output"
	"github.com/spf13/cobra"
)

// modeFixture is a committed repository with one of everything a command in
// the table below reads, so each command has real data to print.
func modeFixture(t *testing.T) string {
	t.Helper()
	dir := outsideAnyRepo(t)
	files := map[string]string{
		"go.mod":        "module example.com/m\n\ngo 1.22\n",
		"a.go":          "package m\n\n// needle\nfunc A() {}\n",
		"a_test.go":     "package m\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n",
		"config.yaml":   "name: demo\nowner: sam\n",
		"tiers.yaml":    "packages:\n  cobra: core\n",
		"greet.tmpl":    "hello [[who]]\n",
		"plan/plan.md":  "# Plan\n\n- [ ] one\n- [x] two\n",
		"change.diff":   "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1 +1,2 @@\n package m\n+// TODO later\n",
		"items/01_a.md": "a\n",
		"items/02_b.md": "b\n",
	}
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	const date = "2026-01-01T00:00:00Z"
	gitAt(t, dir, date, "init", "-q")
	gitAt(t, dir, date, "add", ".")
	gitAt(t, dir, date, "commit", "-q", "-m", "init")
	return dir
}

// modeCase is one command line, and the constructor that runs the same
// command outside the root, which is how it printed before TOON was the
// default: no root, so nothing publishes an output mode.
type modeCase struct {
	name   string
	args   []string
	direct func() *cobra.Command
}

func modeCases() []modeCase {
	yaml := func(args ...string) modeCase {
		return modeCase{"yaml " + args[0], append([]string{"yaml"}, args...), newYamlCmd}
	}
	cases := []modeCase{
		// The home view has no constructor of its own; nil marks it.
		{"home", nil, nil},
		{"catfiles", []string{"catfiles", "a.go"}, newCatfilesCmd},
		{"count", []string{"count", "--path", "plan", "--mode", "checkboxes", "-r"}, newCountCmd},
		{"detect", []string{"detect"}, newDetectCmd},
		{"diff-smell", []string{"diff-smell", "--diff", "change.diff"}, newDiffSmellCmd},
		{"discover-tests", []string{"discover-tests"}, newDiscoverTestsCmd},
		{"git-changes", []string{"git-changes"}, newGitChangesCmd},
		{"git-context", []string{"git-context", "--max-commits", "1"}, newGitContextCmd},
		{"grep", []string{"grep", "needle", "."}, newGrepCmd},
		{"highest", []string{"highest", "--path", "items"}, newHighestCmd},
		{"math", []string{"math", "2+2"}, newMathCmd},
		{"multiexists", []string{"multiexists", "a.go", "go.mod"}, newMultiexistsCmd},
		{"multigrep", []string{"multigrep", "--path", ".", "--keywords", "needle,func"}, newMultigrepCmd},
		{"plan-type", []string{"plan-type", "--path", "plan"}, newPlanTypeCmd},
		{"project-components", []string{"project-components", "--file", "config.yaml"}, newProjectComponentsCmd},
		{"repo-root", []string{"repo-root"}, newRepoRootCmd},
		{"runtime", []string{"runtime", "--start", "1000", "--end", "4600"}, newRuntimeCmd},
		{"setup", []string{"setup", "--dry-run"}, newSetupCmd},
		{"stats", []string{"stats", "--path", "plan"}, newStatsCmd},
		{"template", []string{"template", "greet.tmpl", "--var", "who=sam", "--syntax", "brackets"}, newTemplateCmd},
		{"tier-classifier", []string{"tier-classifier", "--packages", "cobra", "--config", "tiers.yaml"}, newTierClassifierCmd},
		{"tree", []string{"tree", "--path", "plan"}, newTreeCmd},
		yaml("get", "name", "--file", "config.yaml"),
		yaml("multiget", "name", "owner", "--file", "config.yaml"),
		yaml("set", "name", "other", "--file", "config.yaml", "--dry-run"),
		yaml("multiset", "name", "other", "--file", "config.yaml", "--dry-run"),
	}
	return cases
}

// runDirect runs a command constructor without the root, with no output mode
// published: the text a command printed before TOON became the default.
func runDirect(t *testing.T, newCmd func() *cobra.Command, args []string) string {
	t.Helper()
	GlobalAXIOutput, GlobalJSONOutput, GlobalMinOutput = false, false, false
	output.SetDefaultAXI(false)
	cmd := newCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("direct %v: %v", args, err)
	}
	return buf.String()
}

// mustRun drives run as main does and fails unless it exits 0 with stdout.
func mustRun(t *testing.T, args ...string) string {
	t.Helper()
	code, stdout, stderr := runCLI(t, args...)
	if code != 0 {
		t.Fatalf("%v: exit %d\nstdout: %s\nstderr: %s", args, code, stdout, stderr)
	}
	if strings.TrimSpace(stdout) == "" {
		t.Fatalf("%v: exit 0 with nothing on stdout", args)
	}
	return stdout
}

// toonOf is the TOON encoding of a --json document. TOON output that equals
// it is the same payload as --json, in TOON, which no prose that merely
// happens to contain colons can match.
func toonOf(t *testing.T, js string) string {
	t.Helper()
	buf := new(bytes.Buffer)
	if err := output.EncodeAXI(buf, json.RawMessage(js)); err != nil {
		t.Fatalf("encode %q: %v", js, err)
	}
	return buf.String()
}

// Defends epic 11 T1: TOON is the default output. Before it, a run with no
// format flag printed prose, and an agent following the AXI contract had to
// know to ask for --axi. Every command is driven through run, the path main
// takes, in every mode:
//
//   - no flag prints TOON, and the same bytes as --axi (the alias);
//   - --plain prints exactly what the command printed before the change;
//   - --json prints JSON;
//   - --min under TOON is a no-op, byte for byte.
func TestEveryCommandDefaultsToTOON(t *testing.T) {
	fakeExecutable(t)
	dir := modeFixture(t)
	for _, c := range modeCases() {
		t.Run(c.name, func(t *testing.T) {
			chdirTo(t, dir)
			bare := mustRun(t, c.args...)
			js := mustRun(t, append(c.args, "--json")...)
			if !json.Valid([]byte(js)) {
				t.Fatalf("--json: want JSON, got:\n%s", js)
			}
			if want := toonOf(t, js); bare != want {
				t.Errorf("no format flag: want the --json payload as TOON\ngot:\n%s\nwant:\n%s", bare, want)
			}
			if axi := mustRun(t, append(c.args, "--axi")...); axi != bare {
				t.Errorf("--axi differs from the default:\n--axi:\n%s\ndefault:\n%s", axi, bare)
			}
			if minOut := mustRun(t, append(c.args, "--min")...); minOut != bare {
				t.Errorf("--min changed TOON output:\n--min:\n%s\ndefault:\n%s", minOut, bare)
			}
			if axiMin := mustRun(t, append(c.args, "--axi", "--min")...); axiMin != bare {
				t.Errorf("--axi --min changed TOON output:\n%s", axiMin)
			}
			plain := mustRun(t, append(c.args, "--plain")...)
			// The home view's repository lines vary with the fixture; its
			// first two lines are fixed, and home_test.go covers the rest.
			want := "bin: ~/go/bin/llm-data\ndescription: " + homeDescription + "\n"
			matches := strings.HasPrefix(plain, want)
			if c.direct != nil {
				want = runDirect(t, c.direct, c.args[1:])
				matches = plain == want
			}
			if !matches {
				t.Errorf("--plain is not the pre-TOON text:\n--plain:\n%s\nwant:\n%s", plain, want)
			}
			if plain == bare {
				t.Errorf("--plain printed the TOON document:\n%s", plain)
			}
		})
	}
}

// Defends the rule that nothing is accepted and silently ignored: --plain asks
// for text and --json or --axi for a structured format, and no output can be
// both. Picking a winner would hand one of the two requests something it did
// not ask for with exit 0. The run is rejected as a usage error instead, in
// the format of the structured flag, naming both flags.
func TestPlainConflictsWithStructuredFormats(t *testing.T) {
	fakeExecutable(t)
	chdirTo(t, modeFixture(t))
	for _, c := range []struct {
		args  []string
		other string
	}{
		{[]string{"--plain", "--json"}, "--json"},
		{[]string{"--json", "--plain"}, "--json"},
		{[]string{"math", "2+2", "--plain", "--json"}, "--json"},
		{[]string{"--plain", "math", "2+2", "--json"}, "--json"},
		{[]string{"--plain", "--axi"}, "--axi"},
		{[]string{"math", "2+2", "--axi", "--plain"}, "--axi"},
		{[]string{"yaml", "get", "name", "--file", "config.yaml", "--plain", "--axi"}, "--axi"},
		{[]string{"diff-smell", "--diff", "change.diff", "--plain", "--json"}, "--json"},
	} {
		code, stdout, stderr := runCLI(t, c.args...)
		if code != 2 {
			t.Errorf("%v: exit %d, want 2 (usage)", c.args, code)
		}
		if stderr != "" {
			t.Errorf("%v: stderr must be empty, got %q", c.args, stderr)
		}
		for _, flag := range []string{"--plain", c.other} {
			if !strings.Contains(stdout, flag) {
				t.Errorf("%v: stdout must name %s, got %q", c.args, flag, stdout)
			}
		}
		if c.other == "--json" {
			var e struct {
				Error   bool   `json:"error"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal([]byte(stdout), &e); err != nil || !e.Error {
				t.Errorf("%v: want a JSON error on stdout, got %q", c.args, stdout)
			}
		} else if !strings.HasPrefix(stdout, "error: true\n") {
			t.Errorf("%v: want a TOON error on stdout, got %q", c.args, stdout)
		}
		if strings.Contains(stdout, "bin:") || strings.Contains(stdout, "result") {
			t.Errorf("%v: the command ran despite the conflict:\n%s", c.args, stdout)
		}
	}
}

// Defends ExitCodeFor classifying the conflict as a usage error by type, not
// by the wording of its message.
func TestFormatConflictIsAUsageError(t *testing.T) {
	err := formatConflict(true, true, false)
	if err == nil {
		t.Fatal("--plain with --json was accepted")
	}
	if got := output.ExitCodeFor(err); got != 2 {
		t.Errorf("ExitCodeFor = %d, want 2", got)
	}
	if err := formatConflict(true, false, true); err == nil || !strings.Contains(err.Error(), "--axi") {
		t.Errorf("--plain with --axi: got %v", err)
	}
	for _, ok := range [][3]bool{{true, false, false}, {false, true, false}, {false, false, true}, {false, true, true}, {false, false, false}} {
		if err := formatConflict(ok[0], ok[1], ok[2]); err != nil {
			t.Errorf("formatConflict%v = %v, want nil", ok, err)
		}
	}
}
