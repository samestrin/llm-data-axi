//go:build unix

package commands

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Defends readMarkdownFile against a FIFO named as the frontmatter file:
// opening one with no writer blocks forever. It is refused before anything
// opens it, exit 1, naming the path.
func TestMarkdownFrontmatterRefusesAFIFO(t *testing.T) {
	fifo := filepath.Join(fifoTree(t), "pipe.md")
	type res struct {
		code   int
		stdout string
	}
	done := make(chan res, 1)
	go func() {
		c, out, _ := runFrontmatterCLI(t, "markdown", "frontmatter", "--json", fifo)
		done <- res{c, out}
	}()
	select {
	case r := <-done:
		if r.code != 1 || !strings.Contains(r.stdout, notRegularReason) || !strings.Contains(r.stdout, fifo) {
			t.Fatalf("exit %d, want 1 naming %s as %s: %q", r.code, fifo, notRegularReason, r.stdout)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("markdown frontmatter blocked reading a FIFO")
	}
}