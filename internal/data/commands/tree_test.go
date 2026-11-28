package commands

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// resetTreeFlags resets all tree command flags to defaults
func resetTreeFlags() {
	treeDepth = 999
	treeSizes = false
	treeNoGitignore = false
	treeNoDefaultExcludes = false
	treePath = "."
	treeJSON = false
	treeMinimal = false
	treeMaxEntries = 500
	treeExcludePatterns = nil
}

func TestTreeCommand(t *testing.T) {
	tmpDir := t.TempDir()

	// Create directory structure
	os.Mkdir(filepath.Join(tmpDir, "subdir"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("content"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "subdir", "nested.txt"), []byte("nested"), 0644)

	tests := []struct {
		name     string
		args     []string
		expected []string
		hasError bool
	}{
		{
			name:     "basic tree",
			args:     []string{"--path", tmpDir, "--no-gitignore"},
			expected: []string{"file1.txt", "subdir/"},
		},
		{
			name:     "tree with sizes",
			args:     []string{"--path", tmpDir, "--sizes", "--no-gitignore"},
			expected: []string{"file1.txt"},
		},
		{
			name:     "tree with depth limit",
			args:     []string{"--path", tmpDir, "--depth", "1", "--no-gitignore"},
			expected: []string{"subdir/"},
		},
		{
			name:     "non-existent path",
			args:     []string{"--path", "/nonexistent/path"},
			hasError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetTreeFlags()
			cmd := newTreeCmd()
			buf := new(bytes.Buffer)
			errBuf := new(bytes.Buffer)
			cmd.SetOut(buf)
			cmd.SetErr(errBuf)
			cmd.SetArgs(tt.args)

			err := cmd.Execute()

			if tt.hasError {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			output := buf.String()
			for _, exp := range tt.expected {
				if !strings.Contains(output, exp) {
					t.Errorf("output %q should contain %q", output, exp)
				}
			}
		})
	}
}

func TestTreeMaxEntries(t *testing.T) {
	tmpDir := t.TempDir()

	// Create many files
	for i := 0; i < 20; i++ {
		os.WriteFile(filepath.Join(tmpDir, "file"+string(rune('a'+i))+".txt"), []byte("content"), 0644)
	}

	t.Run("truncates at max entries", func(t *testing.T) {
		resetTreeFlags()
		cmd := newTreeCmd()
		buf := new(bytes.Buffer)
		cmd.SetOut(buf)
		cmd.SetArgs([]string{"--path", tmpDir, "--max-entries", "5", "--no-gitignore", "--no-default-excludes"})

		err := cmd.Execute()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, "truncated") {
			t.Error("expected truncation message")
		}
	})

	t.Run("json output includes truncated flag", func(t *testing.T) {
		resetTreeFlags()
		cmd := newTreeCmd()
		buf := new(bytes.Buffer)
		cmd.SetOut(buf)
		cmd.SetArgs([]string{"--path", tmpDir, "--max-entries", "5", "--no-gitignore", "--no-default-excludes", "--json"})

		err := cmd.Execute()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var result TreeResult
		if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
			t.Fatalf("failed to parse JSON: %v", err)
		}

		if !result.Truncated {
			t.Error("expected Truncated to be true")
		}
		if result.Total != 5 {
			t.Errorf("expected Total 5, got %d", result.Total)
		}
	})

	t.Run("unlimited when max-entries is 0", func(t *testing.T) {
		resetTreeFlags()
		cmd := newTreeCmd()
		buf := new(bytes.Buffer)
		cmd.SetOut(buf)
		cmd.SetArgs([]string{"--path", tmpDir, "--max-entries", "0", "--no-gitignore", "--no-default-excludes"})

		err := cmd.Execute()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		output := buf.String()
		if strings.Contains(output, "truncated") {
			t.Error("should not truncate when max-entries is 0")
		}
	})
}

