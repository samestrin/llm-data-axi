package mcpserver

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestCheckBinaryNamesWhatIsWrong defends TD 5678fa997aa13d9d: the startup
// check rejected only a missing path, so a directory or a file without the
// exec bit started the server and every tool call then failed. Each case
// must be rejected with the path and the reason, and a usable binary passed.
func TestCheckBinaryNamesWhatIsWrong(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no exec bit")
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "llm-data")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	noExec := filepath.Join(dir, "noexec")
	if err := os.WriteFile(noExec, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	asDir := filepath.Join(dir, "dir")
	if err := os.Mkdir(asDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := CheckBinary(exe); err != nil {
		t.Errorf("a usable binary was rejected: %v", err)
	}
	for _, tc := range []struct{ path, why string }{
		{filepath.Join(dir, "absent"), "not found"},
		{asDir, "is a directory"},
		{noExec, "is not executable"},
	} {
		err := CheckBinary(tc.path)
		if err == nil {
			t.Errorf("%s: accepted", tc.why)
			continue
		}
		if !strings.Contains(err.Error(), tc.path) || !strings.Contains(err.Error(), tc.why) {
			t.Errorf("%s: err = %q, want the path and the reason", tc.why, err)
		}
	}
}
