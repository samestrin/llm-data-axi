package commands

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/llm-data-axi/v2/pkg/output"
)

// runMarkdownCLI is runCLI with the markdown command added to the root.
// newMarkdownCmd is not registered in NewRootCmd until epic 20 T6, so the
// tests build the tree run builds and add it, then report a failure the way
// run does.
func runMarkdownCLI(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	restoreOutputMode(t)
	var out, errOut bytes.Buffer
	root := NewRootCmd()
	md := newMarkdownCmd()
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

// writeMarkdown writes content to name in a fresh directory and returns its path.
func writeMarkdown(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(realTempDir(t), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// headersJSON runs markdown headers under --json and decodes the result.
func headersJSON(t *testing.T, args ...string) MarkdownHeadersResult {
	t.Helper()
	code, stdout, stderr := runMarkdownCLI(t, append([]string{"markdown", "headers", "--json"}, args...)...)
	if code != 0 || stderr != "" {
		t.Fatalf("markdown headers %v: exit %d, stderr %q, stdout %q", args, code, stderr, stdout)
	}
	var r MarkdownHeadersResult
	if err := json.Unmarshal([]byte(stdout), &r); err != nil {
		t.Fatalf("stdout is not a headers document: %v: %q", err, stdout)
	}
	return r
}

// Defends against the llm-support regex over raw text: a shell comment in a
// fenced code block was listed as a header. Both fence characters are
// covered, and a ~~~ inside a ``` fence must not close it.
func TestMarkdownHeadersSkipsFencedCode(t *testing.T) {
	path := writeMarkdown(t, "doc.md", strings.Join([]string{
		"# Real",
		"```bash",
		"# not a header",
		"~~~",
		"## still code",
		"```",
		"## Second",
		"~~~~",
		"# tilde code",
		"```",
		"~~~~",
		"### Third",
	}, "\n")+"\n")
	r := headersJSON(t, path)
	want := []MarkdownHeader{
		{Level: 1, Title: "Real", Line: 1},
		{Level: 2, Title: "Second", Line: 7},
		{Level: 3, Title: "Third", Line: 12},
	}
	if !headersEqual(r.Headers, want) || r.Total != 3 {
		t.Fatalf("headers = %+v total %d, want %+v", r.Headers, r.Total, want)
	}
}

// Defends against an unclosed fence leaking its body: CommonMark runs an
// unclosed fence to the end of the document, so nothing after it is a header.
func TestMarkdownHeadersUnclosedFenceRunsToTheEnd(t *testing.T) {
	path := writeMarkdown(t, "doc.md", "# Top\n```\n# inside\n")
	r := headersJSON(t, path)
	if !headersEqual(r.Headers, []MarkdownHeader{{Level: 1, Title: "Top", Line: 1}}) {
		t.Fatalf("headers = %+v, want only Top", r.Headers)
	}
}

// Defends against a line of inline code opening a fence: ```x``` has a
// backtick in its info string, so it is not a fence (CommonMark), and
// treating it as one hid every header after it.
func TestMarkdownHeadersInlineBackticksAreNotAFence(t *testing.T) {
	path := writeMarkdown(t, "doc.md", "```inline```\n# After\n")
	r := headersJSON(t, path)
	if !headersEqual(r.Headers, []MarkdownHeader{{Level: 1, Title: "After", Line: 2}}) {
		t.Fatalf("headers = %+v, want After on line 2", r.Headers)
	}
}

// Defends the empty state on a zero-byte file: headers [] and total 0, not
// a null list.
func TestMarkdownHeadersZeroByteFile(t *testing.T) {
	code, stdout, _ := runMarkdownCLI(t, "markdown", "headers", "--json", writeMarkdown(t, "empty.md", ""))
	var doc map[string]any
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil || code != 0 || !jsonEmpty(doc, "headers") {
		t.Fatalf("exit %d, want headers [] : %q", code, stdout)
	}
}

// Defends against reading YAML comments as headers: a plan file's leading
// frontmatter block holds `# comment` lines, which are YAML, not headers.
func TestMarkdownHeadersSkipsLeadingFrontmatter(t *testing.T) {
	path := writeMarkdown(t, "plan.md", "---\n# a yaml comment\ntitle: x\n---\n# Plan\n")
	r := headersJSON(t, path)
	if !headersEqual(r.Headers, []MarkdownHeader{{Level: 1, Title: "Plan", Line: 5}}) {
		t.Fatalf("headers = %+v, want only Plan on line 5", r.Headers)
	}
}

// Defends against hiding every header behind a `---` that never closes: an
// unterminated block is not frontmatter (it is a thematic break), so the
// headers after it are still listed rather than reported as none.
func TestMarkdownHeadersUnterminatedFrontmatterIsNotSkipped(t *testing.T) {
	path := writeMarkdown(t, "doc.md", "---\n# One\n## Two\n")
	r := headersJSON(t, path)
	if r.Total != 2 {
		t.Fatalf("headers = %+v, want One and Two", r.Headers)
	}
}

// Defends against treating a later `---` block as frontmatter: only a block
// opening on line 1 is skipped.
func TestMarkdownHeadersFrontmatterOnlyOnLineOne(t *testing.T) {
	path := writeMarkdown(t, "doc.md", "intro\n---\n# Kept\n---\n")
	r := headersJSON(t, path)
	if !headersEqual(r.Headers, []MarkdownHeader{{Level: 1, Title: "Kept", Line: 3}}) {
		t.Fatalf("headers = %+v, want Kept on line 3", r.Headers)
	}
}

// Defends the CommonMark ATX rules a loose regex gets wrong: #hashtag and
// seven hashes are not headers, four spaces of indent is code, a closing
// sequence and CRLF line endings are not part of the title.
func TestMarkdownHeadersFollowsATXRules(t *testing.T) {
	path := writeMarkdown(t, "doc.md", strings.Join([]string{
		"#hashtag",
		"####### seven",
		"    # indented code",
		"   ## Three spaces ##",
		"# Title with # inside #",
		"## Ends with C#",
		"\t# tab is code",
		"###",
	}, "\r\n")+"\r\n")
	r := headersJSON(t, path)
	want := []MarkdownHeader{
		{Level: 2, Title: "Three spaces", Line: 4},
		{Level: 1, Title: "Title with # inside", Line: 5},
		{Level: 2, Title: "Ends with C#", Line: 6},
		{Level: 3, Title: "", Line: 8},
	}
	if !headersEqual(r.Headers, want) {
		t.Fatalf("headers = %+v, want %+v", r.Headers, want)
	}
}

// Defends the empty state: a file with no headers says so with an empty list
// and a total of 0 in every format, exit 0, rather than dropping the key.
func TestMarkdownHeadersEmptyFileStatesNone(t *testing.T) {
	path := writeMarkdown(t, "plain.md", "just text\n```\n# code\n```\n")

	code, stdout, stderr := runMarkdownCLI(t, "markdown", "headers", path)
	if code != 0 || stderr != "" {
		t.Fatalf("TOON: exit %d, stderr %q", code, stderr)
	}
	if !toonEmpty(stdout, "headers") || !toonEmpty(stdout, "total") {
		t.Errorf("TOON must carry headers[0] and total: 0, got %q", stdout)
	}

	code, stdout, _ = runMarkdownCLI(t, "markdown", "headers", "--json", path)
	var doc map[string]any
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil || code != 0 {
		t.Fatalf("--json: exit %d, %v: %q", code, err, stdout)
	}
	if !jsonEmpty(doc, "headers") || !jsonEmpty(doc, "total") {
		t.Errorf("--json must carry headers [] and total 0, got %q", stdout)
	}

	code, stdout, _ = runMarkdownCLI(t, "markdown", "headers", "--plain", path)
	if code != 0 || !hasLine(stdout, "TOTAL: 0") {
		t.Errorf("--plain: exit %d, want the line TOTAL: 0, got %q", code, stdout)
	}
}

// Defends the three output modes listing level and line for each header, so
// a caller in any mode can jump to a header without another read.
func TestMarkdownHeadersOutputModesCarryLevelAndLine(t *testing.T) {
	path := writeMarkdown(t, "doc.md", "# Top\n\ntext\n## Sub\n")

	code, stdout, _ := runMarkdownCLI(t, "markdown", "headers", path)
	if code != 0 {
		t.Fatalf("TOON: exit %d: %q", code, stdout)
	}
	for _, want := range []string{"headers[2]{level,line,title}:", "1,1,Top", "2,4,Sub", "total: 2"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("TOON missing %q:\n%s", want, stdout)
		}
	}

	r := headersJSON(t, path)
	want := []MarkdownHeader{{Level: 1, Title: "Top", Line: 1}, {Level: 2, Title: "Sub", Line: 4}}
	if !headersEqual(r.Headers, want) || r.Total != 2 {
		t.Errorf("--json = %+v, want %+v", r, want)
	}

	code, stdout, _ = runMarkdownCLI(t, "markdown", "headers", "--plain", path)
	if code != 0 {
		t.Fatalf("--plain: exit %d: %q", code, stdout)
	}
	for _, want := range []string{"1: h1 Top", "4: h2 Sub", "TOTAL: 2"} {
		if !hasLine(stdout, want) {
			t.Errorf("--plain missing the line %q:\n%s", want, stdout)
		}
	}
}

// Defends --level narrowing the list, and the total counting what is listed.
func TestMarkdownHeadersLevelFilter(t *testing.T) {
	path := writeMarkdown(t, "doc.md", "# A\n## B\n### C\n## D\n")
	r := headersJSON(t, "--level", "1,3", path)
	want := []MarkdownHeader{{Level: 1, Title: "A", Line: 1}, {Level: 3, Title: "C", Line: 3}}
	if !headersEqual(r.Headers, want) || r.Total != 2 {
		t.Fatalf("--level 1,3 = %+v total %d, want %+v", r.Headers, r.Total, want)
	}
}

// Defends against a --level value that cannot match being run anyway: it
// printed an empty list with exit 0, as if the file had no such headers.
func TestMarkdownHeadersBadLevelIsUsageError(t *testing.T) {
	path := writeMarkdown(t, "doc.md", "# A\n")
	for _, bad := range []string{"0", "7", "x", "1,,2", ""} {
		code, stdout, _ := runMarkdownCLI(t, "markdown", "headers", "--json", "--level", bad, path)
		if code != 2 || !strings.Contains(stdout, "--level") {
			t.Errorf("--level %q: exit %d, want 2 naming --level: %q", bad, code, stdout)
		}
	}
}

// Defends the bad-path contract: a missing file and a directory are exit 1
// with an error document naming the path as the caller gave it, never an
// empty header list.
func TestMarkdownHeadersBadPathNamesThePath(t *testing.T) {
	dir := realTempDir(t)
	missing := filepath.Join(dir, "nope.md")
	for _, tc := range []struct{ name, path, reason string }{
		{"missing", missing, "not found"},
		{"directory", dir, "directory"},
	} {
		for _, mode := range []string{"", "--json", "--plain"} {
			args := []string{"markdown", "headers", tc.path}
			if mode != "" {
				args = append(args, mode)
			}
			code, stdout, stderr := runMarkdownCLI(t, args...)
			if code != 1 || stderr != "" {
				t.Errorf("%s %s: exit %d stderr %q, want 1 and nothing on stderr", tc.name, mode, code, stderr)
			}
			if !strings.Contains(stdout, tc.path) || !strings.Contains(stdout, tc.reason) {
				t.Errorf("%s %s: want an error naming %s (%s), got %q", tc.name, mode, tc.path, tc.reason, stdout)
			}
		}
	}
}

// Defends naming the path as given: a relative path is reported relative,
// not resolved to an absolute one the caller never typed.
func TestMarkdownHeadersMissingRelativePathIsReportedAsGiven(t *testing.T) {
	t.Chdir(realTempDir(t))
	code, stdout, _ := runMarkdownCLI(t, "markdown", "headers", "--json", "docs/nope.md")
	if code != 1 || !strings.Contains(stdout, `"file not found: docs/nope.md"`) {
		t.Fatalf("exit %d, want 1 with file not found: docs/nope.md: %q", code, stdout)
	}
}

// Defends the argument count being a usage error (exit 2), and the bare
// parent printing help instead of accepting a misspelt subcommand.
func TestMarkdownArgumentErrors(t *testing.T) {
	if code, stdout, _ := runMarkdownCLI(t, "markdown", "headers"); code != 2 {
		t.Errorf("no file: exit %d, want 2: %q", code, stdout)
	}
	if code, stdout, _ := runMarkdownCLI(t, "markdown", "headers", "a.md", "b.md"); code != 2 {
		t.Errorf("two files: exit %d, want 2: %q", code, stdout)
	}
	if code, stdout, _ := runMarkdownCLI(t, "markdown", "header"); code == 0 {
		t.Errorf("misspelt subcommand: exit 0: %q", stdout)
	}
	code, stdout, _ := runMarkdownCLI(t, "markdown")
	if code != 0 || !strings.Contains(stdout, "headers") {
		t.Errorf("bare markdown: exit %d, want help naming headers: %q", code, stdout)
	}
}

// Defends Decision 7: the headers print no path, so --absolute is refused as
// a usage error rather than accepted and ignored.
func TestMarkdownHeadersRejectsAbsolute(t *testing.T) {
	path := writeMarkdown(t, "doc.md", "# A\n")
	if code, stdout, _ := runMarkdownCLI(t, "markdown", "headers", "--absolute", path); code != 2 {
		t.Errorf("--absolute: exit %d, want 2: %q", code, stdout)
	}
}

func headersEqual(got, want []MarkdownHeader) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
