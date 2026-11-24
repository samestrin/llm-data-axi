package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Defends --var against a value that sets nothing: `--var k` (no "=") and
// `--var =x` (no key) were dropped without a word and the template rendered
// with exit 0, so the caller believed the variable was set.
func TestTemplateVarWithoutKeyOrEqualsIsAUsageError(t *testing.T) {
	dir := realTempDir(t)
	tmpl := filepath.Join(dir, "t.txt")
	if err := os.WriteFile(tmpl, []byte("hi {{k}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"k", "=x", " =x"} {
		code, stdout, _ := runCLI(t, "template", tmpl, "--var", v)
		if code != 2 {
			t.Errorf("--var %q: exit %d, want 2; stdout %q", v, code, stdout)
		}
		if !strings.Contains(stdout, "--var") || !strings.Contains(stdout, "KEY=VALUE") {
			t.Errorf("--var %q: error should say what --var takes, got %q", v, stdout)
		}
	}
}

// Defends --literal-vars, the switch the MCP server passes: a --var value
// starting with @ was always read as a file path, so an MCP client sending
// {"k": "@secret.txt"} got the file's contents back. Under --literal-vars the
// value is substituted as given and no file is opened.
func TestTemplateLiteralVarsDoesNotReadAFile(t *testing.T) {
	dir := realTempDir(t)
	tmpl := filepath.Join(dir, "t.txt")
	secret := filepath.Join(dir, "sec.txt")
	if err := os.WriteFile(tmpl, []byte("v=[[k]]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secret, []byte("TOPSECRET"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, stdout, _ := runCLI(t, "template", tmpl, "--syntax", "brackets", "--literal-vars", "--var", "k=@"+secret, "--plain")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stdout)
	}
	if strings.Contains(stdout, "TOPSECRET") || !strings.Contains(stdout, "v=@"+secret) {
		t.Errorf("--literal-vars: want the value as given, got %q", stdout)
	}

	// Without it the CLI keeps its documented, opt-in file read.
	code, stdout, _ = runCLI(t, "template", tmpl, "--syntax", "brackets", "--var", "k=@"+secret, "--plain")
	if code != 0 || !strings.Contains(stdout, "v=TOPSECRET") {
		t.Errorf("--var k=@PATH should still read PATH on the CLI: exit %d %q", code, stdout)
	}
}

// Defends -o against a target that is not a regular file: a directory used to
// fail late with exit 1 after the render, and a FIFO hung (see the unix test).
func TestTemplateOutputToADirectoryIsAUsageError(t *testing.T) {
	dir := realTempDir(t)
	tmpl := filepath.Join(dir, "t.txt")
	if err := os.WriteFile(tmpl, []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := runCLI(t, "template", tmpl, "-o", dir)
	if code != 2 || !strings.Contains(stdout, "not a regular file") {
		t.Errorf("-o <dir>: exit %d %q, want exit 2 naming it not a regular file", code, stdout)
	}
}
