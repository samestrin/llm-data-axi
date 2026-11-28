//go:build unix

package commands

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Defends readMarkdownFile on the section path against a FIFO named as the
// file: opening one with no writer blocks forever. It is refused before
// anything opens it, exit 1, naming the path.
func TestMarkdownSectionRefusesAFIFO(t *testing.T) {
	fifo := filepath.Join(fifoTree(t), "pipe.txt")
	type res struct {
		code   int
		stdout string
	}
	done := make(chan res, 1)
	go func() {
		c, out, _ := runMarkdownSectionCLI(t, "markdown", "section", "--json", fifo, "Target")
		done <- res{c, out}
	}()
	select {
	case r := <-done:
		if r.code != 1 || !strings.Contains(r.stdout, notRegularReason) || !strings.Contains(r.stdout, fifo) {
			t.Fatalf("exit %d, want 1 naming %s as %s: %q", r.code, fifo, notRegularReason, r.stdout)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("markdown section blocked reading a FIFO")
	}
}
