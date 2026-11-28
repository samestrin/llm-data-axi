package commands

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The expected entries are spelled out here rather than read from the
// production variables, so a change to what an agent is told to do next is a
// deliberate edit in two places and not a silent one in one.
const (
	helpCountCheckboxRec   = "Add -r to count checkboxes in subdirectories too"
	helpCountCheckboxStyle = "Add --style all to count list, heading and table checkboxes"
	helpCountCheckboxGrep  = "Run `llm-data grep '\\[' <path>` to see the checkbox lines in a file"
	helpCountLinesRec      = "Add -r to count lines in subdirectories too"
	helpCountLinesCat      = "Run `llm-data catfiles <path>` to check the files have content"
	helpCountFilesPattern  = "Drop --pattern to count every file"
	helpCountFilesRec      = "Add -r to count files in subdirectories too"
	helpCountFilesTree     = "Run `llm-data tree --path <dir>` to see what the directory holds"
	helpHighestPattern     = "Add --pattern <regex> to match a different numbering scheme"
	helpHighestType        = "Add --type both to search files and directories"
	helpHighestTree        = "Run `llm-data tree --path <dir>` to see what the directory holds"
)

// runZeroOK runs the CLI in-process and fails on a non-zero exit: a zero
// result is a successful run, not an error.
func runZeroOK(t *testing.T, args ...string) string {
	t.Helper()
	code, stdout, stderr := runCLI(t, args...)
	if code != 0 {
		t.Fatalf("%v exited %d\nstdout:\n%s\nstderr:\n%s", args, code, stdout, stderr)
	}
	return stdout
}

// zeroHelpJSON asserts the exact help list of a --json run.
func zeroHelpJSON(t *testing.T, want []string, args ...string) {
	t.Helper()
	help, ok := jsonHelp(t, runZeroOK(t, append(args, "--json")...))
	if !ok || !reflect.DeepEqual(help, want) {
		t.Errorf("%v --json help = %q (present %v), want %q", args, help, ok, want)
	}
}

// zeroHelpAbsent asserts a run carries no help key in any encoding: the
// result is non-empty, so next steps there are noise an agent pays tokens
// to read.
func zeroHelpAbsent(t *testing.T, args ...string) {
	t.Helper()
	for _, extra := range [][]string{{"--json"}, {"--json", "--min"}} {
		if help, ok := jsonHelp(t, runZeroOK(t, append(args, extra...)...)); ok {
			t.Errorf("%v %v carried help %q", args, extra, help)
		}
	}
	if out := runZeroOK(t, append(args, "--axi")...); hasHelpKey(out) {
		t.Errorf("%v --axi carried help:\n%s", args, out)
	}
}

// zeroWriteFile writes content to dir/name, failing the test on error.
func zeroWriteFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// Defends epic 22 T2: a checkbox count of zero handing an agent a way
// forward instead of a bare COUNT: 0 dead end. Each assertion pins the exact
// text, so removing the emptyHelp call fails here.
func TestCountCheckboxZeroCarriesHelp(t *testing.T) {
	file := zeroWriteFile(t, t.TempDir(), "notes.md", "# Nothing to do here\n")
	args := []string{"count", "--path", file, "--mode", "checkboxes"}
	want := []string{helpCountCheckboxGrep}

	zeroHelpJSON(t, want, args...)
	zeroHelpJSON(t, want, append(args, "--min")...)
	assertAXIHelp(t, runZeroOK(t, append(args, "--axi")...), want)

	// Plain output stays help-free, as grep and the home view are, and still
	// states the zero.
	out := runZeroOK(t, append(args, "--plain")...)
	if hasHelpKey(out) || !strings.Contains(out, "COUNT: 0") {
		t.Errorf("--plain zero checkbox count = %q, want COUNT: 0 and no help key", out)
	}
}

// Defends the zero-count hints staying actionable: -r is only suggested for
// a directory not already walked recursively, and --style all only when a
// style filter could be hiding the checkboxes. Telling an agent to add a
// flag it already passed is advice it cannot act on (the grep precedent).
func TestCountCheckboxZeroHelpOmitsFlagsAlreadyGiven(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	zeroWriteFile(t, sub, "a.md", "no checkboxes here\n")

	base := []string{"count", "--path", dir, "--mode", "checkboxes"}
	zeroHelpJSON(t, []string{helpCountCheckboxRec, helpCountCheckboxStyle, helpCountCheckboxGrep},
		append(base, "--style", "list")...)
	zeroHelpJSON(t, []string{helpCountCheckboxStyle, helpCountCheckboxGrep},
		append(base, "--style", "list", "-r")...)
	zeroHelpJSON(t, []string{helpCountCheckboxGrep}, append(base, "-r")...)
}

// Defends a checkbox count that found something staying clean: no help key
// on a non-empty result, in either encoding and under --min.
func TestCountCheckboxNonEmptyCarriesNoHelp(t *testing.T) {
	file := zeroWriteFile(t, t.TempDir(), "todo.md", "- [x] done\n- [ ] todo\n")
	zeroHelpAbsent(t, "count", "--path", file, "--mode", "checkboxes")
}

