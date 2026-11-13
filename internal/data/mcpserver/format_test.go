package mcpserver

import (
	"errors"
	"io"
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

// The first fix for the duplicate encoding flags deleted every argument token
// spelled --json or --min, so a grep pattern "--json" vanished and the CLI
// failed with "requires at least 2 arg(s)", and an exclude named "--min" was
// dropped, silently widening a tree. A user value must reach the CLI intact.
func TestExecuteHandlerKeepsUserValuesSpelledLikeEncodingFlags(t *testing.T) {
	fakeBinary(t)

	for _, format := range []string{"", "json"} {
		args := map[string]interface{}{"pattern": "--json", "paths": []interface{}{"--min"}}
		if format != "" {
			args["format"] = format
		}
		out, err := ExecuteHandler(ToolPrefix+"grep", args)
		if err != nil {
			t.Fatalf("format=%q: ExecuteHandler: %v", format, err)
		}
		if !strings.HasPrefix(out, "grep --json --min ") {
			t.Errorf("format=%q: the pattern and path were not passed through: %q", format, out)
		}

		args = map[string]interface{}{"exclude": []interface{}{"--min"}}
		if format != "" {
			args["format"] = format
		}
		out, err = ExecuteHandler(ToolPrefix+"tree", args)
		if err != nil {
			t.Fatalf("format=%q: ExecuteHandler: %v", format, err)
		}
		if !strings.Contains(out, "--exclude --min") {
			t.Errorf("format=%q: the exclude value was dropped: %q", format, out)
		}
	}
}

// The README promises that a non-boolean LLM_DATA_MCP_AXI is reported on
// stderr. Without this test the warning line could be deleted and nothing
// would go red.
func TestLoadAXIDefaultWarnsOnStderrAboutAnUnreadableValue(t *testing.T) {
	t.Setenv("LLM_DATA_MCP_AXI", "sometimes")

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stderr
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = original })

	axi := loadAXIDefault()
	os.Stderr = original
	_ = w.Close()
	captured, _ := io.ReadAll(r)

	if !axi {
		t.Error("an unreadable LLM_DATA_MCP_AXI must keep the TOON default")
	}
	if !strings.Contains(string(captured), `LLM_DATA_MCP_AXI="sometimes"`) {
		t.Errorf("no warning naming the bad value on stderr, got %q", captured)
	}
}

func TestLoadAXIDefaultIsSilentForAValidValue(t *testing.T) {
	t.Setenv("LLM_DATA_MCP_AXI", "false")

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stderr
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = original })

	axi := loadAXIDefault()
	os.Stderr = original
	_ = w.Close()
	captured, _ := io.ReadAll(r)

	if axi {
		t.Error("LLM_DATA_MCP_AXI=false must select JSON")
	}
	if len(captured) != 0 {
		t.Errorf("a valid value must not warn, got %q", captured)
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
	// Pin the executable to an empty fixture dir: the sibling step must not
	// accidentally find a file beside the test binary itself.
	withExecutable(t, filepath.Join(fixtureDir(t), "llm-data-mcp"))
	if got := resolveBinaryPath(); got != "/usr/local/bin/llm-data" {
		t.Errorf("resolveBinaryPath() = %q, want the install-location fallback", got)
	}
}

// fixtureDir is t.TempDir with symlinks resolved, so a path built from it
// matches what os.Executable would report (on macOS /var -> /private/var).
func fixtureDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// withExecutable points the sibling lookup at a fixture path for one test.
// The running test binary is not llm-data-mcp, so the executable path must be
// injectable for the lookup to be testable at all.
func withExecutable(t *testing.T, path string) {
	t.Helper()
	original := osExecutable
	osExecutable = func() (string, error) { return path, nil }
	t.Cleanup(func() { osExecutable = original })
}

func writeBinaryFixture(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), mode); err != nil {
		t.Fatal(err)
	}
}

