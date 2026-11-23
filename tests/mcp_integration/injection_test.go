package mcp_integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// callText sends one tools/call over stdio and returns its text and isError.
func callText(t *testing.T, tool string, arguments map[string]interface{}) (string, bool) {
	t.Helper()
	call, err := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]interface{}{"name": tool, "arguments": arguments},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := speak(t, []string{initRequest, initNotification, string(call)})
	resp, ok := got[2]
	if !ok {
		t.Fatal("no response to tools/call")
	}
	var result struct {
		IsError bool `json:"isError"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("tools/call result: %v\n%s", err, resp.Result)
	}
	if len(result.Content) == 0 {
		t.Fatalf("tools/call returned no content: %s", resp.Result)
	}
	return result.Content[0].Text, result.IsError
}

// Defends against option injection through a tool value, end to end. The
// review's reproduction: a yaml_multiset pair {"--file=h.yaml": "--create"}
// reached the CLI as options, redirected the write to h.yaml and created it.
// A tool value must never name a file the caller did not pass as "file".
func TestToolValueSpelledAsAnOptionCannotRedirectAWrite(t *testing.T) {
	dir := t.TempDir()
	g := filepath.Join(dir, "g.yaml")
	h := filepath.Join(dir, "h.yaml")
	if err := os.WriteFile(g, []byte("a: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _ = callText(t, "llm_data_yaml_multiset", map[string]interface{}{
		"file":  g,
		"pairs": map[string]interface{}{"--file=" + h: "--create", "a": "9"},
	})
	if _, err := os.Stat(h); err == nil {
		t.Fatalf("a pair key spelled --file=%s created that file", h)
	}
}

// Defends grep's pattern: one that starts with "-" was parsed as an option,
// so `-v` could not be searched for through MCP at all.
func TestGrepPatternStartingWithADashIsSearchedForOverStdio(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "flags.txt")
	if err := os.WriteFile(f, []byte("run with -v for detail\nnothing here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	text, isErr := callText(t, "llm_data_grep", map[string]interface{}{"pattern": "-v", "paths": []string{f}})
	if isErr {
		t.Fatalf("grep for -v failed: %s", text)
	}
	if !strings.Contains(text, "run with -v for detail") || strings.Contains(text, "nothing here") {
		t.Errorf("grep for -v did not return exactly the matching line: %q", text)
	}
}
