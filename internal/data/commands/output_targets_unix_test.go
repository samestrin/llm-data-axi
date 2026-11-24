//go:build unix

package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// Defends template -o against a FIFO: os.WriteFile opened it and blocked for a
// reader that never came, so the command hung while the changelog said no
// command hangs on a FIFO. A target that is not a regular file or a new path
// is refused before anything opens it, exit 2.
func TestTemplateOutputRefusesAFIFOAndASymlink(t *testing.T) {
	dir := realTempDir(t)
	tmpl := filepath.Join(dir, "t.txt")
	if err := os.WriteFile(tmpl, []byte("rendered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(dir, "out.fifo")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skipf("cannot make a FIFO here: %v", err)
	}
	src := filepath.Join(dir, "src.txt")
	if err := os.WriteFile(src, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(src, link); err != nil {
		t.Fatal(err)
	}

	for _, target := range []string{fifo, link} {
		code, stdout := runCLIWithin(t, "template", tmpl, "-o", target)
		if code != 2 {
			t.Errorf("-o %s: exit %d, want 2; stdout %q", target, code, stdout)
		}
		if !strings.Contains(stdout, "not a regular file") || !strings.Contains(stdout, target) {
			t.Errorf("-o %s: want an error naming it, got %q", target, stdout)
		}
	}
	if got := readString(t, src); got != "original\n" {
		t.Errorf("the symlink's target was written through: %q", got)
	}
}

// Defends multigrep --output-dir against what already sits at a keyword's
// file name: a FIFO hung the run, and a symlink was written through, which
// overwrote the source file it pointed at. Every target is checked before any
// is written; one that is not a regular file or a new path fails the run and
// nothing is written.
func TestMultigrepOutputDirRefusesAFIFOAndASymlink(t *testing.T) {
	setup := func(t *testing.T) (src, out string) {
		dir := realTempDir(t)
		if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
			t.Fatal(err)
		}
		src = filepath.Join(dir, "src", "f.txt")
		if err := os.WriteFile(src, []byte("needle here\nother here\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		out = filepath.Join(dir, "out")
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
		return src, out
	}
	check := func(t *testing.T, src, out, planted string, code int, stdout string) {
		t.Helper()
		if code != 1 {
			t.Errorf("exit %d, want 1; stdout %q", code, stdout)
		}
		var got struct {
			Error        bool `json:"error"`
			FailedWrites []struct {
				File   string `json:"file"`
				Reason string `json:"reason"`
			} `json:"failed_writes"`
		}
		if err := json.Unmarshal([]byte(stdout), &got); err != nil {
			t.Fatalf("%v: %s", err, stdout)
		}
		if !got.Error || len(got.FailedWrites) != 1 || got.FailedWrites[0].File != planted || got.FailedWrites[0].Reason != "not a regular file" {
			t.Errorf("want failed_writes naming %s as not a regular file, got %+v", planted, got)
		}
		if b := readString(t, src); b != "needle here\nother here\n" {
			t.Errorf("source file changed: %q", b)
		}
		if _, err := os.Lstat(filepath.Join(out, "keyword_other.txt")); !os.IsNotExist(err) {
			t.Errorf("keyword_other.txt was written though the run failed: %v", err)
		}
	}

	t.Run("fifo", func(t *testing.T) {
		src, out := setup(t)
		planted := filepath.Join(out, "keyword_needle.txt")
		if err := syscall.Mkfifo(planted, 0o644); err != nil {
			t.Skipf("cannot make a FIFO here: %v", err)
		}
		code, stdout := runCLIWithin(t, "multigrep", "--path", src, "--keywords", "needle,other", "--output-dir", out, "--json")
		check(t, src, out, planted, code, stdout)
	})

	t.Run("symlink", func(t *testing.T) {
		src, out := setup(t)
		planted := filepath.Join(out, "keyword_needle.txt")
		if err := os.Symlink(src, planted); err != nil {
			t.Fatal(err)
		}
		code, stdout := runCLIWithin(t, "multigrep", "--path", src, "--keywords", "needle,other", "--output-dir", out, "--json")
		check(t, src, out, planted, code, stdout)
	})
}
