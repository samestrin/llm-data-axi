package commands

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// Tests for the EST-6854 AC5 review findings (EST-6876). Each names the
// finding it defends.

// rootCmdForTest is the full command tree with the output-mode globals
// restored when the test ends, for tests that pass root flags such as --json
// to a subcommand.
func rootCmdForTest(t *testing.T) *cobra.Command {
	t.Helper()
	restoreOutputMode(t)
	return NewRootCmd()
}

// decodeOne decodes stdout as exactly one JSON document. A second document
// after the first is the failure finding 1 describes, and json.Unmarshal
// alone would report it only as "invalid character".
func decodeOne(t *testing.T, stdout string) map[string]any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(stdout))
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if dec.More() {
		t.Fatalf("stdout holds more than one JSON document:\n%s", stdout)
	}
	return doc
}

// Finding 1: `multiexists a --json` printed the full result and then an error
// object, two documents on one stream, so a JSON parser failed on the output
// of the very case a caller runs multiexists to detect.
func TestMultiexistsMissingIsOneErrorDocument(t *testing.T) {
	dir := t.TempDir()
	present := filepath.Join(dir, "here.txt")
	if err := os.WriteFile(present, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "gone.txt")

	code, stdout, _ := runCLI(t, "multiexists", present, missing, "--json")
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	doc := decodeOne(t, stdout)
	if doc["error"] != true {
		t.Errorf("want error: true, got %v", doc)
	}
	got, _ := doc["missing"].([]any)
	if len(got) != 1 || got[0] != missing {
		t.Errorf("missing = %v, want [%s]", doc["missing"], missing)
	}
	if doc["exist_count"] != float64(1) || doc["missing_count"] != float64(1) {
		t.Errorf("want exist_count 1 and missing_count 1, got %v", doc)
	}
	if !strings.Contains(doc["message"].(string), missing) {
		t.Errorf("message must name the missing path for --plain readers: %q", doc["message"])
	}

	// --plain carries the same through the message.
	code, stdout, _ = runCLI(t, "multiexists", present, missing, "--plain")
	if code != 1 || !strings.Contains(stdout, missing) {
		t.Errorf("--plain: exit %d, want 1 naming %s:\n%s", code, missing, stdout)
	}
}

// Finding 1, the other side: --no-fail is the caller saying a missing path is
// an answer, not a failure. It stays one success document with exit 0.
func TestMultiexistsNoFailIsOneSuccessDocument(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "gone.txt")
	code, stdout, _ := runCLI(t, "multiexists", missing, "--no-fail", "--json")
	if code != 0 {
		t.Errorf("exit %d, want 0", code)
	}
	doc := decodeOne(t, stdout)
	if doc["all_exist"] != false || doc["missing_count"] != float64(1) {
		t.Errorf("want all_exist false and missing_count 1, got %v", doc)
	}
}

// Finding 2: stats on an empty directory dropped files, directories and
// by_extension (omitempty), so "no files" and "field not reported" looked
// the same.
func TestStatsEmptyDirStatesCounts(t *testing.T) {
	dir := t.TempDir()
	code, stdout, _ := runCLI(t, "stats", "--path", dir, "--json")
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stdout)
	}
	doc := decodeOne(t, stdout)
	for _, k := range []string{"files", "directories"} {
		if v, ok := doc[k]; !ok || v != float64(0) {
			t.Errorf("%s = %v (present %v), want 0", k, v, ok)
		}
	}
	if exts, ok := doc["by_extension"].([]any); !ok || len(exts) != 0 {
		t.Errorf("by_extension = %v, want an empty list", doc["by_extension"])
	}

	_, toon, _ := runCLI(t, "stats", "--path", dir)
	for _, want := range []string{"files: 0", "directories: 0", "by_extension"} {
		if !strings.Contains(toon, want) {
			t.Errorf("TOON lacks %q:\n%s", want, toon)
		}
	}
}

// Finding 3: tree on an empty directory dropped total (omitempty), so a
// caller could not tell zero entries from a missing count.
func TestTreeEmptyDirStatesTotal(t *testing.T) {
	dir := t.TempDir()
	code, stdout, _ := runCLI(t, "tree", "--path", dir, "--json")
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stdout)
	}
	doc := decodeOne(t, stdout)
	if v, ok := doc["total"]; !ok || v != float64(0) {
		t.Errorf("total = %v (present %v), want 0", v, ok)
	}
	_, toon, _ := runCLI(t, "tree", "--path", dir)
	if !strings.Contains(toon, "total: 0") {
		t.Errorf("TOON lacks total: 0:\n%s", toon)
	}
}