func TestTreeDefaultExcludes(t *testing.T) {
	tmpDir := t.TempDir()

	// Create directories that should be excluded by default
	os.Mkdir(filepath.Join(tmpDir, "node_modules"), 0755)
	os.Mkdir(filepath.Join(tmpDir, "vendor"), 0755)
	os.Mkdir(filepath.Join(tmpDir, "__pycache__"), 0755)
	os.Mkdir(filepath.Join(tmpDir, "src"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "node_modules", "package.json"), []byte("{}"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "src", "main.go"), []byte("package main"), 0644)

	t.Run("excludes default directories", func(t *testing.T) {
		resetTreeFlags()
		cmd := newTreeCmd()
		buf := new(bytes.Buffer)
		cmd.SetOut(buf)
		cmd.SetArgs([]string{"--path", tmpDir, "--no-gitignore"})

		err := cmd.Execute()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		output := buf.String()
		if strings.Contains(output, "node_modules") {
			t.Error("should exclude node_modules by default")
		}
		if strings.Contains(output, "vendor") {
			t.Error("should exclude vendor by default")
		}
		if strings.Contains(output, "__pycache__") {
			t.Error("should exclude __pycache__ by default")
		}
		if !strings.Contains(output, "src") {
			t.Error("should include src")
		}
	})

	t.Run("includes defaults with --no-default-excludes", func(t *testing.T) {
		resetTreeFlags()
		cmd := newTreeCmd()
		buf := new(bytes.Buffer)
		cmd.SetOut(buf)
		cmd.SetArgs([]string{"--path", tmpDir, "--no-gitignore", "--no-default-excludes"})

		err := cmd.Execute()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		output := buf.String()
		if !strings.Contains(output, "node_modules") {
			t.Error("should include node_modules with --no-default-excludes")
		}
		if !strings.Contains(output, "vendor") {
			t.Error("should include vendor with --no-default-excludes")
		}
	})
}

func TestTreeExcludePatterns(t *testing.T) {
	tmpDir := t.TempDir()

	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "main_test.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "util.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "util_test.go"), []byte("package main"), 0644)

	t.Run("excludes by pattern", func(t *testing.T) {
		resetTreeFlags()
		cmd := newTreeCmd()
		buf := new(bytes.Buffer)
		cmd.SetOut(buf)
		cmd.SetArgs([]string{"--path", tmpDir, "--exclude", "_test\\.go$", "--no-gitignore"})

		err := cmd.Execute()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		output := buf.String()
		if strings.Contains(output, "_test.go") {
			t.Error("should exclude _test.go files")
		}
		if !strings.Contains(output, "main.go") {
			t.Error("should include main.go")
		}
		if !strings.Contains(output, "util.go") {
			t.Error("should include util.go")
		}
	})

	t.Run("multiple exclude patterns", func(t *testing.T) {
		resetTreeFlags()
		cmd := newTreeCmd()
		buf := new(bytes.Buffer)
		cmd.SetOut(buf)
		cmd.SetArgs([]string{"--path", tmpDir, "--exclude", "_test\\.go$", "--exclude", "^util", "--no-gitignore"})

		err := cmd.Execute()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		output := buf.String()
		if strings.Contains(output, "_test.go") {
			t.Error("should exclude _test.go files")
		}
		if strings.Contains(output, "util") {
			t.Error("should exclude util files")
		}
		if !strings.Contains(output, "main.go") {
			t.Error("should include main.go")
		}
	})

	t.Run("invalid regex returns error", func(t *testing.T) {
		resetTreeFlags()
		cmd := newTreeCmd()
		buf := new(bytes.Buffer)
		cmd.SetOut(buf)
		cmd.SetArgs([]string{"--path", tmpDir, "--exclude", "[invalid"})

		err := cmd.Execute()
		if err == nil {
			t.Error("expected error for invalid regex")
		}
	})
}

func TestTreeBuilderHelpers(t *testing.T) {
	t.Run("isDefaultExcluded", func(t *testing.T) {
		b := &treeBuilder{}

		if !b.isDefaultExcluded("node_modules") {
			t.Error("node_modules should be excluded")
		}
		if !b.isDefaultExcluded("vendor") {
			t.Error("vendor should be excluded")
		}
		if b.isDefaultExcluded("src") {
			t.Error("src should not be excluded")
		}
	})
}

// TestTreeMinimalOutput tests minimal output mode
func TestTreeMinimalOutput(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("content"), 0644)

	resetTreeFlags()
	cmd := newTreeCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--path", tmpDir, "--min", "--no-gitignore"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	// Minimal output should still have content
	if len(output) == 0 {
		t.Error("expected output for minimal mode")
	}
}

