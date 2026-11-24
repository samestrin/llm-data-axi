package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Defends catfiles' stdout as the whole story: the size limit and a binary
// skip were reported only on stderr, so stdout read as every file in full. A
// file left out must be named on stdout, and a stop at the limit must say so.
func TestCatfilesReportsTruncationAndSkipsOnStdout(t *testing.T) {
	dir := realTempDir(t)
	big := strings.Repeat("x", 700*1024) + "\n"
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(big), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "c.bin"), []byte("bin\x00ary"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, stdout, _ := runCLI(t, "catfiles", dir, "--max-size", "1", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stdout)
	}
	var got CatfilesResult
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("%v: %s", err, stdout)
	}
	// a.txt fits; b.txt does not, and c.bin sorts after it, so two are not read.
	if got.FileCount != 1 || !got.Truncated || got.NotRead != 2 {
		t.Errorf("want 1 file read, truncated, 2 not read; got count %d truncated %v not_read %d", got.FileCount, got.Truncated, got.NotRead)
	}
	if len(got.Help) == 0 || !strings.Contains(strings.Join(got.Help, " "), "--max-size") {
		t.Errorf("a truncated run must offer --max-size as the next step, got %v", got.Help)
	}

	code, stdout, _ = runCLI(t, "catfiles", filepath.Join(dir, "c.bin"), filepath.Join(dir, "a.txt"))
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stdout)
	}
	if !strings.Contains(stdout, "c.bin") || !strings.Contains(stdout, "binary") {
		t.Errorf("TOON must name the skipped binary file, got %q", truncForMsg(stdout))
	}
}
