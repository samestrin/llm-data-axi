//go:build unix

package commands

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// Defends Decision 7: a FIFO named as the file is refused before anything
// opens it; reading one with no writer blocks forever.
func TestJSONQueryFIFOIsRefused(t *testing.T) {
	p := filepath.Join(t.TempDir(), "fifo.json")
	if err := syscall.Mkfifo(p, 0o644); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}
	out, code := runJSONQuery(t, "query", p, "a", "--json")
	if code != 1 || !strings.Contains(out, notRegularReason) || !strings.Contains(out, p) {
		t.Fatalf("exit %d, want 1 naming %s as %q\n%s", code, p, notRegularReason, out)
	}
}
