//go:build unix

package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// runCLIWithin is runCLI that fails the test instead of hanging the suite
// when the command blocks: opening a FIFO with no writer, or reading
// /dev/zero to the end, never returns.
func runCLIWithin(t *testing.T, args ...string) (code int, stdout string) {
	t.Helper()
	type res struct {
		code   int
		stdout string
	}
	done := make(chan res, 1)
	go func() {
		c, out, _ := runCLI(t, args...)
		done <- res{c, out}
	}()
	select {
	case r := <-done:
		return r.code, r.stdout
	case <-time.After(10 * time.Second):
		t.Fatalf("%v did not return within 10s: it is reading a file that never ends", args)
		return 0, ""
	}
}

// fifoTree is a directory with one text file and one FIFO nobody writes to.
func fifoTree(t *testing.T) string {
	t.Helper()
	dir := realTempDir(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("needle one\nsecond line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe.txt"), 0o644); err != nil {
		t.Skipf("cannot make a FIFO here: %v", err)
	}
	return dir
}

// Defends against hanging on a named special file: catfiles, template and
// count --mode lines read /dev/zero until memory ran out, because the size
// limits use the stat size, which is 0 for a device. A file that is not a
// regular file is refused, exit 1, naming it.
func TestNamedNonRegularFileIsRefused(t *testing.T) {
	if _, err := os.Stat("/dev/zero"); err != nil {
		t.Skip("no /dev/zero here")
	}
	for _, args := range [][]string{
		{"catfiles", "/dev/zero"},
		{"template", "/dev/zero"},
		{"count", "--mode", "lines", "--path", "/dev/zero"},
		{"grep", "x", "/dev/zero"},
	} {
		code, stdout := runCLIWithin(t, args...)
		if code != 1 {
			t.Errorf("%v: exit %d, want 1; stdout %q", args, code, stdout)
		}
		if !strings.Contains(stdout, "not a regular file") || !strings.Contains(stdout, "/dev/zero") {
			t.Errorf("%v: want an error naming /dev/zero as not a regular file, got %q", args, stdout)
		}
	}
}

// Defends a directory walk against a FIFO: grep, catfiles, count -r and
// multigrep opened it and blocked forever. The walk reads regular files only
// and says how many it passed over, so the skipped file is not silently part
// of the result.
func TestWalkSkipsAFIFOAndReportsIt(t *testing.T) {
	dir := fifoTree(t)

	t.Run("grep", func(t *testing.T) {
		code, stdout := runCLIWithin(t, "grep", "needle", dir, "--json")
		if code != 0 {
			t.Fatalf("exit %d: %s", code, stdout)
		}
		var got GrepResult
		if err := json.Unmarshal([]byte(stdout), &got); err != nil {
			t.Fatalf("%v: %s", err, stdout)
		}
		if got.TotalMatches != 1 || len(got.NonRegularSkipped) != 1 || !strings.HasSuffix(got.NonRegularSkipped[0], "pipe.txt") {
			t.Errorf("want 1 match and pipe.txt in non_regular_skipped, got %+v", got)
		}
	})

	t.Run("catfiles", func(t *testing.T) {
		code, stdout := runCLIWithin(t, "catfiles", dir, "--json")
		if code != 0 {
			t.Fatalf("exit %d: %s", code, stdout)
		}
		var got CatfilesResult
		if err := json.Unmarshal([]byte(stdout), &got); err != nil {
			t.Fatalf("%v: %s", err, stdout)
		}
		if got.FileCount != 1 || len(got.Skipped) != 1 || !strings.HasSuffix(got.Skipped[0].Path, "pipe.txt") || got.Skipped[0].Reason != "not a regular file" {
			t.Errorf("want a.txt read and pipe.txt skipped as not a regular file, got %+v", got)
		}
	})

	t.Run("count", func(t *testing.T) {
		code, stdout := runCLIWithin(t, "count", "--mode", "lines", "--path", dir, "-r", "--json")
		if code != 0 {
			t.Fatalf("exit %d: %s", code, stdout)
		}
		var got CountLinesResult
		if err := json.Unmarshal([]byte(stdout), &got); err != nil {
			t.Fatalf("%v: %s", err, stdout)
		}
		if got.Count != 2 || got.Skipped != 1 {
			t.Errorf("want 2 lines and 1 skipped, got %+v", got)
		}
	})

	t.Run("multigrep", func(t *testing.T) {
		code, stdout := runCLIWithin(t, "multigrep", "--path", dir, "--keywords", "needle", "--json")
		if code != 0 {
			t.Fatalf("exit %d: %s", code, stdout)
		}
		var got struct {
			FilesSearched     int      `json:"files_searched"`
			NonRegularSkipped []string `json:"non_regular_skipped"`
		}
		if err := json.Unmarshal([]byte(stdout), &got); err != nil {
			t.Fatalf("%v: %s", err, stdout)
		}
		if got.FilesSearched != 1 || len(got.NonRegularSkipped) != 1 {
			t.Errorf("want 1 file searched and 1 skipped, got %+v", got)
		}
	})
}
