package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The README's ## Limits section and the catalog entry's exception are two
// lists of the same gaps. A reviewer at kunchenguid/axi reads the entry; a
// user reads the README. These tests defend against the two drifting apart:
// the README saying "selected search results" where the entry names the exact
// carriers, or dropping the session-start half of the lifecycle gap. Each
// phrase is checked against BOTH texts, so a reword on either side fails here
// instead of silently diverging.

// markdownSection returns the body of a level-2 heading, up to the next
// level-2 heading or end of file. ok is false when the heading is absent, so
// a renamed section fails the caller rather than matching against "".
func markdownSection(content, name string) (string, bool) {
	header := "## " + name
	lines := strings.Split(content, "\n")
	start := -1
	for i, l := range lines {
		if strings.TrimRight(l, " \t") == header {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return "", false
	}
	end := len(lines)
	for i := start; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			end = i
			break
		}
	}
	return strings.Join(lines[start:end], "\n"), true
}

// Defends the extractor both ways: a present section (including the last one
// in the file) is returned without its heading or the next section, and a
// missing or renamed section reports false instead of an empty match.
func TestMarkdownSection(t *testing.T) {
	doc := "# Title\n\n## Limits\n\n- gap one\n- gap two\n\n## Documentation\n\n- [entry](docs/axi-catalog-entry.yaml)\n"
	for _, c := range []struct {
		name string
		want string
		ok   bool
	}{
		{"Limits", "\n- gap one\n- gap two\n", true},
		{"Documentation", "\n- [entry](docs/axi-catalog-entry.yaml)\n", true},
		{"Known AXI gaps", "", false},
	} {
		got, ok := markdownSection(doc, c.name)
		if ok != c.ok || got != c.want {
			t.Errorf("markdownSection(%q) = %q, %v; want %q, %v", c.name, got, ok, c.want, c.ok)
		}
	}
}

func readmeFileSection(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRootDir, "README.md"))
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	section, ok := markdownSection(string(b), name)
	if !ok {
		t.Fatalf("README.md has no ## %s section", name)
	}
	return section
}

// Defends the task's done-when: the README's Limits and the entry's exception
// are word-for-word consistent in substance. Every phrase below must appear
// in both texts; the groups are the entry's two numbered remainders.
func TestReadmeLimitsMirrorTheCatalogException(t *testing.T) {
	e, _ := readCatalogEntry(t)
	exception := strings.ToLower(e.Admission.Exception)
	limits := strings.ToLower(readmeFileSection(t, "Limits"))

	gaps := map[string][]string{
		"session-end lifecycle-capture hook": {
			"session-end",
			"no session state",
			"no transcript",
			"record of files touched",
			"session-start",
			"claude code, codex and opencode",
		},
		"scoped help[] next steps": {
			"help[]",
			"scoped",
			"home view",
			"empty grep or multigrep",
			"truncated search line",
		},
	}
	for gap, phrases := range gaps {
		for _, p := range phrases {
			if !strings.Contains(exception, p) {
				t.Errorf("%s: exception no longer states %q (%s); update this guard with the entry", catalogEntryFile, p, gap)
			}
			if !strings.Contains(limits, p) {
				t.Errorf("README.md ## Limits does not state %q (%s); it must mirror the catalog exception", p, gap)
			}
		}
	}
}

// Defends the entry's discoverability: ## Documentation must link the YAML
// entry, not the deleted prose draft (whose absence another guard checks).
func TestReadmeDocumentationLinksTheCatalogEntry(t *testing.T) {
	doc := readmeFileSection(t, "Documentation")
	if !strings.Contains(doc, "](docs/axi-catalog-entry.yaml)") {
		t.Errorf("README.md ## Documentation does not link docs/axi-catalog-entry.yaml")
	}
}