// Finding 4: catfiles on an empty directory was an error, exit 1, when
// nothing failed: the directory was read and holds no files. That is an
// empty result, stated as file_count 0.
func TestCatfilesEmptyDirIsZeroFiles(t *testing.T) {
	dir := t.TempDir()
	code, stdout, _ := runCLI(t, "catfiles", dir, "--json")
	if code != 0 {
		t.Fatalf("exit %d, want 0\n%s", code, stdout)
	}
	doc := decodeOne(t, stdout)
	if v, ok := doc["file_count"]; !ok || v != float64(0) {
		t.Errorf("file_count = %v (present %v), want 0", v, ok)
	}
	if v, ok := doc["total_size"]; !ok || v != float64(0) {
		t.Errorf("total_size = %v (present %v), want 0", v, ok)
	}

	_, toon, _ := runCLI(t, "catfiles", dir)
	if !strings.Contains(toon, "file_count: 0") {
		t.Errorf("TOON lacks file_count: 0:\n%s", toon)
	}
	// --plain must say it too, or exit 0 with an empty stdout reads as nothing
	// having run.
	_, plain, _ := runCLI(t, "catfiles", dir, "--plain")
	if !strings.Contains(plain, "FILES: 0") {
		t.Errorf("--plain lacks FILES: 0:\n%q", plain)
	}
}

// smellyDiff adds a suppression whose line is longer than any row should be.
const smellyDiff = "diff --git a/svc.go b/svc.go\n--- a/svc.go\n+++ b/svc.go\n@@ -1,1 +1,2 @@\n package m\n+var x = 1 //nolint:all because this line is long enough to bloat every row of output\n"

func writeSmellyDiff(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "fix.diff")
	if err := os.WriteFile(p, []byte(smellyDiff), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// Finding 5: every diff-smell row carried evidence, the whole added line,
// uncut, so the default output was five fields wide with the widest one
// unbounded. The default row is type, file, line, severity; --full adds the
// evidence, and the output says so.
func TestDiffSmellDefaultRowHasNoEvidence(t *testing.T) {
	p := writeSmellyDiff(t)
	code, stdout, _ := runCLI(t, "diff-smell", "--diff", p, "--json")
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stdout)
	}
	doc := decodeOne(t, stdout)
	smells, _ := doc["smells"].([]any)
	if len(smells) == 0 {
		t.Fatalf("want a suppression smell, got %v", doc)
	}
	for _, s := range smells {
		var keys []string
		for k := range s.(map[string]any) {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		if !slices.Equal(keys, []string{"file", "line", "severity", "type"}) {
			t.Errorf("default row keys = %v, want file, line, severity, type", keys)
		}
	}
	help, _ := doc["help"].([]any)
	if len(help) == 0 || !strings.Contains(help[0].(string), "--full") {
		t.Errorf("default output must offer --full for the evidence, help = %v", doc["help"])
	}

	code, full, _ := runCLI(t, "diff-smell", "--diff", p, "--json", "--full")
	if code != 0 {
		t.Fatalf("--full: exit %d\n%s", code, full)
	}
	doc = decodeOne(t, full)
	row := doc["smells"].([]any)[0].(map[string]any)
	if ev, _ := row["evidence"].(string); !strings.Contains(ev, "//nolint:all") {
		t.Errorf("--full row evidence = %q, want the added line", ev)
	}
	if _, ok := doc["help"]; ok {
		t.Errorf("--full must not offer --full again: %v", doc["help"])
	}
}

// Finding 5: a test_only smell has no line of its own. The row still states
// line, as 0, so every row has the same four fields.
func TestDiffSmellFileLevelSmellStatesLineZero(t *testing.T) {
	p := filepath.Join(t.TempDir(), "fix.diff")
	diff := "diff --git a/foo_test.go b/foo_test.go\n--- a/foo_test.go\n+++ b/foo_test.go\n@@ -1,1 +1,2 @@\n package main\n+// relax\n"
	if err := os.WriteFile(p, []byte(diff), 0o644); err != nil {
		t.Fatal(err)
	}
	_, stdout, _ := runCLI(t, "diff-smell", "--diff", p, "--json")
	doc := decodeOne(t, stdout)
	row := doc["smells"].([]any)[0].(map[string]any)
	if v, ok := row["line"]; !ok || v != float64(0) {
		t.Errorf("test_only line = %v (present %v), want 0", v, ok)
	}
}

