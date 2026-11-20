package commands

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

// docs/axi-catalog-entry.yaml is the entry the owner appends to the
// `community` list in kunchenguid/axi's catalog.yaml. A reviewer there reads
// the source at reviewed_revision, so a claim the source contradicts sinks
// the entry. These tests hold the claims a test can check to the code; the
// one they cannot, that reviewed_revision is the commit the release tag
// carries, is the Reviewer's `git rev-parse v2.0.0^{commit}` (the tag is not
// in a CI checkout).

const catalogEntryFile = "docs/axi-catalog-entry.yaml"

type catalogEntry struct {
	Name        string `yaml:"name"`
	URL         string `yaml:"url"`
	Author      string `yaml:"author"`
	Domain      string `yaml:"domain"`
	Description string `yaml:"description"`
	Admission   struct {
		Status             string   `yaml:"status"`
		ReviewedRevision   string   `yaml:"reviewed_revision"`
		ReviewedComponents []string `yaml:"reviewed_components"`
		SourceObservations []string `yaml:"source_observations"`
		Exception          string   `yaml:"exception"`
	} `yaml:"admission"`
}

// readCatalogEntry parses the entry as the catalog does: a one-item list.
// A missing file, a parse error or any other item count fails the test
// rather than passing on an empty value.
func readCatalogEntry(t *testing.T) (catalogEntry, string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRootDir, catalogEntryFile))
	if err != nil {
		t.Fatalf("read %s: %v", catalogEntryFile, err)
	}
	var entries []catalogEntry
	if err := yaml.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("%s does not parse as YAML: %v", catalogEntryFile, err)
	}
	if len(entries) != 1 {
		t.Fatalf("%s holds %d entries, want exactly 1", catalogEntryFile, len(entries))
	}
	return entries[0], string(raw)
}

// repoRootDir is the repository root as seen from this package's directory.
var repoRootDir = filepath.Join("..", "..", "..")

// Defends the shape the catalog's docs:gen expects and the values the plan
// fixed (author, domain, status), so a field typo is not found by the
// catalog's CI after the PR is public.
func TestCatalogEntryHasTheCatalogFields(t *testing.T) {
	e, _ := readCatalogEntry(t)
	for field, got := range map[string]string{
		"name":        e.Name,
		"url":         e.URL,
		"description": e.Description,
		"exception":   e.Admission.Exception,
	} {
		if strings.TrimSpace(got) == "" {
			t.Errorf("%s: %s is empty", catalogEntryFile, field)
		}
	}
	for field, c := range map[string][2]string{
		"name":             {e.Name, "llm-data-axi"},
		"url":              {e.URL, "https://github.com/samestrin/llm-data-axi"},
		"author":           {e.Author, "samestrin"},
		"domain":           {e.Domain, "Data and Project Analysis"},
		"admission.status": {e.Admission.Status, "exception"},
	} {
		if c[0] != c[1] {
			t.Errorf("%s: %s = %q, want %q", catalogEntryFile, field, c[0], c[1])
		}
	}
	if len(e.Admission.SourceObservations) == 0 {
		t.Errorf("%s: source_observations is empty", catalogEntryFile)
	}
}

// Defends against a pin a reviewer cannot check out: an abbreviated sha, a
// branch name or a placeholder left from the draft.
func TestCatalogEntryPinsAFullCommit(t *testing.T) {
	e, _ := readCatalogEntry(t)
	if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(e.Admission.ReviewedRevision) {
		t.Errorf("%s: reviewed_revision %q is not a full 40-character commit sha", catalogEntryFile, e.Admission.ReviewedRevision)
	}
}

// Defends against citing a file the reviewer will not find: every reviewed
// component must exist in this tree.
func TestCatalogEntryReviewedComponentsExist(t *testing.T) {
	e, _ := readCatalogEntry(t)
	if len(e.Admission.ReviewedComponents) == 0 {
		t.Fatalf("%s: reviewed_components is empty", catalogEntryFile)
	}
	for _, c := range e.Admission.ReviewedComponents {
		if _, err := os.Stat(filepath.Join(repoRootDir, c)); err != nil {
			t.Errorf("%s: reviewed component %s: %v", catalogEntryFile, c, err)
		}
	}
}

// Defends the exception against the gaps epic 11 closed and against listing
// exit 2, which the spec asks for: the exception may name only the two
// remainders docs/axi-audit.md states.
func TestCatalogEntryExceptionNamesOnlyTheLiveGaps(t *testing.T) {
	e, _ := readCatalogEntry(t)
	ex := strings.ToLower(e.Admission.Exception)
	for _, want := range []string{"session-end", "no session state", "help[]"} {
		if !strings.Contains(ex, want) {
			t.Errorf("%s: exception does not state %q", catalogEntryFile, want)
		}
	}
	for _, gone := range []string{"exit 2", "exit code 2", "usage error", "stderr", "opt-in", "--axi", "no codex", "no opencode", "claude code only", "claude-only"} {
		if strings.Contains(ex, gone) {
			t.Errorf("%s: exception still lists %q; that is not a gap at this revision", catalogEntryFile, gone)
		}
	}
}

