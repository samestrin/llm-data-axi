package commands

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/llm-data-axi/v2/pkg/output"
)

// runMarkdownSectionCLI is runMarkdownCLI with the section subcommand added
// to the markdown parent. newMarkdownSectionCmd is not added to the parent
// until epic 20 T6, so the tests build the tree T6 will build.
func runMarkdownSectionCLI(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	restoreOutputMode(t)
	var out, errOut bytes.Buffer
	root := NewRootCmd()
	md := newMarkdownCmd()
	md.AddCommand(newMarkdownSectionCmd())
	argErrorsAreUsage(md)
	root.AddCommand(md)
	root.SetArgs(args)
	root.SetOut(&out)
	root.SetErr(&errOut)
	cmd, err := root.ExecuteC()
	if err != nil {
		axi, jsonOut, minOut := outputMode(cmd, args)
		output.SetDefaultAXI(axi)
		code = output.New(jsonOut, minOut, &out).PrintError(withUsageHelp(cmd, err))
	}
	return code, out.String(), errOut.String()
}

// sectionJSON runs markdown section under --json and decodes the result.
func sectionJSON(t *testing.T, args ...string) markdownSectionResult {
	t.Helper()
	code, stdout, stderr := runMarkdownSectionCLI(t, append([]string{"markdown", "section", "--json"}, args...)...)
	if code != 0 || stderr != "" {
		t.Fatalf("markdown section %v: exit %d, stderr %q, stdout %q", args, code, stderr, stdout)
	}
	var r markdownSectionResult
	if err := json.Unmarshal([]byte(stdout), &r); err != nil {
		t.Fatalf("stdout is not a section document: %v: %q", err, stdout)
	}
	return r
}

// sectionDoc is the fixture most boundary tests read: Target at level 2
// holds a deeper header, and ends at the next level-2 header.
const sectionDoc = `# Top

intro

## Target

body one

### Deep

deep body

## Next

after
`

// Defends against the source's regex boundary (any next # line): a section
// ends at the next header of the same or a higher level, never at a deeper
// one, and the content is byte-for-byte the lines in between, trimmed only
// at its ends.
func TestMarkdownSectionEndsAtSameOrHigherHeader(t *testing.T) {
	path := writeMarkdown(t, "doc.md", sectionDoc)
	r := sectionJSON(t, path, "Target")
	want := markdownSectionResult{
		Title:   "Target",
		Level:   2,
		Line:    5,
		Content: "body one\n\n### Deep\n\ndeep body",
	}
	if r != want {
		t.Fatalf("section Target = %+v, want %+v", r, want)
	}
}

// Defends the higher-level half of the boundary: a level-3 section ends at
// the next level-2 header, and a section with no later boundary runs to the
// end of the file.
func TestMarkdownSectionHigherHeaderEndsAndEOFRuns(t *testing.T) {
	path := writeMarkdown(t, "doc.md", sectionDoc)
	r := sectionJSON(t, path, "Deep")
	if r.Content != "deep body" || r.Level != 3 || r.Line != 9 {
		t.Fatalf("section Deep = %+v, want deep body at level 3 line 9", r)
	}
	r = sectionJSON(t, path, "Next")
	if r.Content != "after" {
		t.Fatalf("section Next = %+v, want after (runs to EOF)", r)
	}
}

// Defends against the source's regex over raw text: a ## line inside a
// fenced code block is code, not the end of the section, and a # comment in
// the leading frontmatter is neither a match nor a boundary.
func TestMarkdownSectionFenceAndFrontmatterAreNotBoundaries(t *testing.T) {
	path := writeMarkdown(t, "doc.md", strings.Join([]string{
		"---",
		"# Target",
		"---",
		"## Target",
		"before",
		"```markdown",
		"## Not a boundary",
		"# neither",
		"```",
		"~~~",
		"## still code",
		"~~~",
		"after",
		"## Real Next",
		"outside",
	}, "\n")+"\n")
	r := sectionJSON(t, path, "Target")
	want := "before\n```markdown\n## Not a boundary\n# neither\n```\n~~~\n## still code\n~~~\nafter"
	if r.Content != want || r.Line != 4 {
		t.Fatalf("section Target = %+v, want content %q starting at line 4", r, want)
	}
}

