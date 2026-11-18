package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Tests for the EST-6854 AC5 review findings (EST-6876). Each names the
// finding it defends.

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

// Findings 2 and 4 under --json --min: dropping omitempty from the shared
// dual-key structs put the long keys (file_count, total_size, files) next to
// the short ones in minimal output, the leak 1.1.0 fixed for multigrep. The
// minimal shape carries only the short keys, and still states its zeros.
func TestMinimalJSONEmptyDirShortKeysOnly(t *testing.T) {
	empty := t.TempDir()
	full := t.TempDir()
	if err := os.WriteFile(filepath.Join(full, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		args       []string
		want, deny []string
	}{
		{[]string{"catfiles", empty}, []string{"fc", "ts"}, []string{"file_count", "total_size", "files", "fs"}},
		{[]string{"catfiles", full}, []string{"f", "fc", "ts"}, []string{"file_count", "total_size", "files", "fs"}},
		{[]string{"stats", "--path", empty}, []string{"f", "d", "ext"}, []string{"files", "directories", "by_extension", "fs", "dirs"}},
	} {
		code, stdout, _ := runCLI(t, append(c.args, "--json", "--min")...)
		if code != 0 {
			t.Fatalf("%v: exit %d\n%s", c.args, code, stdout)
		}
		doc := decodeOne(t, stdout)
		for _, k := range c.want {
			if _, ok := doc[k]; !ok {
				t.Errorf("%v --min: missing %q in %s", c.args, k, stdout)
			}
		}
		for _, k := range c.deny {
			if _, ok := doc[k]; ok {
				t.Errorf("%v --min: long key %q leaked: %s", c.args, k, stdout)
			}
		}
	}
}
