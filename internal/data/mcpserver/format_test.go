package mcpserver

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeBinary installs a shell script in place of the real llm-data binary. The
// script echoes the arguments it was given, so a test can assert what the
// handler actually put on the command line rather than what it meant to.
func fakeBinary(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake binary is a shell script")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "llm-data")
	body := "#!/bin/sh\necho \"$@\"\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	original := BinaryPath
	BinaryPath = script
	t.Cleanup(func() { BinaryPath = original })
}

// llm-tools appended "--json --min" unconditionally, so --axi was unreachable
// through MCP no matter what the CLI supported. These tests pin that both
// routes exist and that exactly one encoding flag is sent.
func TestExecuteHandlerRequestsAXIWhenTheCallAsksForIt(t *testing.T) {
	fakeBinary(t)

	out, err := ExecuteHandler(ToolPrefix+"math", map[string]interface{}{
		"expression": "2+2",
		"format":     "axi",
	})
	if err != nil {
		t.Fatalf("ExecuteHandler: %v", err)
	}
	if !strings.Contains(out, "--axi") {
		t.Errorf("expected --axi on the command line, got %q", out)
	}
	if strings.Contains(out, "--json") {
		t.Errorf("--json must not be sent alongside --axi, got %q", out)
	}
}

func TestExecuteHandlerDefaultsToJSONMin(t *testing.T) {
	fakeBinary(t)

	out, err := ExecuteHandler(ToolPrefix+"math", map[string]interface{}{
		"expression": "2+2",
	})
	if err != nil {
		t.Fatalf("ExecuteHandler: %v", err)
	}
	if !strings.Contains(out, "--json") || !strings.Contains(out, "--min") {
		t.Errorf("expected --json --min by default, got %q", out)
	}
	if strings.Contains(out, "--axi") {
		t.Errorf("--axi must not be sent by default, got %q", out)
	}
}

func TestExecuteHandlerHonoursTheServerWideAXIDefault(t *testing.T) {
	fakeBinary(t)

	original := AXIFormat
	AXIFormat = true
	t.Cleanup(func() { AXIFormat = original })

	out, err := ExecuteHandler(ToolPrefix+"math", map[string]interface{}{
		"expression": "2+2",
	})
	if err != nil {
		t.Fatalf("ExecuteHandler: %v", err)
	}
	if !strings.Contains(out, "--axi") {
		t.Errorf("expected --axi from the server-wide default, got %q", out)
	}
}

func TestPerCallFormatOverridesTheServerWideDefault(t *testing.T) {
	original := AXIFormat
	AXIFormat = true
	t.Cleanup(func() { AXIFormat = original })

	if wantsAXI(map[string]interface{}{"format": "json"}) {
		t.Error("an explicit format=json must beat the server-wide AXI default")
	}
	if !wantsAXI(map[string]interface{}{"format": "toon"}) {
		t.Error("format=toon must select AXI")
	}
	if !wantsAXI(map[string]interface{}{"format": "AXI"}) {
		t.Error("the format argument must be case-insensitive")
	}
}

// The two commands that reach pkg/llmapi were both absent from llm-tools'
// llmCommands map, so the only two tools that make a network LLM call were the
// two capped at the 60-second local timeout.
func TestLLMCommandsCoversEveryCommandThatCallsAnLLM(t *testing.T) {
	for _, name := range []string{"extract_relevant", "extract_links"} {
		if !llmCommands[name] {
			t.Errorf("%s calls an LLM API and must get LLMTimeout, not CommandTimeout", name)
		}
	}
	if LLMTimeout <= CommandTimeout {
		t.Errorf("LLMTimeout (%v) must exceed CommandTimeout (%v)", LLMTimeout, CommandTimeout)
	}
	if LLMTimeout != 300*time.Second {
		t.Errorf("LLMTimeout = %v, want 300s", LLMTimeout)
	}
}

// Every command that llmCommands names must actually be registered, or the map
// is granting a longer timeout to a tool that does not exist.
func TestLLMCommandsNamesOnlyRegisteredTools(t *testing.T) {
	registered := make(map[string]bool)
	for _, tool := range GetToolDefinitions() {
		registered[strings.TrimPrefix(tool.Name, ToolPrefix)] = true
	}
	for name := range llmCommands {
		if !registered[name] {
			t.Errorf("llmCommands names %q, which is not a registered tool", name)
		}
	}
}

func TestResolveBinaryPathPrefersTheExplicitOverride(t *testing.T) {
	t.Setenv("LLM_DATA_BINARY", "/somewhere/else/llm-data")
	if got := resolveBinaryPath(); got != "/somewhere/else/llm-data" {
		t.Errorf("resolveBinaryPath() = %q, want the override", got)
	}
}

func TestResolveBinaryPathFallsBackToTheInstallLocation(t *testing.T) {
	t.Setenv("LLM_DATA_BINARY", "")
	// An empty PATH makes LookPath fail, which is the branch under test.
	t.Setenv("PATH", "")
	if got := resolveBinaryPath(); got != "/usr/local/bin/llm-data" {
		t.Errorf("resolveBinaryPath() = %q, want the install-location fallback", got)
	}
}