// Finding 6: `diff-smell --diff -` read stdin with io.ReadAll and no terminal
// check, so on a TTY it sat waiting for input: a prompt in all but name. It
// refuses a terminal the way the yaml stdin reader does. /dev/null is a
// character device, which is what the check looks for, and it reads as empty
// instantly, so before the fix this ran to a "clean" verdict instead.
func TestDiffSmellStdinRefusesTerminal(t *testing.T) {
	tty, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer tty.Close()
	root := rootCmdForTest(t)
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(new(bytes.Buffer))
	root.SetIn(tty)
	root.SetArgs([]string{"diff-smell", "--diff", "-", "--json"})
	err = root.Execute()
	if err == nil || !strings.Contains(err.Error(), "no data piped to stdin") {
		t.Fatalf("want the stdin-is-a-terminal refusal, got %v\n%s", err, out.String())
	}
	if out.Len() != 0 {
		t.Errorf("a refused run printed a result: %s", out.String())
	}
}

// Finding 6: a pipe is still read.
func TestDiffSmellStdinReadsPipe(t *testing.T) {
	root := rootCmdForTest(t)
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(new(bytes.Buffer))
	root.SetIn(strings.NewReader(smellyDiff))
	root.SetArgs([]string{"diff-smell", "--diff", "-", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if doc := decodeOne(t, out.String()); doc["summary"].(map[string]any)["verdict"] != "soft_only" {
		t.Errorf("want soft_only from the piped diff, got %v", doc)
	}
}

// Finding 7: tier-classifier and diff-smell said "Output is JSON" (it is
// TOON by default) and registered their own --json and --min, which hid the
// root's flags of the same name and their real descriptions from --help.
func TestTierClassifierAndDiffSmellUseRootFormatFlags(t *testing.T) {
	for _, name := range []string{"tier-classifier", "diff-smell"} {
		root := rootCmdForTest(t)
		cmd, _, err := root.Find([]string{name})
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range []string{"json", "min"} {
			if cmd.LocalNonPersistentFlags().Lookup(f) != nil {
				t.Errorf("%s registers its own --%s, hiding the root flag", name, f)
			}
		}
		code, help, _ := runCLI(t, name, "--help")
		if code != 0 {
			t.Fatalf("%s --help: exit %d", name, code)
		}
		if strings.Contains(help, "Output is JSON") || strings.Contains(help, "Output JSON:") {
			t.Errorf("%s --help still claims JSON output:\n%s", name, help)
		}
		if !strings.Contains(help, "Output as JSON instead of TOON") {
			t.Errorf("%s --help does not show the root --json:\n%s", name, help)
		}
	}

	// The root flags still reach both commands.
	if _, stdout, _ := runCLI(t, "tier-classifier", "--packages", "a", "--json"); !json.Valid([]byte(stdout)) {
		t.Errorf("tier-classifier --json is not JSON:\n%s", stdout)
	}
	if _, stdout, _ := runCLI(t, "tier-classifier", "--packages", "a", "--json", "--min"); strings.Contains(strings.TrimSpace(stdout), "\n") {
		t.Errorf("tier-classifier --json --min is not one line:\n%s", stdout)
	}
	p := writeSmellyDiff(t)
	if _, stdout, _ := runCLI(t, "diff-smell", "--diff", p, "--json"); !json.Valid([]byte(stdout)) {
		t.Errorf("diff-smell --json is not JSON:\n%s", stdout)
	}
	if _, stdout, _ := runCLI(t, "diff-smell", "--diff", p, "--json", "--min"); strings.Contains(strings.TrimSpace(stdout), "\n") {
		t.Errorf("diff-smell --json --min is not one line:\n%s", stdout)
	}
}

// Finding 8: a wrong argument count exited 1, a runtime failure, while a
// missing required flag exited 2. Both are malformed invocations; retrying
// either unchanged never works, and the exit code is how a caller tells.
func TestArgumentCountErrorsExitTwo(t *testing.T) {
	for _, args := range [][]string{
		{"yaml", "set", "--file", "c.yaml", "a"},
		{"math"},
		{"grep", "onlyone"},
		{"stats", "extra"},
		{"template"},
		{"catfiles"},
		{"multiexists"},
		{"tier-classifier", "--packages", ""},
		{"tier-classifier", "--packages", " , "},
	} {
		code, stdout, _ := runCLI(t, append(args, "--json")...)
		if code != 2 {
			t.Errorf("%v: exit %d, want 2\n%s", args, code, stdout)
		}
		if doc := decodeOne(t, stdout); doc["error"] != true {
			t.Errorf("%v: want an error document, got %v", args, doc)
		}
	}
}
