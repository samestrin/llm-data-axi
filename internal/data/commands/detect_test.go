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

func TestDetectCommand(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(string) // function to set up project structure
		expected []string
	}{
		{
			name: "detect node project",
			setup: func(dir string) {
				os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"test"}`), 0644)
			},
			expected: []string{"STACK: node", "LANGUAGE: javascript"},
		},
		{
			name: "detect typescript project",
			setup: func(dir string) {
				os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"test"}`), 0644)
				os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(`{}`), 0644)
			},
			expected: []string{"STACK: node", "LANGUAGE: typescript"},
		},
		{
			name: "detect go project",
			setup: func(dir string) {
				os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0644)
			},
			expected: []string{"STACK: go", "LANGUAGE: go"},
		},
		{
			name: "detect python project",
			setup: func(dir string) {
				os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte("[project]"), 0644)
			},
			expected: []string{"STACK: python", "LANGUAGE: python"},
		},
		{
			name: "detect rust project",
			setup: func(dir string) {
				os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[package]"), 0644)
			},
			expected: []string{"STACK: rust", "LANGUAGE: rust"},
		},
		{
			name: "detect npm package manager",
			setup: func(dir string) {
				os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{}`), 0644)
				os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte(`{}`), 0644)
			},
			expected: []string{"PACKAGE_MANAGER: npm"},
		},
		{
			name: "detect nextjs framework",
			setup: func(dir string) {
				os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{}`), 0644)
				os.WriteFile(filepath.Join(dir, "next.config.js"), []byte("module.exports = {}"), 0644)
			},
			expected: []string{"FRAMEWORK: nextjs"},
		},
		{
			name: "detect tests directory",
			setup: func(dir string) {
				os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{}`), 0644)
				os.MkdirAll(filepath.Join(dir, "tests"), 0755)
			},
			expected: []string{"HAS_TESTS: true"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			tt.setup(tmpDir)

			cmd := newDetectCmd()
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

func TestDetectJSONOutput(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module test"), 0644)

	cmd := newDetectCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--path", tmpDir, "--json"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, `"stack"`) || !strings.Contains(output, `"go"`) {
		t.Errorf("JSON output should contain stack field: %s", output)
	}
}

func TestDetectDirs_TwoComponents(t *testing.T) {
	tmpDir := t.TempDir()

	// Create backend (python) and frontend (node) subdirectories
	os.MkdirAll(filepath.Join(tmpDir, "backend"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "backend", "requirements.txt"), []byte("flask"), 0644)

	os.MkdirAll(filepath.Join(tmpDir, "frontend"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "frontend", "package.json"), []byte(`{"name":"test"}`), 0644)
	os.WriteFile(filepath.Join(tmpDir, "frontend", "tsconfig.json"), []byte(`{}`), 0644)

	cmd := newDetectCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--path", tmpDir, "--dirs", "backend,frontend"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "backend") {
		t.Errorf("expected 'backend' section in output: %s", output)
	}
	if !strings.Contains(output, "frontend") {
		t.Errorf("expected 'frontend' section in output: %s", output)
	}
	if !strings.Contains(output, "python") {
		t.Errorf("expected 'python' stack for backend: %s", output)
	}
	if !strings.Contains(output, "node") {
		t.Errorf("expected 'node' stack for frontend: %s", output)
	}
}

func TestDetectDirs_SingleDir(t *testing.T) {
	tmpDir := t.TempDir()

	os.MkdirAll(filepath.Join(tmpDir, "api"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "api", "go.mod"), []byte("module test"), 0644)

	cmd := newDetectCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--path", tmpDir, "--dirs", "api"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "api") {
		t.Errorf("expected 'api' section in output: %s", output)
	}
	if !strings.Contains(output, "go") {
		t.Errorf("expected 'go' stack: %s", output)
	}
}

func TestDetectDirs_NonExistentDir(t *testing.T) {
	tmpDir := t.TempDir()

	os.MkdirAll(filepath.Join(tmpDir, "backend"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "backend", "go.mod"), []byte("module test"), 0644)

	cmd := newDetectCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--path", tmpDir, "--dirs", "backend,nonexistent"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	// Should still have backend results
	if !strings.Contains(output, "backend") {
		t.Errorf("expected 'backend' section despite nonexistent dir: %s", output)
	}
}

func TestDetectDirs_JSONOutput(t *testing.T) {
	tmpDir := t.TempDir()

	os.MkdirAll(filepath.Join(tmpDir, "backend"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "backend", "requirements.txt"), []byte("django"), 0644)

	os.MkdirAll(filepath.Join(tmpDir, "frontend"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "frontend", "package.json"), []byte(`{"name":"test"}`), 0644)

	cmd := newDetectCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--path", tmpDir, "--dirs", "backend,frontend", "--json"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, `"components"`) {
		t.Errorf("JSON output should contain 'components' key: %s", output)
	}
	if !strings.Contains(output, `"backend"`) {
		t.Errorf("JSON output should contain 'backend' key: %s", output)
	}
	if !strings.Contains(output, `"frontend"`) {
		t.Errorf("JSON output should contain 'frontend' key: %s", output)
	}
}

func TestDetectDirs_NoDirsFlag_Unchanged(t *testing.T) {
	// Regression test: without --dirs, behavior is unchanged
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module test"), 0644)

	cmd := newDetectCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--path", tmpDir})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	// Original format: no "components" wrapper
	if strings.Contains(output, "components") {
		t.Errorf("without --dirs, output should NOT contain 'components': %s", output)
	}
	if !strings.Contains(output, "STACK: go") {
		t.Errorf("expected original format 'STACK: go': %s", output)
	}
}

// TestDetectInDir_TwoMarkersStable defends against detection depending on Go's
// random map order: with two markers in one directory, detectInDir used to range
// a map and keep whichever marker it hit first, so stack, package_manager and
// framework could flip between runs, and the home view's project_type with them.
// Each table is checked separately, and looped, because one call can get lucky.
func TestDetectInDir_TwoMarkersStable(t *testing.T) {
	cases := []struct {
		name    string
		markers []string
		field   func(DetectResult) string
		want    string
	}{
		{"stack", []string{"go.mod", "package.json"}, func(r DetectResult) string { return r.Stack }, "node"},
		{"language", []string{"go.mod", "package.json"}, func(r DetectResult) string { return r.Language }, "javascript"},
		{"package_manager", []string{"package-lock.json", "yarn.lock"}, func(r DetectResult) string { return r.PackageManager }, "yarn"},
		{"framework", []string{"angular.json", "next.config.js"}, func(r DetectResult) string { return r.Framework }, "nextjs"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, m := range tc.markers {
				if err := os.WriteFile(filepath.Join(dir, m), []byte("{}"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			for i := 0; i < 200; i++ {
				if got := tc.field(detectInDir(dir)); got != tc.want {
					t.Fatalf("call %d: %s = %q, want %q", i, tc.name, got, tc.want)
				}
			}
		})
	}
}

// TestDetectPathIsFile defends epic 18 T4: detect --path <file> used to exit 0
// and report stack "unknown", indistinguishable from a real directory with no
// markers. It must fail with tree's and stats' wording, exit 1, an error
// document on stdout and empty stderr, in every output format; a directory
// path must keep succeeding.
func TestDetectPathIsFile(t *testing.T) {
	dir := realTempDir(t)
	file := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(file, []byte("not a project"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, f := range errorFormats {
		t.Run(f.name, func(t *testing.T) {
			args := append([]string{"detect", "--path", file}, f.flags...)
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

	// A missing path keeps its own wording, distinct from the file case.
	for _, f := range errorFormats {
		t.Run("missing/"+f.name, func(t *testing.T) {
			missing := filepath.Join(dir, "missing")
			args := append([]string{"detect", "--path", missing}, f.flags...)
			code, stdout, stderr := runCLI(t, args...)
			if code != 1 || stderr != "" {
				t.Errorf("exit %d stderr %q, want 1 and empty (stdout %q)", code, stderr, stdout)
			}
			msg, ok := f.message(stdout)
			if !ok {
				t.Fatalf("stdout is not a %s error: %q", f.name, stdout)
			}
			if want := "path does not exist: " + missing; !strings.Contains(msg, want) {
				t.Errorf("error must say %q, got %q", want, msg)
			}
		})
	}

	// A directory path is unchanged: still exit 0 with a result, no error.
	for _, f := range errorFormats {
		t.Run("dir/"+f.name, func(t *testing.T) {
			args := append([]string{"detect", "--path", dir}, f.flags...)
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

// The expected next steps are spelled out here, not read from production
// variables, so a change to what an agent is told to do next is a deliberate
// edit in two places (as help_next_steps_test.go does).
var wantDetectUnknownHelp = []string{
	"Check the directory holds a project marker such as package.json, go.mod or pyproject.toml",
	"Run `llm-data tree --path <dir>` to see the directory layout",
}

var wantDetectNoComponentsHelp = []string{
	"Check the --dirs names exist under --path",
	"Run `llm-data tree --path <dir>` to list subdirectories",
}

// Defends epic 22 T5: detect finding no stack used to print a bare
// stack: unknown, a dead end. The usual cause is pointing at a directory
// above or beside the one holding the project's marker files.
func TestDetectUnknownStackCarriesHelp(t *testing.T) {
	dir := t.TempDir()

	for _, minOut := range []bool{false, true} {
		args := []string{"detect", "--path", dir, "--json"}
		if minOut {
			args = append(args, "--min")
		}
		help, ok := jsonHelp(t, runOK(t, args...))
		if !ok || !reflect.DeepEqual(help, wantDetectUnknownHelp) {
			t.Errorf("--json (min %v) help = %q (present %v), want %q", minOut, help, ok, wantDetectUnknownHelp)
		}
	}
	assertAXIHelp(t, runOK(t, "detect", "--path", dir, "--axi"), wantDetectUnknownHelp)
}

// Defends a detected stack staying clean: next steps there are noise an agent
// pays tokens to read, in either encoding.
func TestDetectKnownStackCarriesNoHelp(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0644)

	if help, ok := jsonHelp(t, runOK(t, "detect", "--path", dir, "--json")); ok {
		t.Errorf("--json carried help %q", help)
	}
	if out := runOK(t, "detect", "--path", dir, "--axi"); hasHelpKey(out) {
		t.Errorf("--axi carried help:\n%s", out)
	}
}

// Defends --plain staying help-free, as grep, multigrep and the home view are.
func TestDetectUnknownHelpAbsentFromPlain(t *testing.T) {
	dir := t.TempDir()
	out := runOK(t, "detect", "--path", dir, "--plain")
	if hasHelpKey(out) {
		t.Errorf("--plain unknown result printed a help key:\n%s", out)
	}
	if !strings.Contains(out, "STACK: unknown") {
		t.Errorf("--plain unknown result lost its stack line:\n%s", out)
	}
}

// detectDirsDoc decodes a --dirs payload keeping key presence: "no help" and
// "an empty help list" must be told apart, per component and at the top level.
type detectDirsDoc struct {
	Components map[string]map[string]json.RawMessage `json:"components"`
	Help       json.RawMessage                       `json:"help"`
}

func parseDetectDirsDoc(t *testing.T, out string) detectDirsDoc {
	t.Helper()
	var doc detectDirsDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	return doc
}

func componentHelp(t *testing.T, comp map[string]json.RawMessage) ([]string, bool) {
	t.Helper()
	raw, ok := comp["help"]
	if !ok {
		return nil, false
	}
	var help []string
	if err := json.Unmarshal(raw, &help); err != nil {
		t.Fatalf("help is not a list of strings: %v\n%s", err, raw)
	}
	return help, true
}

// Defends --dirs runs: an unknown component is a dead end of its own, so it
// carries the same next steps, while a detected component and the top level
// stay clean. A run where every component is detected carries no help at all.
func TestDetectDirsUnknownComponentCarriesHelp(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "backend"), 0755)
	os.WriteFile(filepath.Join(dir, "backend", "go.mod"), []byte("module test"), 0644)
	os.MkdirAll(filepath.Join(dir, "frontend"), 0755)

	for _, minOut := range []bool{false, true} {
		args := []string{"detect", "--path", dir, "--dirs", "backend,frontend", "--json"}
		if minOut {
			args = append(args, "--min")
		}
		doc := parseDetectDirsDoc(t, runOK(t, args...))
		help, ok := componentHelp(t, doc.Components["frontend"])
		if !ok || !reflect.DeepEqual(help, wantDetectUnknownHelp) {
			t.Errorf("--json (min %v) frontend help = %q (present %v), want %q", minOut, help, ok, wantDetectUnknownHelp)
		}
		if help, ok := componentHelp(t, doc.Components["backend"]); ok {
			t.Errorf("--json (min %v) backend carried help %q", minOut, help)
		}
		if doc.Help != nil {
			t.Errorf("--json (min %v) top level carried help %s", minOut, doc.Help)
		}
	}
	assertAXIHelp(t, runOK(t, "detect", "--path", dir, "--dirs", "backend,frontend", "--axi"), wantDetectUnknownHelp)

	// Every component detected: no help anywhere.
	os.WriteFile(filepath.Join(dir, "frontend", "package.json"), []byte(`{}`), 0644)
	doc := parseDetectDirsDoc(t, runOK(t, "detect", "--path", dir, "--dirs", "backend,frontend", "--json"))
	for name, comp := range doc.Components {
		if help, ok := componentHelp(t, comp); ok {
			t.Errorf("component %s carried help %q", name, help)
		}
	}
	if doc.Help != nil {
		t.Errorf("top level carried help %s", doc.Help)
	}
}

// Defends a --dirs run where every named directory is missing: the components
// map is empty and the warnings go to stderr, so without top-level help the
// stdout payload is a bare dead end.
func TestDetectDirsAllMissingCarriesHelp(t *testing.T) {
	dir := t.TempDir()

	for _, minOut := range []bool{false, true} {
		args := []string{"detect", "--path", dir, "--dirs", "gone1,gone2", "--json"}
		if minOut {
			args = append(args, "--min")
		}
		out := runOK(t, args...)
		help, ok := jsonHelp(t, out)
		if !ok || !reflect.DeepEqual(help, wantDetectNoComponentsHelp) {
			t.Errorf("--json (min %v) help = %q (present %v), want %q", minOut, help, ok, wantDetectNoComponentsHelp)
		}
		if doc := parseDetectDirsDoc(t, out); len(doc.Components) != 0 {
			t.Errorf("--json (min %v) components = %v, want empty", minOut, doc.Components)
		}
	}
	assertAXIHelp(t, runOK(t, "detect", "--path", dir, "--dirs", "gone1,gone2", "--axi"), wantDetectNoComponentsHelp)
}
