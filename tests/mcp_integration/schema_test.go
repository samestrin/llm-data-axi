// Package mcp_integration checks the MCP server from OUTSIDE its own package.
//
// The in-package tests in internal/data/mcpserver call GetToolDefinitions and
// ExecuteHandler directly. These drive the shipped binary over real stdio
// instead, so they cover the wiring that the in-package tests cannot see: the
// SDK registration loop in cmd/llm-data-mcp, JSON-RPC framing, and the exec of
// the CLI itself.
package mcp_integration

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	mcpserver "github.com/samestrin/llm-data-axi/internal/data/mcpserver"
)

// portedToolCount is the size of the set carried over from llm-support by the
// Phase 2 port rule. llm-tools asserted 77 here; llm-data ships the 26 tools
// that the cadence skills actually call.
const portedToolCount = 26

func TestToolCount(t *testing.T) {
	if got := len(mcpserver.GetToolDefinitions()); got != portedToolCount {
		t.Errorf("expected %d llm-data tools, got %d", portedToolCount, got)
	}
}

func TestToolSchemasDeclarePropertiesAndType(t *testing.T) {
	for _, tool := range mcpserver.GetToolDefinitions() {
		var schema map[string]interface{}
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Errorf("tool %s has an invalid schema: %v", tool.Name, err)
			continue
		}
		if schemaType, ok := schema["type"].(string); !ok || schemaType != "object" {
			t.Errorf("tool %s schema type should be 'object', got %v", tool.Name, schema["type"])
		}
		if _, ok := schema["properties"]; !ok {
			t.Errorf("tool %s schema is missing 'properties'", tool.Name)
		}
	}
}

func TestToolPrefixes(t *testing.T) {
	const want = "llm_data_"
	for _, tool := range mcpserver.GetToolDefinitions() {
		if !strings.HasPrefix(tool.Name, want) {
			t.Errorf("tool %s should carry the %q prefix", tool.Name, want)
		}
	}
}

func TestToolDescriptions(t *testing.T) {
	for _, tool := range mcpserver.GetToolDefinitions() {
		if len(tool.Description) < 20 {
			t.Errorf("tool %s has a %d-character description", tool.Name, len(tool.Description))
		}
	}
}

// --- end-to-end over stdio ---

// repoRoot walks up from this test file to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate this test file")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

// builtBinaries returns the CLI and MCP paths from build/, skipping when they
// are absent. CI runs `make build` before `go test` for exactly this reason.
func builtBinaries(t *testing.T) (cli, mcp string) {
	t.Helper()
	root := repoRoot(t)
	cli = filepath.Join(root, "build", "llm-data")
	mcp = filepath.Join(root, "build", "llm-data-mcp")
	for _, p := range []string{cli, mcp} {
		if _, err := os.Stat(p); err != nil {
			t.Skipf("run `make build` first; %s is missing", p)
		}
	}
	return cli, mcp
}

type rpcResponse struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

// speak runs the MCP server, writes the given JSON-RPC lines to its stdin, and
// returns every response keyed by id.
func speak(t *testing.T, requests []string) map[int]rpcResponse {
	t.Helper()
	cli, mcp := builtBinaries(t)

	cmd := exec.Command(mcp)
	// Points the server at the freshly built CLI rather than whatever happens
	// to be installed, so this test cannot pass against a stale binary.
	cmd.Env = append(os.Environ(), "LLM_DATA_BINARY="+cli)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	done := make(chan map[int]rpcResponse, 1)
	go func() {
		got := make(map[int]rpcResponse)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var resp rpcResponse
			if err := json.Unmarshal([]byte(line), &resp); err != nil {
				continue
			}
			if resp.ID != 0 {
				got[resp.ID] = resp
			}
		}
		done <- got
	}()

	for _, r := range requests {
		if _, err := stdin.Write([]byte(r + "\n")); err != nil {
			t.Fatal(err)
		}
	}
	// The server answers in order, so a short settle is enough before closing
	// stdin; closing it is what ends the scanner loop.
	time.Sleep(500 * time.Millisecond)
	_ = stdin.Close()

	select {
	case got := <-done:
		_ = cmd.Wait()
		return got
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("the MCP server did not finish within 30s")
		return nil
	}
}

const initRequest = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"integration","version":"1"}}}`
const initNotification = `{"jsonrpc":"2.0","method":"notifications/initialized"}`

func TestServerListsEveryPortedToolOverStdio(t *testing.T) {
	got := speak(t, []string{
		initRequest,
		initNotification,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	})

	resp, ok := got[2]
	if !ok {
		t.Fatal("no response to tools/list")
	}
	var payload struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &payload); err != nil {
		t.Fatalf("tools/list result: %v", err)
	}
	if len(payload.Tools) != portedToolCount {
		t.Errorf("the running server advertises %d tools, want %d", len(payload.Tools), portedToolCount)
	}
}

// The end-to-end proof that --axi is reachable through MCP. llm-tools appended
// "--json --min" unconditionally, so no MCP client could ever obtain TOON no
// matter what the CLI supported.
func TestToolCallHonoursTheFormatArgumentOverStdio(t *testing.T) {
	got := speak(t, []string{
		initRequest,
		initNotification,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"llm_data_math","arguments":{"expression":"2+2"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"llm_data_math","arguments":{"expression":"6*7","format":"axi"}}}`,
	})

	text := func(id int) string {
		t.Helper()
		resp, ok := got[id]
		if !ok {
			t.Fatalf("no response to request %d", id)
		}
		var payload struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		}
		if err := json.Unmarshal(resp.Result, &payload); err != nil {
			t.Fatalf("request %d result: %v", id, err)
		}
		if payload.IsError {
			t.Fatalf("request %d returned an error: %s", id, resp.Result)
		}
		if len(payload.Content) == 0 {
			t.Fatalf("request %d returned no content", id)
		}
		return payload.Content[0].Text
	}

	jsonOut := strings.TrimSpace(text(2))
	if !strings.HasPrefix(jsonOut, "{") {
		t.Errorf("the default call did not return JSON: %q", jsonOut)
	}
	if !strings.Contains(jsonOut, "4") {
		t.Errorf("2+2 did not reach the CLI: %q", jsonOut)
	}

	axiOut := strings.TrimSpace(text(3))
	if strings.HasPrefix(axiOut, "{") {
		t.Errorf("format=axi still returned JSON: %q", axiOut)
	}
	if !strings.Contains(axiOut, "42") {
		t.Errorf("6*7 did not reach the CLI: %q", axiOut)
	}
}
