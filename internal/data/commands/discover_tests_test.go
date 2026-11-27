package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverTestsCommand(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(string)
		expected []string
	}{
		{
			name: "detect vitest",
			setup: func(dir string) {
				os.WriteFile(filepath.Join(dir, "vitest.config.ts"), []byte("export default {}"), 0644)
				os.MkdirAll(filepath.Join(dir, "src"), 0755)
				os.MkdirAll(filepath.Join(dir, "tests"), 0755)
				os.WriteFile(filepath.Join(dir, "tests", "app.test.ts"), []byte("test()"), 0644)
			},
			expected: []string{"TEST_RUNNER: vitest", "PATTERN: SEPARATED"},
		},
		{
			name: "detect jest",
			setup: func(dir string) {
				os.WriteFile(filepath.Join(dir, "jest.config.js"), []byte("module.exports = {}"), 0644)
				os.MkdirAll(filepath.Join(dir, "src"), 0755)
				os.MkdirAll(filepath.Join(dir, "__tests__"), 0755)
				os.WriteFile(filepath.Join(dir, "__tests__", "app.test.js"), []byte("test()"), 0644)
			},
			expected: []string{"TEST_RUNNER: jest", "PATTERN: SEPARATED"},
		},
		{
			name: "detect nextjs framework",
			setup: func(dir string) {
				os.WriteFile(filepath.Join(dir, "next.config.js"), []byte("module.exports = {}"), 0644)
			},
			expected: []string{"FRAMEWORK: nextjs"},
		},
		{
			name: "detect e2e directory",
			setup: func(dir string) {
				os.MkdirAll(filepath.Join(dir, "e2e"), 0755)
				os.WriteFile(filepath.Join(dir, "e2e", "app.spec.ts"), []byte("test()"), 0644)
			},
			expected: []string{"E2E_DIR: e2e/"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			tt.setup(tmpDir)

			cmd := newDiscoverTestsCmd()
			buf := new(bytes.Buffer)
			cmd.SetOut(buf)
			cmd.SetArgs([]string{"--path", tmpDir})

			err := cmd.Execute()
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

// TestDiscoverTestsColocatedPattern tests detection of colocated test pattern
func TestDiscoverTestsColocatedPattern(t *testing.T) {
	tmpDir := t.TempDir()

	// Create colocated tests (test files next to source files)
	srcDir := filepath.Join(tmpDir, "src")
	os.MkdirAll(srcDir, 0755)
	os.WriteFile(filepath.Join(srcDir, "app.ts"), []byte("export default {}"), 0644)
	os.WriteFile(filepath.Join(srcDir, "app.test.ts"), []byte("test()"), 0644)
	os.WriteFile(filepath.Join(srcDir, "utils.ts"), []byte("export default {}"), 0644)
	os.WriteFile(filepath.Join(srcDir, "utils.spec.ts"), []byte("test()"), 0644)

	cmd := newDiscoverTestsCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--path", tmpDir})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "PATTERN: COLOCATED") {
		t.Errorf("output %q should contain PATTERN: COLOCATED", output)
	}
}

// TestDiscoverTestsGoProject tests detection of Go test infrastructure
func TestDiscoverTestsGoProject(t *testing.T) {
	tmpDir := t.TempDir()

	// Create Go project structure with colocated tests
	os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module example.com/test"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "main_test.go"), []byte("package main"), 0644)

	cmd := newDiscoverTestsCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--path", tmpDir})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	// Go projects should be detected as colocated pattern since _test.go files are next to source
	if !strings.Contains(output, "PATTERN:") {
		t.Errorf("output %q should contain PATTERN:", output)
	}
}

// TestDiscoverTestsPytestProject tests detection of pytest infrastructure
func TestDiscoverTestsPytestProject(t *testing.T) {
	tmpDir := t.TempDir()

	// Create pytest project structure
	os.WriteFile(filepath.Join(tmpDir, "pytest.ini"), []byte("[pytest]"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "test_example.py"), []byte("def test_foo(): pass"), 0644)

	cmd := newDiscoverTestsCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--path", tmpDir})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "TEST_RUNNER: pytest") {
		t.Errorf("output %q should contain TEST_RUNNER: pytest", output)
	}
}

// TestDiscoverTestsJSONOutput tests JSON output mode
func TestDiscoverTestsJSONOutput(t *testing.T) {
	tmpDir := t.TempDir()

	// Create vitest project
	os.WriteFile(filepath.Join(tmpDir, "vitest.config.ts"), []byte("export default {}"), 0644)
	os.MkdirAll(filepath.Join(tmpDir, "tests"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "tests", "app.test.ts"), []byte("test()"), 0644)

	cmd := newDiscoverTestsCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--path", tmpDir, "--json"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	// JSON output should have proper keys
	expectedKeys := []string{`"test_runner"`, `"pattern"`}
	for _, key := range expectedKeys {
		if !strings.Contains(output, key) {
			t.Errorf("JSON output %q should contain key %s", output, key)
		}
	}
}

// TestDiscoverTestsMinimalOutput tests minimal output mode
func TestDiscoverTestsMinimalOutput(t *testing.T) {
	tmpDir := t.TempDir()

	// Create jest project
	os.WriteFile(filepath.Join(tmpDir, "jest.config.js"), []byte("module.exports = {}"), 0644)
	os.MkdirAll(filepath.Join(tmpDir, "__tests__"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "__tests__", "app.test.js"), []byte("test()"), 0644)

	cmd := newDiscoverTestsCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--path", tmpDir, "--min"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	// Minimal output should contain key info
	if !strings.Contains(output, "jest") {
		t.Errorf("minimal output %q should contain 'jest'", output)
	}
}

// TestDiscoverTestsWithCypressE2E tests detection of Cypress e2e tests
func TestDiscoverTestsWithCypressE2E(t *testing.T) {
	tmpDir := t.TempDir()

	// Create Cypress project structure
	os.WriteFile(filepath.Join(tmpDir, "cypress.config.ts"), []byte("export default {}"), 0644)
	os.MkdirAll(filepath.Join(tmpDir, "cypress", "e2e"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "cypress", "e2e", "login.cy.ts"), []byte("describe()"), 0644)

	cmd := newDiscoverTestsCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--path", tmpDir})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	// Cypress directory should be detected
	if !strings.Contains(output, "E2E_DIR: cypress/") {
		t.Errorf("output %q should contain E2E_DIR: cypress/", output)
	}
}

// TestDiscoverTestsPathIsFile defends epic 18 T4: discover-tests --path <file>
// used to exit 0 and report pattern "UNKNOWN", indistinguishable from a real
// directory with no tests. It must fail with tree's and stats' wording, exit
// 1, an error document on stdout and empty stderr, in every output format; a
// directory path must keep succeeding.
func TestDiscoverTestsPathIsFile(t *testing.T) {
	dir := realTempDir(t)
	file := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(file, []byte("not a project"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, f := range errorFormats {
		t.Run(f.name, func(t *testing.T) {
			args := append([]string{"discover-tests", "--path", file}, f.flags...)
			code, stdout, stderr := runCLI(t, args...)
			if code != 1 {
				t.Errorf("exit %d, want 1 (stdout %q)", code, stdout)
			}
			if stderr != "" {
				t.Errorf("stderr must be empty, got %q", stderr)
			}
			msg, ok := f.message(stdout)
			if !ok {
				t.Fatalf("stdout is not a %s error: %q", f.name, stdout)
			}
			if want := "path is not a directory: " + file; !strings.Contains(msg, want) {
				t.Errorf("error must say %q, got %q", want, msg)
			}
		})
	}

	// A directory path is unchanged: still exit 0 with a result, no error.
	for _, f := range errorFormats {
		t.Run("dir/"+f.name, func(t *testing.T) {
			args := append([]string{"discover-tests", "--path", dir}, f.flags...)
			code, stdout, stderr := runCLI(t, args...)
			if code != 0 || stderr != "" {
				t.Errorf("exit %d stderr %q, want 0 and empty (stdout %q)", code, stderr, stdout)
			}
			if stdout == "" || strings.Contains(stdout, "path is not a directory") {
				t.Errorf("directory must still produce a result, got %q", stdout)
			}
		})
	}
}
