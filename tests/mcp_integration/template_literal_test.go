package mcp_integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Defends the operator's reproduction end to end: llm_data_template with
// vars {"k": "@sec.txt"} returned the contents of sec.txt, because the CLI
// reads a --var value starting with @ as a file. Over MCP the value must come
// back as given and the file must not be read.
func TestTemplateVarStartingWithAtIsNotReadOverStdio(t *testing.T) {
	dir := t.TempDir()
	tmpl := filepath.Join(dir, "t.txt")
	secret := filepath.Join(dir, "sec.txt")
	if err := os.WriteFile(tmpl, []byte("v=[[k]]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secret, []byte("TOPSECRET"), 0o644); err != nil {
		t.Fatal(err)
	}
	call, err := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]interface{}{
			"name":      "llm_data_template",
			"arguments": map[string]interface{}{"file": tmpl, "vars": map[string]interface{}{"k": "@" + secret}},
		},
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
	if result.IsError || len(result.Content) == 0 {
		t.Fatalf("template call failed: %s", resp.Result)
	}
	text := result.Content[0].Text
	if strings.Contains(text, "TOPSECRET") {
		t.Errorf("the MCP template call read the file named by a var value: %q", text)
	}
	if !strings.Contains(text, "@"+secret) {
		t.Errorf("want the value substituted as given, got %q", text)
	}
}