// Defends epic 09 T4 / AC4: install.sh puts both binaries in ~/.local/bin,
// which is off PATH in a fresh shell. The server must find llm-data beside
// its own executable instead of refusing to start.
func TestResolveBinaryPathFindsTheSiblingBesideTheServerExecutable(t *testing.T) {
	t.Setenv("LLM_DATA_BINARY", "")
	t.Setenv("PATH", "")
	dir := fixtureDir(t)
	withExecutable(t, filepath.Join(dir, "llm-data-mcp"))
	writeBinaryFixture(t, filepath.Join(dir, "llm-data"), 0o755)

	if got, want := resolveBinaryPath(), filepath.Join(dir, "llm-data"); got != want {
		t.Errorf("resolveBinaryPath() = %q, want the sibling %q", got, want)
	}
}

// The plan pins PATH before the sibling: a stale llm-data on PATH shadows the
// sibling, and that order is the accepted trade-off. If the order flips, this
// test goes red.
func TestResolveBinaryPathPrefersPATHOverTheSibling(t *testing.T) {
	t.Setenv("LLM_DATA_BINARY", "")
	pathDir := fixtureDir(t)
	writeBinaryFixture(t, filepath.Join(pathDir, "llm-data"), 0o755)
	t.Setenv("PATH", pathDir)

	exeDir := fixtureDir(t)
	withExecutable(t, filepath.Join(exeDir, "llm-data-mcp"))
	writeBinaryFixture(t, filepath.Join(exeDir, "llm-data"), 0o755)

	if got, want := resolveBinaryPath(), filepath.Join(pathDir, "llm-data"); got != want {
		t.Errorf("resolveBinaryPath() = %q, want the PATH entry %q", got, want)
	}
}

// The override must beat every lookup step, not just PATH: CI points it at
// ./build even when a sibling or a PATH entry exists.
func TestResolveBinaryPathPrefersTheOverrideOverPATHAndTheSibling(t *testing.T) {
	const override = "/somewhere/else/llm-data"
	t.Setenv("LLM_DATA_BINARY", override)
	pathDir := fixtureDir(t)
	writeBinaryFixture(t, filepath.Join(pathDir, "llm-data"), 0o755)
	t.Setenv("PATH", pathDir)
	exeDir := fixtureDir(t)
	withExecutable(t, filepath.Join(exeDir, "llm-data-mcp"))
	writeBinaryFixture(t, filepath.Join(exeDir, "llm-data"), 0o755)

	if got := resolveBinaryPath(); got != override {
		t.Errorf("resolveBinaryPath() = %q, want the override %q", got, override)
	}
}

// A sibling that cannot be exec'd must not be reported as found: the server
// would start and every tool call would fail — a success report for a broken
// install. Nothing may report success when it did not succeed.
func TestResolveBinaryPathSkipsANonExecutableSibling(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no exec bit; usability there is the extension")
	}
	t.Setenv("LLM_DATA_BINARY", "")
	t.Setenv("PATH", "")

	t.Run("file without the exec bit", func(t *testing.T) {
		dir := fixtureDir(t)
		withExecutable(t, filepath.Join(dir, "llm-data-mcp"))
		writeBinaryFixture(t, filepath.Join(dir, "llm-data"), 0o644)
		if got := resolveBinaryPath(); got != "/usr/local/bin/llm-data" {
			t.Errorf("resolveBinaryPath() = %q, want the fallback, not the non-executable sibling", got)
		}
	})

	t.Run("directory named llm-data", func(t *testing.T) {
		dir := fixtureDir(t)
		withExecutable(t, filepath.Join(dir, "llm-data-mcp"))
		if err := os.Mkdir(filepath.Join(dir, "llm-data"), 0o755); err != nil {
			t.Fatal(err)
		}
		if got := resolveBinaryPath(); got != "/usr/local/bin/llm-data" {
			t.Errorf("resolveBinaryPath() = %q, want the fallback, not the directory", got)
		}
	})
}

// When the executable's own location cannot be determined, the lookup must
// fall through to the install location, not panic or return a bare name.
func TestResolveBinaryPathFallsBackWhenTheExecutableIsUnknown(t *testing.T) {
	t.Setenv("LLM_DATA_BINARY", "")
	t.Setenv("PATH", "")
	original := osExecutable
	osExecutable = func() (string, error) { return "", errors.New("no executable") }
	t.Cleanup(func() { osExecutable = original })

	if got := resolveBinaryPath(); got != "/usr/local/bin/llm-data" {
		t.Errorf("resolveBinaryPath() = %q, want the install-location fallback", got)
	}
}
