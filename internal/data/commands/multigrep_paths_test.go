package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Defends multigrep's match labels when --path is a file: a match was
// labelled ".", the file's path relative to itself, so a caller could not
// tell which file it came from.
func TestMultigrepOnAFileLabelsMatchesWithTheFilePath(t *testing.T) {
	dir := realTempDir(t)
	file := filepath.Join(dir, "src.go")
	if err := os.WriteFile(file, []byte("func needle() {}\nneedle()\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := runCLI(t, "multigrep", "--path", file, "--keywords", "needle", "--json", "--absolute")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stdout)
	}
	var got struct {
		Results map[string]struct {
			FilesMatched      []string    `json:"files_matched"`
			DefinitionMatches []matchInfo `json:"definition_matches"`
			OtherMatches      []matchInfo `json:"other_matches"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("%v: %s", err, stdout)
	}
	r := got.Results["needle"]
	labels := append([]string{}, r.FilesMatched...)
	for _, m := range append(r.DefinitionMatches, r.OtherMatches...) {
		labels = append(labels, m.File)
	}
	if len(labels) < 3 {
		t.Fatalf("want a file and two matches, got %+v", r)
	}
	for _, l := range labels {
		if l != file {
			t.Errorf("label %q, want the file path %q", l, file)
		}
	}
}

// Defends --output-dir against two keywords sharing a file: "a b" and "a_b"
// both became keyword_a_b.txt, and the second write replaced the first. Each
// keyword gets its own file, and the result says which file holds which.
func TestMultigrepOutputDirGivesEveryKeywordItsOwnFile(t *testing.T) {
	dir := realTempDir(t)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x a b x\ny a_b y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")
	code, stdout, _ := runCLI(t, "multigrep", "--path", filepath.Join(dir, "f.txt"), "--keywords", "a b,a_b", "--output-dir", out, "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stdout)
	}
	var got struct {
		OutputFiles map[string]string `json:"output_files"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("%v: %s", err, stdout)
	}
	if len(got.OutputFiles) != 2 || got.OutputFiles["a b"] == got.OutputFiles["a_b"] {
		t.Fatalf("want two distinct output files, got %v", got.OutputFiles)
	}
	for kw, want := range map[string]string{"a b": "x a b x", "a_b": "y a_b y"} {
		b, err := os.ReadFile(got.OutputFiles[kw])
		if err != nil {
			t.Fatalf("%s: %v", kw, err)
		}
		if !strings.Contains(string(b), want) {
			t.Errorf("file for %q = %q, want its line %q", kw, b, want)
		}
	}
	// A keyword that is already a safe name keeps the plain file name.
	if filepath.Base(got.OutputFiles["a_b"]) != "keyword_a_b.txt" {
		t.Errorf("a_b's file = %s, want keyword_a_b.txt", got.OutputFiles["a_b"])
	}
}