// Defends the counts against the binary: every "<n> commands" and
// "<n> yaml subcommands" the entry quotes must equal what llm-data --help
// lists (completion and help are cobra's, hidden commands are the moved
// stubs), and the entry must quote each at least once.
func TestCatalogEntryCommandCountsMatchHelp(t *testing.T) {
	_, raw := readCatalogEntry(t)

	root := NewRootCmd()
	top, yamlSubs := 0, 0
	for _, c := range root.Commands() {
		if c.Hidden || c.Name() == "help" || c.Name() == "completion" {
			continue
		}
		top++
		if c.Name() == "yaml" {
			for _, s := range c.Commands() {
				if !s.Hidden && s.Name() != "help" {
					yamlSubs++
				}
			}
		}
	}

	check := func(re *regexp.Regexp, want int, what string) {
		t.Helper()
		ms := re.FindAllStringSubmatch(raw, -1)
		if len(ms) == 0 {
			t.Errorf("%s never quotes the %s count (%d)", catalogEntryFile, what, want)
		}
		for _, m := range ms {
			if n, _ := strconv.Atoi(m[1]); n != want {
				t.Errorf("%s says %q; llm-data --help lists %d %s", catalogEntryFile, m[0], want, what)
			}
		}
	}
	check(regexp.MustCompile(`\b(\d+) (?:top-level )?commands\b`), top, "commands")
	check(regexp.MustCompile(`\b(\d+) yaml subcommands\b`), yamlSubs, "yaml subcommands")
}

// Defends the swap from the prose draft: the .md must be gone and nothing in
// the public docs may still link it.
func TestCatalogEntryProseDraftIsGone(t *testing.T) {
	if _, err := os.Stat(filepath.Join(repoRootDir, "docs", "axi-catalog-entry.md")); err == nil {
		t.Errorf("docs/axi-catalog-entry.md still exists; %s replaces it", catalogEntryFile)
	}
	for _, f := range []string{"README.md", "docs/axi-audit.md", "docs/release-checklist.md"} {
		b, err := os.ReadFile(filepath.Join(repoRootDir, f))
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if strings.Contains(string(b), "axi-catalog-entry.md") {
			t.Errorf("%s still links docs/axi-catalog-entry.md", f)
		}
	}
}

// workflowGlobMatches reports whether a GitHub Actions `paths` glob matches a
// repository path: `**` crosses directories, `*` stays within one.
func workflowGlobMatches(glob, path string) bool {
	var re strings.Builder
	re.WriteString("^")
	for i := 0; i < len(glob); i++ {
		switch {
		case strings.HasPrefix(glob[i:], "**"):
			re.WriteString(".*")
			i++
		case glob[i] == '*':
			re.WriteString("[^/]*")
		default:
			re.WriteString(regexp.QuoteMeta(string(glob[i])))
		}
	}
	re.WriteString("$")
	return regexp.MustCompile(re.String()).MatchString(path)
}

// Defends the guards above against never running: test.yml filters on paths,
// and a pull request that edits only the entry (a re-pin, a re-quoted
// benchmark) registered zero checks because no glob matched the .yaml.
func TestCatalogEntryEditTriggersCI(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRootDir, ".github", "workflows", "test.yml"))
	if err != nil {
		t.Fatalf("read test.yml: %v", err)
	}
	var wf struct {
		On map[string]struct {
			Paths []string `yaml:"paths"`
		} `yaml:"on"`
	}
	if err := yaml.Unmarshal(raw, &wf); err != nil {
		t.Fatalf("test.yml does not parse: %v", err)
	}
	for _, event := range []string{"push", "pull_request"} {
		trig, ok := wf.On[event]
		if !ok {
			t.Errorf("test.yml has no %s trigger", event)
			continue
		}
		if len(trig.Paths) == 0 {
			continue // no filter: every change runs CI
		}
		matched := false
		for _, g := range trig.Paths {
			if workflowGlobMatches(g, catalogEntryFile) {
				matched = true
			}
		}
		if !matched {
			t.Errorf("test.yml %s paths %v do not match %s, so an edit to it alone runs no check", event, trig.Paths, catalogEntryFile)
		}
	}
	// The matcher itself, both ways, so a broken matcher cannot pass the check.
	for _, c := range []struct {
		glob, path string
		want       bool
	}{
		{"**.md", "docs/axi-audit.md", true},
		{"**.go", "docs/axi-catalog-entry.yaml", false},
		{"docs/demo/**", "docs/demo/llm-data.tape", true},
		{"docs/demo/**", "docs/axi-catalog-entry.yaml", false},
		{"go.mod", "go.mod", true},
	} {
		if got := workflowGlobMatches(c.glob, c.path); got != c.want {
			t.Errorf("workflowGlobMatches(%q, %q) = %v, want %v", c.glob, c.path, got, c.want)
		}
	}
}