// Defends the retry contract: a missing title is exit 1 with the file's
// header list in the error fields, so the caller can pick a real title
// without another read. Checked in TOON and --json; --plain names the title
// on stdout, never stderr.
func TestMarkdownSectionMissingTitleListsHeaders(t *testing.T) {
	path := writeMarkdown(t, "doc.md", sectionDoc)

	code, stdout, stderr := runMarkdownSectionCLI(t, "markdown", "section", "--json", path, "Nope")
	if code != 1 || stderr != "" {
		t.Fatalf("--json: exit %d stderr %q, want 1 and nothing on stderr", code, stderr)
	}
	var doc struct {
		Error   bool             `json:"error"`
		Message string           `json:"message"`
		Headers []MarkdownHeader `json:"headers"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("stdout is not an error document: %v: %q", err, stdout)
	}
	if !doc.Error || !strings.Contains(doc.Message, "Nope") {
		t.Errorf("error document must name the missing title Nope: %q", stdout)
	}
	want := []MarkdownHeader{
		{Level: 1, Title: "Top", Line: 1},
		{Level: 2, Title: "Target", Line: 5},
		{Level: 3, Title: "Deep", Line: 9},
		{Level: 2, Title: "Next", Line: 13},
	}
	if !headersEqual(doc.Headers, want) {
		t.Errorf("headers = %+v, want the file's headers %+v", doc.Headers, want)
	}

	code, stdout, _ = runMarkdownSectionCLI(t, "markdown", "section", path, "Nope")
	if code != 1 || !strings.Contains(stdout, "Nope") || !strings.Contains(stdout, "headers[4]{level,line,title}:") {
		t.Errorf("TOON error must carry the header list, got exit %d: %q", code, stdout)
	}

	code, stdout, stderr = runMarkdownSectionCLI(t, "markdown", "section", "--plain", path, "Nope")
	if code != 1 || stderr != "" || !strings.Contains(stdout, "Nope") {
		t.Errorf("--plain: exit %d stderr %q, want the message naming Nope on stdout: %q", code, stderr, stdout)
	}
}

// Defends the empty state on the error path: a file with no headers still
// carries headers [] in the error fields, not a dropped key.
func TestMarkdownSectionMissingTitleNoHeadersStatesNone(t *testing.T) {
	path := writeMarkdown(t, "plain.md", "just text\n")
	code, stdout, _ := runMarkdownSectionCLI(t, "markdown", "section", "--json", path, "Nope")
	var doc map[string]any
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil || code != 1 || !jsonEmpty(doc, "headers") {
		t.Fatalf("exit %d, want an error with headers []: %v: %q", code, err, stdout)
	}
}

// Defends --include-header: the content starts with the header line as the
// file wrote it, and the boundary is unchanged.
func TestMarkdownSectionIncludeHeader(t *testing.T) {
	path := writeMarkdown(t, "doc.md", sectionDoc)
	r := sectionJSON(t, "--include-header", path, "Target")
	want := "## Target\n\nbody one\n\n### Deep\n\ndeep body"
	if r.Content != want {
		t.Fatalf("--include-header content = %q, want %q", r.Content, want)
	}
}

// Defends Decision 3's match rule: exact after trimming, case-insensitive,
// first match wins. A substring or a second occurrence must not match.
func TestMarkdownSectionMatchIsTrimmedCaseInsensitiveFirst(t *testing.T) {
	path := writeMarkdown(t, "doc.md", "## Dup\nfirst\n## Other\nx\n## dup\nsecond\n")
	r := sectionJSON(t, path, "  DUP  ")
	if r.Content != "first" || r.Line != 1 {
		t.Fatalf("section '  DUP  ' = %+v, want the first Dup (content first)", r)
	}
	code, stdout, _ := runMarkdownSectionCLI(t, "markdown", "section", "--json", path, "Du")
	if code != 1 {
		t.Fatalf("a substring matched: exit %d: %q", code, stdout)
	}
}

// Defends the byte-for-byte contract: blank lines and indentation inside
// the section survive; only the ends are trimmed.
func TestMarkdownSectionContentKeepsInteriorBytes(t *testing.T) {
	path := writeMarkdown(t, "doc.md", "## Target\n\n\n    indented code\n\n\ttabbed\n\n## Next\n")
	r := sectionJSON(t, path, "Target")
	if r.Content != "    indented code\n\n\ttabbed" {
		t.Fatalf("content = %q, want interior blank lines and indentation kept", r.Content)
	}
}

// Defends the empty state: a section with no body says content "" with
// exit 0 in every format, rather than dropping the key or failing.
func TestMarkdownSectionEmptyContentStatesEmpty(t *testing.T) {
	path := writeMarkdown(t, "doc.md", "## Target\n## Next\nbody\n")

	code, stdout, _ := runMarkdownSectionCLI(t, "markdown", "section", "--json", path, "Target")
	var doc map[string]any
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil || code != 0 {
		t.Fatalf("--json: exit %d, %v: %q", code, err, stdout)
	}
	if v, ok := doc["content"]; !ok || v != "" {
		t.Errorf("--json must carry content \"\", got %q", stdout)
	}

	code, stdout, _ = runMarkdownSectionCLI(t, "markdown", "section", "--json", "--min", path, "Target")
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil || code != 0 {
		t.Fatalf("--json --min: exit %d, %v: %q", code, err, stdout)
	}
	if v, ok := doc["content"]; !ok || v != "" {
		t.Errorf("--json --min must keep content \"\", got %q", stdout)
	}

	code, stdout, _ = runMarkdownSectionCLI(t, "markdown", "section", path, "Target")
	if code != 0 || !strings.Contains(stdout, "content:") {
		t.Errorf("TOON: exit %d, want a content key: %q", code, stdout)
	}
}

// Defends the output modes: TOON carries title, level, line and content;
// --plain prints the bare content, so a caller can pipe the section body.
func TestMarkdownSectionOutputModes(t *testing.T) {
	path := writeMarkdown(t, "doc.md", "## Target\nbody one\nbody two\n## Next\n")

	code, stdout, _ := runMarkdownSectionCLI(t, "markdown", "section", path, "Target")
	if code != 0 {
		t.Fatalf("TOON: exit %d: %q", code, stdout)
	}
	for _, want := range []string{"title: Target", "level: 2", "line: 1", "content:"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("TOON missing %q:\n%s", want, stdout)
		}
	}

	r := sectionJSON(t, path, "Target")
	if r.Title != "Target" || r.Level != 2 || r.Line != 1 || r.Content != "body one\nbody two" {
		t.Errorf("--json = %+v, want Target level 2 line 1 with both body lines", r)
	}

	code, stdout, stderr := runMarkdownSectionCLI(t, "markdown", "section", "--plain", path, "Target")
	if code != 0 || stderr != "" || stdout != "body one\nbody two\n" {
		t.Errorf("--plain: exit %d stderr %q, want the bare content, got %q", code, stderr, stdout)
	}
}

// Defends the bad-path contract: a missing file and a directory are exit 1
// with an error document naming the path as the caller gave it, never an
// empty section.
func TestMarkdownSectionBadPathNamesThePath(t *testing.T) {
	dir := realTempDir(t)
	missing := filepath.Join(dir, "nope.md")
	for _, tc := range []struct{ name, path, reason string }{
		{"missing", missing, "not found"},
		{"directory", dir, "directory"},
	} {
		for _, mode := range []string{"", "--json", "--plain"} {
			args := []string{"markdown", "section", tc.path, "Target"}
			if mode != "" {
				args = append(args, mode)
			}
			code, stdout, stderr := runMarkdownSectionCLI(t, args...)
			if code != 1 || stderr != "" {
				t.Errorf("%s %s: exit %d stderr %q, want 1 and nothing on stderr", tc.name, mode, code, stderr)
			}
			if !strings.Contains(stdout, tc.path) || !strings.Contains(stdout, tc.reason) {
				t.Errorf("%s %s: want an error naming %s (%s), got %q", tc.name, mode, tc.path, tc.reason, stdout)
			}
		}
	}
}

// Defends the argument count being a usage error (exit 2): section needs
// exactly a file and a title.
func TestMarkdownSectionArgumentCount(t *testing.T) {
	path := writeMarkdown(t, "doc.md", "## Target\nbody\n")
	if code, stdout, _ := runMarkdownSectionCLI(t, "markdown", "section", path); code != 2 {
		t.Errorf("no title: exit %d, want 2: %q", code, stdout)
	}
	if code, stdout, _ := runMarkdownSectionCLI(t, "markdown", "section", path, "Target", "extra"); code != 2 {
		t.Errorf("three args: exit %d, want 2: %q", code, stdout)
	}
}

// Defends Decision 7: section prints no path, so --absolute is refused as a
// usage error rather than accepted and ignored.
func TestMarkdownSectionRejectsAbsolute(t *testing.T) {
	path := writeMarkdown(t, "doc.md", "## Target\nbody\n")
	if code, stdout, _ := runMarkdownSectionCLI(t, "markdown", "section", "--absolute", path, "Target"); code != 2 {
		t.Errorf("--absolute: exit %d, want 2: %q", code, stdout)
	}
}
