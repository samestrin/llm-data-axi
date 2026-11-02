package mcpserver

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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

// encodingFlags returns the encoding flags the fake binary saw, in order.
// Positional tokens are dropped so a test asserts exactly what was asked for.
func encodingFlags(out string) []string {
	var flags []string
	for _, tok := range strings.Fields(out) {
		if tok == "--axi" || tok == "--json" || tok == "--min" {
			flags = append(flags, tok)
		}
	}
	return flags
}

// withServerDefault sets the server-wide encoding for one test.
func withServerDefault(t *testing.T, axi bool) {
	t.Helper()
	original := AXIFormat
	AXIFormat = axi
	t.Cleanup(func() { AXIFormat = original })
}

// llm-tools appended "--json --min" unconditionally, so --axi was unreachable
// through MCP no matter what the CLI supported. Epic 03 T6 then made TOON the
// default. These tests pin that both routes exist and that exactly one
// encoding is sent: most builders also emit --json themselves, and a default
// call that sent "--json --min --axi" would get TOON only by the Formatter's
// precedence rule, not because that is what was asked for.
func TestExecuteHandlerDefaultsToAXIForEveryTool(t *testing.T) {
	fakeBinary(t)
	withServerDefault(t, true)

	for _, tool := range GetToolDefinitions() {
		out, err := ExecuteHandler(tool.Name, map[string]interface{}{})
		if err != nil {
			t.Fatalf("%s: ExecuteHandler: %v", tool.Name, err)
		}
		if got := encodingFlags(out); len(got) != 1 || got[0] != "--axi" {
			t.Errorf("%s: a call with no format sent %v, want exactly [--axi] (command line %q)", tool.Name, got, out)
		}
	}
}

func TestExecuteHandlerSendsJSONMinWhenTheCallAsksForJSON(t *testing.T) {
	fakeBinary(t)
	withServerDefault(t, true)

	for _, tool := range GetToolDefinitions() {
		if tool.Name == ToolPrefix+"runtime" {
			// runtime's own "format" argument is its time format; covered below.
			continue
		}
		out, err := ExecuteHandler(tool.Name, map[string]interface{}{"format": "json"})
		if err != nil {
			t.Fatalf("%s: ExecuteHandler: %v", tool.Name, err)
		}
		if got := strings.Join(encodingFlags(out), " "); got != "--json --min" {
			t.Errorf("%s: format=json sent %q, want exactly \"--json --min\" (command line %q)", tool.Name, got, out)
		}
	}
}

func TestExecuteHandlerRequestsAXIWhenTheCallAsksForIt(t *testing.T) {
	fakeBinary(t)
	// The server-wide default says JSON; the call must still win.
	withServerDefault(t, false)

	for _, format := range []string{"axi", "toon", "AXI"} {
		out, err := ExecuteHandler(ToolPrefix+"count", map[string]interface{}{"format": format})
		if err != nil {
			t.Fatalf("format=%s: ExecuteHandler: %v", format, err)
		}
		if got := encodingFlags(out); len(got) != 1 || got[0] != "--axi" {
			t.Errorf("format=%s sent %v, want exactly [--axi]", format, got)
		}
	}
}

func TestExecuteHandlerHonoursAServerWideJSONDefault(t *testing.T) {
	fakeBinary(t)
	withServerDefault(t, false)

	out, err := ExecuteHandler(ToolPrefix+"count", map[string]interface{}{})
	if err != nil {
		t.Fatalf("ExecuteHandler: %v", err)
	}
	if got := strings.Join(encodingFlags(out), " "); got != "--json --min" {
		t.Errorf("LLM_DATA_MCP_AXI=false sent %q, want \"--json --min\"", got)
	}
}

// A format the server does not know must fail, not fall back to some default:
// a caller asking for "yaml" and silently getting TOON is a false success.
func TestExecuteHandlerRejectsAnUnknownFormat(t *testing.T) {
	fakeBinary(t)

	out, err := ExecuteHandler(ToolPrefix+"count", map[string]interface{}{"format": "yaml"})
	if err == nil {
		t.Fatalf("format=yaml was accepted; the command line was %q", out)
	}
	if !strings.Contains(err.Error(), "yaml") || !strings.Contains(err.Error(), "json") {
		t.Errorf("the error does not name the bad value and the valid ones: %v", err)
	}
}

// runtime advertises its own "format" argument (secs, hms, human, ...). It must
// reach the CLI as --format and must not be read as an encoding, or every
// runtime call with a time format would fail as an unknown encoding.
func TestRuntimeFormatIsItsTimeFormatNotTheEncoding(t *testing.T) {
	fakeBinary(t)
	withServerDefault(t, true)

	out, err := ExecuteHandler(ToolPrefix+"runtime", map[string]interface{}{"start": 1, "format": "hms"})
	if err != nil {
		t.Fatalf("ExecuteHandler: %v", err)
	}
	if !strings.Contains(out, "--format hms") {
		t.Errorf("runtime's time format did not reach the CLI: %q", out)
	}
	if got := encodingFlags(out); len(got) != 1 || got[0] != "--axi" {
		t.Errorf("runtime with a time format sent %v, want the server default [--axi]", got)
	}
}

func TestAXIDefaultFromEnv(t *testing.T) {
	cases := []struct {
		value   string
		want    bool
		wantErr bool
	}{
		{"", true, false},
		{"true", true, false},
		{"1", true, false},
		{"false", false, false},
		{"0", false, false},
		// An unreadable value keeps the default but is reported, not swallowed.
		{"sometimes", true, true},
	}
	for _, c := range cases {
		got, err := axiDefaultFromEnv(c.value)
		if got != c.want || (err != nil) != c.wantErr {
			t.Errorf("axiDefaultFromEnv(%q) = %v, %v; want %v, error %v", c.value, got, err, c.want, c.wantErr)
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