// Defends a line count of zero handing an agent a way forward: the files
// are empty or the walk missed them, and -r plus a content check are the
// next steps.
func TestCountLinesZeroCarriesHelp(t *testing.T) {
	dir := t.TempDir()
	file := zeroWriteFile(t, dir, "empty.txt", "")

	want := []string{helpCountLinesCat}
	zeroHelpJSON(t, want, "count", "--path", file, "--mode", "lines")
	assertAXIHelp(t, runZeroOK(t, "count", "--path", file, "--mode", "lines", "--axi"), want)

	zeroHelpJSON(t, []string{helpCountLinesRec, helpCountLinesCat},
		"count", "--path", dir, "--mode", "lines")
	zeroHelpJSON(t, want, "count", "--path", dir, "--mode", "lines", "-r")
	zeroHelpJSON(t, want, append([]string{"count", "--path", file, "--mode", "lines"}, "--min")...)

	out := runZeroOK(t, "count", "--path", file, "--mode", "lines", "--plain")
	if hasHelpKey(out) || !strings.Contains(out, "COUNT: 0") {
		t.Errorf("--plain zero line count = %q, want COUNT: 0 and no help key", out)
	}
}

// Defends a line count that found lines staying clean.
func TestCountLinesNonEmptyCarriesNoHelp(t *testing.T) {
	file := zeroWriteFile(t, t.TempDir(), "lines.txt", "a\nb\n")
	zeroHelpAbsent(t, "count", "--path", file, "--mode", "lines")
}

// Defends a file count of zero handing an agent a way forward: the pattern
// or the missing recursion are the usual reasons, and a tree listing shows
// what is actually there.
func TestCountFilesZeroCarriesHelp(t *testing.T) {
	empty := t.TempDir()
	zeroHelpJSON(t, []string{helpCountFilesRec, helpCountFilesTree},
		"count", "--path", empty, "--mode", "files")
	zeroHelpJSON(t, []string{helpCountFilesTree},
		"count", "--path", empty, "--mode", "files", "-r")
	assertAXIHelp(t, runZeroOK(t, "count", "--path", empty, "--mode", "files", "--axi"),
		[]string{helpCountFilesRec, helpCountFilesTree})

	dir := t.TempDir()
	zeroWriteFile(t, dir, "a.txt", "x\n")
	zeroHelpJSON(t, []string{helpCountFilesPattern, helpCountFilesRec, helpCountFilesTree},
		"count", "--path", dir, "--mode", "files", "--pattern", "*.go")
	zeroHelpJSON(t, []string{helpCountFilesPattern, helpCountFilesTree},
		"count", "--path", dir, "--mode", "files", "--pattern", "*.go", "-r")
	zeroHelpJSON(t, []string{helpCountFilesRec, helpCountFilesTree},
		append([]string{"count", "--path", empty, "--mode", "files"}, "--min")...)

	out := runZeroOK(t, "count", "--path", empty, "--mode", "files", "--plain")
	if hasHelpKey(out) || !strings.Contains(out, "COUNT: 0") {
		t.Errorf("--plain zero file count = %q, want COUNT: 0 and no help key", out)
	}
}

// Defends a file count that found files staying clean, including the
// single-file target that always counts 1.
func TestCountFilesNonEmptyCarriesNoHelp(t *testing.T) {
	dir := t.TempDir()
	file := zeroWriteFile(t, dir, "a.txt", "x\n")
	zeroHelpAbsent(t, "count", "--path", dir, "--mode", "files")
	zeroHelpAbsent(t, "count", "--path", file, "--mode", "files")
}

// Defends highest with no numbered match handing an agent a way forward:
// the pattern is the usual reason nothing matched, and a tree listing shows
// what the directory holds. The result already says NEXT: 1; the help says
// how to check that zero is real.
func TestHighestZeroCarriesHelp(t *testing.T) {
	dir := t.TempDir()
	zeroWriteFile(t, dir, "notes.txt", "x\n")
	args := []string{"highest", "--path", dir}
	want := []string{helpHighestPattern, helpHighestTree}

	zeroHelpJSON(t, want, args...)
	zeroHelpJSON(t, want, append(args, "--min")...)
	assertAXIHelp(t, runZeroOK(t, append(args, "--axi")...), want)

	out := runZeroOK(t, append(args, "--plain")...)
	if hasHelpKey(out) || !strings.Contains(out, "COUNT: 0") || !strings.Contains(out, "NEXT: 1") {
		t.Errorf("--plain zero highest = %q, want COUNT: 0, NEXT: 1 and no help key", out)
	}
}

// Defends the highest hints staying actionable: --pattern is only suggested
// when the caller did not pass one, and --type both only when a type filter
// could be hiding the numbered entries.
func TestHighestZeroHelpOmitsFlagsAlreadyGiven(t *testing.T) {
	dir := t.TempDir()
	zeroWriteFile(t, dir, "1.0_plan.md", "x\n")

	// A numbered file exists, but --type dir hides it.
	zeroHelpJSON(t, []string{helpHighestPattern, helpHighestType, helpHighestTree},
		"highest", "--path", dir, "--type", "dir")
	zeroHelpJSON(t, []string{helpHighestType, helpHighestTree},
		"highest", "--path", dir, "--type", "dir", "--pattern", `^(\d+)\.`)
	zeroHelpJSON(t, []string{helpHighestTree},
		"highest", "--path", dir, "--pattern", "^no-match")
}

// Defends a highest run that found numbered entries staying clean.
func TestHighestNonEmptyCarriesNoHelp(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "1.0_first"), 0o755); err != nil {
		t.Fatal(err)
	}
	zeroHelpAbsent(t, "highest", "--path", dir)
}