// TestTreeJSONOutput tests JSON output mode
func TestTreeJSONOutput(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("content"), 0644)

	resetTreeFlags()
	cmd := newTreeCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--path", tmpDir, "--json", "--no-gitignore"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result TreeResult
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if result.Root == "" {
		t.Error("expected root in JSON output")
	}
}

// TestTreeEmptyDirectory tests behavior with empty directory
func TestTreeEmptyDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	resetTreeFlags()
	cmd := newTreeCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--path", tmpDir, "--no-gitignore"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should complete without error for empty directory
	output := buf.String()
	if len(output) == 0 {
		t.Error("expected some output for empty directory")
	}
}

// Defends epic 18 T2: tree on an empty directory dropped entries (omitempty
// on TreeResult.Entries), so a caller could not tell an empty directory from
// a result that never reported its list. VISION.md: a command that returns a
// list always returns the key, even when the list is empty. The sibling
// total test lives in ac5_review_test.go and must stay green.
func TestTreeEmptyDirStatesEmptyEntries(t *testing.T) {
	dir := t.TempDir()

	// --json: entries present as an empty list, not null and not absent.
	code, stdout, _ := runCLI(t, "tree", "--path", dir, "--json")
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stdout)
	}
	doc := decodeOne(t, stdout)
	if entries, ok := doc["entries"].([]any); !ok || len(entries) != 0 {
		t.Errorf("entries = %v (present %v), want an empty list", doc["entries"], ok)
	}
	if v, ok := doc["total"]; !ok || v != float64(0) {
		t.Errorf("total = %v (present %v), want 0", v, ok)
	}

	// TOON (the default): the empty list is stated as entries[0]:.
	code, toon, _ := runCLI(t, "tree", "--path", dir)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, toon)
	}
	if !strings.Contains(toon, "entries[0]:") {
		t.Errorf("TOON lacks entries[0]:\n%s", toon)
	}

	// --plain is unchanged: the root line only, no list and no count. The
	// shown root comes from the result itself so the test never compares a
	// raw t.TempDir path with a cwd-relative one.
	root, _ := doc["root"].(string)
	if root == "" {
		t.Fatalf("json result has no root: %v", doc)
	}
	code, plain, _ := runCLI(t, "tree", "--path", dir, "--plain")
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, plain)
	}
	if plain != root+"/\n" {
		t.Errorf("--plain = %q, want %q (unchanged root line)", plain, root+"/\n")
	}
}

// The expected next steps are spelled out here rather than read from the
// production variables, so a change to what an agent is told to do next is a
// deliberate edit in two places and not a silent one in one.
const (
	helpTreeGitignore  = "Add --no-gitignore to include ignored and hidden files"
	helpTreeExcludes   = "Add --no-default-excludes to include node_modules and other default-excluded directories"
	helpTreeExclude    = "Drop --exclude to include names your patterns filter out"
	helpTreeParent     = "Run `llm-data tree --path <parent>` to check the parent directory"
	helpTreeMaxEntries = "More entries: rerun with a larger --max-entries, or --max-entries 0 for no limit"
)

