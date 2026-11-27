package commands

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
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
