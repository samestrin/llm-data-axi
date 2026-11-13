package mcp_integration

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// These tests drive the shipped binaries through the binary lookup itself
// (epic 09 T4). The other end-to-end tests set LLM_DATA_BINARY, which skips
// the lookup; here the lookup is the thing under test.

// resolvedTempDir is t.TempDir with symlinks resolved. On macOS the temp dir
// sits behind /var -> /private/var and os.Executable reports the resolved
// path, so an expected sibling path built from the raw temp dir would not
// match (AGENTS.md: never mix a real path with a t.TempDir path).
func resolvedTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// copyBinary copies src to dst with the exec bit set, checking every error:
// a silently truncated copy would make the test pass or fail for the wrong
// reason.
func copyBinary(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}

// envWithout drops the named variables from an environment, so a value
// exported in the test runner's environment cannot steer the lookup.
func envWithout(environ []string, keys ...string) []string {
	var out []string
	for _, kv := range environ {
		k, _, _ := strings.Cut(kv, "=")
		if slices.Contains(keys, k) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// Defends epic 09 T4 / AC4: with llm-data only beside llm-data-mcp and off
// PATH, a tool call must succeed. install.sh defaults to ~/.local/bin, which
// is not on PATH in a fresh shell; before the fix the server fell back to
// /usr/local/bin/llm-data and refused to start at all.
func TestToolCallSucceedsWithTheCLIBesideTheServerAndOffPATH(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the harness copies unix-named binaries; the sibling is llm-data.exe on Windows")
	}
	cli, mcp := builtBinaries(t)
	dir := resolvedTempDir(t)
	copyBinary(t, cli, filepath.Join(dir, "llm-data"))
	copyBinary(t, mcp, filepath.Join(dir, "llm-data-mcp"))

	// No LLM_DATA_BINARY and an empty PATH: the sibling directory is the only
	// place the CLI can be found. LLM_DATA_MCP_AXI is cleared so an exported
	// value cannot change the encoding the assertion sees.
	env := envWithout(os.Environ(), "LLM_DATA_BINARY", "PATH", "LLM_DATA_MCP_AXI")
	env = append(env, "PATH=", "LLM_DATA_MCP_AXI=")

	got := speakWith(t, filepath.Join(dir, "llm-data-mcp"), env, []string{
		initRequest,
		initNotification,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"llm_data_math","arguments":{"expression":"2+2"}}}`,
	})

	resp, ok := got[2]
	if !ok {
		t.Fatal("no response to tools/call")
	}
	var payload struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(resp.Result, &payload); err != nil {
		t.Fatalf("tools/call result: %v", err)
	}
	if payload.IsError || len(payload.Content) == 0 {
		t.Fatalf("the sibling CLI was not executed: %s", resp.Result)
	}
	// Assert the payload, not just the absence of an error: the computation
	// must have reached the copied CLI and come back.
	if text := payload.Content[0].Text; !strings.Contains(text, "result: 4") {
		t.Errorf("2+2 did not come back from the sibling CLI: %q", text)
	}
}

// Defends epic 09 T4: with llm-data nowhere, the startup error must name the
// path tried and LLM_DATA_BINARY, so a broken install is fixable from the
// error alone. LLM_DATA_BINARY points at a guaranteed-absent path to make the
// failure deterministic: without it the fallback /usr/local/bin/llm-data
// might exist on the test machine, and an environment-dependent test is worse
// than no test.
func TestStartupErrorNamesTheTriedPathAndTheOverride(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the harness copies unix-named binaries; the sibling is llm-data.exe on Windows")
	}
	_, mcp := builtBinaries(t)
	dir := resolvedTempDir(t)
	// Only the server is copied: there is no sibling llm-data to find.
	copyBinary(t, mcp, filepath.Join(dir, "llm-data-mcp"))

	absent := filepath.Join(dir, "absent", "llm-data")
	env := envWithout(os.Environ(), "LLM_DATA_BINARY", "PATH", "LLM_DATA_MCP_AXI")
	env = append(env, "PATH=", "LLM_DATA_BINARY="+absent, "LLM_DATA_MCP_AXI=")

	cmd := exec.Command(filepath.Join(dir, "llm-data-mcp"))
	cmd.Env = env
	out, err := cmd.CombinedOutput()

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("want exit 1 with the binary nowhere, got err=%v output=%q", err, out)
	}
	for _, want := range []string{
		absent,                         // the path tried
		"LLM_DATA_BINARY",              // the override that fixes it
		filepath.Join(dir, "llm-data"), // the sibling location that was looked at
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("the startup error does not name %q; got:\n%s", want, out)
		}
	}
}