// treeHelpFixture is a directory that reads as empty under the default
// filters: either truly empty, or holding only a hidden file.
func treeHelpFixture(t *testing.T, hiddenOnly bool) string {
	t.Helper()
	dir := t.TempDir()
	if hiddenOnly {
		if err := os.WriteFile(filepath.Join(dir, ".hidden"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// Defends epic 22 T4: tree on an empty directory carried a bare entries[0]
// and left an agent to guess why nothing showed. The empty result must state
// the exact next steps in TOON and --json (and --json --min), and --plain
// must stay help-free. Fails when the emptyHelp call is removed.
func TestTreeEmptyDirCarriesNextSteps(t *testing.T) {
	want := []string{helpTreeGitignore, helpTreeExcludes, helpTreeParent}
	for _, hiddenOnly := range []bool{false, true} {
		dir := treeHelpFixture(t, hiddenOnly)
		for _, min := range []bool{false, true} {
			args := []string{"tree", "--path", dir, "--json"}
			if min {
				args = append(args, "--min")
			}
			code, stdout, _ := runCLI(t, args...)
			if code != 0 {
				t.Fatalf("exit %d\n%s", code, stdout)
			}
			help, ok := jsonHelp(t, stdout)
			if !ok || !reflect.DeepEqual(help, want) {
				t.Errorf("--json (min %v, hiddenOnly %v) help = %q (present %v), want %q", min, hiddenOnly, help, ok, want)
			}
		}
		code, toon, _ := runCLI(t, "tree", "--path", dir)
		if code != 0 {
			t.Fatalf("exit %d\n%s", code, toon)
		}
		assertAXIHelp(t, toon, want)

		code, plain, _ := runCLI(t, "tree", "--path", dir, "--plain")
		if code != 0 {
			t.Fatalf("exit %d\n%s", code, plain)
		}
		if hasHelpKey(plain) {
			t.Errorf("--plain carried a help key:\n%s", plain)
		}
	}
}

// Defends the grep precedent on tree: a filter flag the caller already
// passed is not offered again, --exclude is named only when it was used, and
// one unconditional step remains so an empty result never has nothing to
// say (emptyHelp panics on zero steps).
func TestTreeEmptyHelpDropsFlagsAlreadyPassed(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct {
		name string
		args []string
		want []string
	}{
		{"filters off", []string{"--no-gitignore", "--no-default-excludes"}, []string{helpTreeParent}},
		{"exclude used", []string{"--exclude", "nothing-matches-this"}, []string{helpTreeGitignore, helpTreeExcludes, helpTreeExclude, helpTreeParent}},
	} {
		t.Run(c.name, func(t *testing.T) {
			args := append([]string{"tree", "--path", dir, "--json"}, c.args...)
			code, stdout, _ := runCLI(t, args...)
			if code != 0 {
				t.Fatalf("exit %d\n%s", code, stdout)
			}
			help, ok := jsonHelp(t, stdout)
			if !ok || !reflect.DeepEqual(help, c.want) {
				t.Errorf("help = %q (present %v), want %q", help, ok, c.want)
			}
		})
	}
}

// Defends the other half of epic 22's rule: a non-empty tree carries no help
// key at all, not an empty list an agent pays tokens to read.
func TestTreeNonEmptyCarriesNoHelp(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, min := range []bool{false, true} {
		args := []string{"tree", "--path", dir, "--json"}
		if min {
			args = append(args, "--min")
		}
		code, stdout, _ := runCLI(t, args...)
		if code != 0 {
			t.Fatalf("exit %d\n%s", code, stdout)
		}
		if help, ok := jsonHelp(t, stdout); ok {
			t.Errorf("--json (min %v) non-empty tree carried help %q", min, help)
		}
	}
	code, toon, _ := runCLI(t, "tree", "--path", dir)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, toon)
	}
	if hasHelpKey(toon) {
		t.Errorf("TOON non-empty tree carried help:\n%s", toon)
	}
}

// Defends epic 22 T4's cut half: a tree truncated at --max-entries said only
// "use --max-entries to adjust" in a message; the structured result must
// carry a help[] entry naming the flag and the unlimited value, in TOON and
// --json (and --json --min). --plain keeps its truncation line, no help key.
func TestTreeTruncatedCarriesMaxEntriesHint(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{helpTreeMaxEntries}
	for _, min := range []bool{false, true} {
		args := []string{"tree", "--path", dir, "--max-entries", "1", "--json"}
		if min {
			args = append(args, "--min")
		}
		code, stdout, _ := runCLI(t, args...)
		if code != 0 {
			t.Fatalf("exit %d\n%s", code, stdout)
		}
		doc := decodeOne(t, stdout)
		if doc["truncated"] != true {
			t.Fatalf("result is not truncated: %s", stdout)
		}
		help, ok := jsonHelp(t, stdout)
		if !ok || !reflect.DeepEqual(help, want) {
			t.Errorf("--json (min %v) truncated help = %q (present %v), want %q", min, help, ok, want)
		}
	}
	code, toon, _ := runCLI(t, "tree", "--path", dir, "--max-entries", "1")
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, toon)
	}
	assertAXIHelp(t, toon, want)

	code, plain, _ := runCLI(t, "tree", "--path", dir, "--max-entries", "1", "--plain")
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, plain)
	}
	if hasHelpKey(plain) || !strings.Contains(plain, "truncated") {
		t.Errorf("--plain = %q, want the truncation line and no help key", plain)
	}
}
