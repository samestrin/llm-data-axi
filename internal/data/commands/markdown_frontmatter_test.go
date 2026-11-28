package commands

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/llm-data-axi/v2/pkg/output"
)

// runFrontmatterCLI is runMarkdownCLI with the frontmatter constructor added
// to the markdown parent. newMarkdownFrontmatterCmd is not added to
// newMarkdownCmd until epic 20 T6, so the tests build the tree T6 will build.
func runFrontmatterCLI(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	restoreOutputMode(t)
	var out, errOut bytes.Buffer
	root := NewRootCmd()
	md := newMarkdownCmd()
	md.AddCommand(newMarkdownFrontmatterCmd())
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

// frontmatterJSON runs markdown frontmatter under --json and returns the
// frontmatter mapping from the result document.
func frontmatterJSON(t *testing.T, args ...string) map[string]any {
	t.Helper()
	code, stdout, stderr := runFrontmatterCLI(t, append([]string{"markdown", "frontmatter", "--json"}, args...)...)
	if code != 0 || stderr != "" {
		t.Fatalf("markdown frontmatter %v: exit %d, stderr %q, stdout %q", args, code, stderr, stdout)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("stdout is not a frontmatter document: %v: %q", err, stdout)
	}
	fm, ok := doc["frontmatter"].(map[string]any)
	if !ok {
		t.Fatalf("frontmatter is not a mapping in %q", stdout)
	}
	return fm
}

// Defends against the llm-support flattening: it split lines on ":" into a
// map[string]string, so a list became the string "[a, b]" and a nested
// mapping became wrong strings. Nested keys, flow lists, block lists and
// lists of mappings must survive as the values YAML says they are.
func TestMarkdownFrontmatterNestedKeysAndListsSurvive(t *testing.T) {
	path := writeMarkdown(t, "doc.md", strings.Join([]string{
		"---",
		"title: My Doc",
		"tags: [a, b]",
		"meta:",
		"  nested:",
		"    deep: true",
		"authors:",
		"  - name: A",
		"    role: writer",
		"  - name: B",
		"draft: false",
		"---",
		"# Body",
	}, "\n")+"\n")
	fm := frontmatterJSON(t, path)

	tags, ok := fm["tags"].([]any)
	if !ok || len(tags) != 2 || tags[0] != "a" || tags[1] != "b" {
		t.Fatalf("tags = %#v, want the list [a, b]", fm["tags"])
	}
	meta, ok := fm["meta"].(map[string]any)
	if !ok {
		t.Fatalf("meta = %#v, want a nested mapping", fm["meta"])
	}
	nested, ok := meta["nested"].(map[string]any)
	if !ok || nested["deep"] != true {
		t.Fatalf("meta.nested = %#v, want deep: true", meta["nested"])
	}
	authors, ok := fm["authors"].([]any)
	if !ok || len(authors) != 2 {
		t.Fatalf("authors = %#v, want a list of two mappings", fm["authors"])
	}
	first, ok := authors[0].(map[string]any)
	if !ok || first["name"] != "A" || first["role"] != "writer" {
		t.Fatalf("authors[0] = %#v, want name A, role writer", authors[0])
	}
	if fm["title"] != "My Doc" || fm["draft"] != false {
		t.Fatalf("title/draft = %#v / %#v, want My Doc / false", fm["title"], fm["draft"])
	}
}

// Defends the three output modes carrying the same nested values, so a
// caller in any mode reads the list as a list, not a flattened string.
func TestMarkdownFrontmatterOutputModes(t *testing.T) {
	path := writeMarkdown(t, "doc.md", "---\ntitle: x\ntags: [a, b]\n---\n")

	code, stdout, stderr := runFrontmatterCLI(t, "markdown", "frontmatter", path)
	if code != 0 || stderr != "" {
		t.Fatalf("TOON: exit %d, stderr %q", code, stderr)
	}
	for _, want := range []string{"frontmatter:", "tags[2]: a,b", "title: x"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("TOON missing %q:\n%s", want, stdout)
		}
	}

	fm := frontmatterJSON(t, path)
	if tags, ok := fm["tags"].([]any); !ok || len(tags) != 2 {
		t.Errorf("--json tags = %#v, want a list of two", fm["tags"])
	}

	code, stdout, _ = runFrontmatterCLI(t, "markdown", "frontmatter", "--json", "--min", path)
	if code != 0 || strings.Count(strings.TrimSpace(stdout), "\n") != 0 || !strings.Contains(stdout, `"tags":["a","b"]`) {
		t.Errorf("--json --min: exit %d, want one line carrying the tags list: %q", code, stdout)
	}
}

// Defends --plain printing the raw block: the lines between the --- markers,
// byte for byte, with comments and spacing as written. Re-encoding the
// parsed map would drop comments and reorder keys the caller asked to see.
func TestMarkdownFrontmatterPlainPrintsRawBlock(t *testing.T) {
	block := "# a comment\ntitle:   \"My Doc\"\ntags: [a, b]\n"
	path := writeMarkdown(t, "doc.md", "---\n"+block+"---\n# Body\n")
	code, stdout, stderr := runFrontmatterCLI(t, "markdown", "frontmatter", "--plain", path)
	if code != 0 || stderr != "" {
		t.Fatalf("--plain: exit %d, stderr %q", code, stderr)
	}
	if stdout != block {
		t.Fatalf("--plain = %q, want the raw block %q", stdout, block)
	}
}

// Defends Decision 4's three distinct exit-1 errors: no block, an
// unterminated block and YAML that does not parse each name their own cause
// and the path, in every output mode. One shared "bad frontmatter" message
// would leave the caller unable to tell a file with none from a broken one.
func TestMarkdownFrontmatterThreeErrorsAreDistinct(t *testing.T) {
	cases := []struct{ name, content, phrase string }{
		{"no block", "# Title\ntext\n", "no frontmatter block"},
		{"empty file", "", "no frontmatter block"},
		{"later block", "intro\n---\ntitle: x\n---\n", "no frontmatter block"},
		{"unterminated", "---\ntitle: x\n", "unterminated frontmatter block"},
		{"invalid YAML", "---\nkey: [unclosed\n---\n", "invalid frontmatter YAML"},
	}
	phrases := []string{"no frontmatter block", "unterminated frontmatter block", "invalid frontmatter YAML"}
	for _, tc := range cases {
		path := writeMarkdown(t, "err.md", tc.content)
		for _, mode := range []string{"", "--json", "--plain"} {
			args := []string{"markdown", "frontmatter"}
			if mode != "" {
				args = append(args, mode)
			}
			args = append(args, path)
			code, stdout, stderr := runFrontmatterCLI(t, args...)
			if code != 1 || stderr != "" {
				t.Errorf("%s %s: exit %d stderr %q, want 1 and nothing on stderr", tc.name, mode, code, stderr)
			}
			if !strings.Contains(stdout, tc.phrase) || !strings.Contains(stdout, path) {
				t.Errorf("%s %s: want an error naming %q and %s, got %q", tc.name, mode, tc.phrase, path, stdout)
			}
			for _, other := range phrases {
				if other != tc.phrase && strings.Contains(stdout, other) {
					t.Errorf("%s %s: error also claims %q: %q", tc.name, mode, other, stdout)
				}
			}
		}
	}
}

// Defends against a frontmatter block whose root is not a mapping being
// printed as if it were key-value metadata: a list or a bare scalar is not
// frontmatter, and encoding it anyway would publish a document the caller
// cannot read as keys.
func TestMarkdownFrontmatterNonMappingRootIsInvalid(t *testing.T) {
	for _, content := range []string{"---\n- a\n- b\n---\n", "---\njust a string\n---\n"} {
		path := writeMarkdown(t, "doc.md", content)
		code, stdout, _ := runFrontmatterCLI(t, "markdown", "frontmatter", "--json", path)
		if code != 1 || !strings.Contains(stdout, "invalid frontmatter YAML") || !strings.Contains(stdout, "mapping") {
			t.Errorf("%q: exit %d, want 1 naming invalid frontmatter YAML and a mapping: %q", content, code, stdout)
		}
	}
}

// Defends the empty state: a terminated block with no keys, only comments,
// or a null document is frontmatter that exists and is empty, so --json says
// frontmatter: {} and TOON carries the frontmatter key. A null or an empty
// document with exit 0 would read as "nothing happened".
func TestMarkdownFrontmatterEmptyBlockStatesEmpty(t *testing.T) {
	for _, content := range []string{"---\n---\n", "---\n# only a comment\n---\n", "---\nnull\n---\n"} {
		path := writeMarkdown(t, "doc.md", content)

		code, stdout, _ := runFrontmatterCLI(t, "markdown", "frontmatter", "--json", path)
		var doc map[string]any
		if err := json.Unmarshal([]byte(stdout), &doc); err != nil || code != 0 {
			t.Fatalf("%q --json: exit %d, %v: %q", content, code, err, stdout)
		}
		fm, ok := doc["frontmatter"].(map[string]any)
		if !ok || len(fm) != 0 {
			t.Errorf("%q --json: frontmatter = %#v, want an empty mapping", content, doc["frontmatter"])
		}

		code, stdout, _ = runFrontmatterCLI(t, "markdown", "frontmatter", path)
		if code != 0 || !hasLine(stdout, "frontmatter:") {
			t.Errorf("%q TOON: exit %d, want the frontmatter key stated: %q", content, code, stdout)
		}
	}
}

// Defends the bad-path contract shared with headers: a missing file and a
// directory are exit 1 with an error document naming the path as given,
// never an empty frontmatter.
func TestMarkdownFrontmatterBadPathNamesThePath(t *testing.T) {
	dir := realTempDir(t)
	missing := filepath.Join(dir, "nope.md")
	for _, tc := range []struct{ name, path, reason string }{
		{"missing", missing, "not found"},
		{"directory", dir, "directory"},
	} {
		for _, mode := range []string{"", "--json", "--plain"} {
			args := []string{"markdown", "frontmatter", tc.path}
			if mode != "" {
				args = append(args, mode)
			}
			code, stdout, stderr := runFrontmatterCLI(t, args...)
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
func TestMarkdownFrontmatterMissingRelativePathIsReportedAsGiven(t *testing.T) {
	t.Chdir(realTempDir(t))
	code, stdout, _ := runFrontmatterCLI(t, "markdown", "frontmatter", "--json", "docs/nope.md")
	if code != 1 || !strings.Contains(stdout, `"file not found: docs/nope.md"`) {
		t.Fatalf("exit %d, want 1 with file not found: docs/nope.md: %q", code, stdout)
	}
}

// Defends Decision 7: frontmatter prints no path, so --absolute is refused
// as a usage error rather than accepted and ignored.
func TestMarkdownFrontmatterRejectsAbsolute(t *testing.T) {
	path := writeMarkdown(t, "doc.md", "---\ntitle: x\n---\n")
	if code, stdout, _ := runFrontmatterCLI(t, "markdown", "frontmatter", "--absolute", path); code != 2 {
		t.Errorf("--absolute: exit %d, want 2: %q", code, stdout)
	}
}

// Defends the argument count being a usage error (exit 2), not a runtime
// failure or a help page with exit 0.
func TestMarkdownFrontmatterArgumentErrors(t *testing.T) {
	if code, stdout, _ := runFrontmatterCLI(t, "markdown", "frontmatter"); code != 2 {
		t.Errorf("no file: exit %d, want 2: %q", code, stdout)
	}
	if code, stdout, _ := runFrontmatterCLI(t, "markdown", "frontmatter", "a.md", "b.md"); code != 2 {
		t.Errorf("two files: exit %d, want 2: %q", code, stdout)
	}
}

// Defends reading files from Windows editors: a byte order mark before the
// opening --- and CRLF line endings must not turn the block into "no
// frontmatter", and --plain must not carry the \r bytes into its output.
func TestMarkdownFrontmatterBOMAndCRLF(t *testing.T) {
	path := writeMarkdown(t, "doc.md", "\ufeff---\r\ntitle: x\r\ntags: [a, b]\r\n---\r\n# Body\r\n")
	fm := frontmatterJSON(t, path)
	if fm["title"] != "x" {
		t.Fatalf("title = %#v, want x", fm["title"])
	}
	code, stdout, _ := runFrontmatterCLI(t, "markdown", "frontmatter", "--plain", path)
	if code != 0 || stdout != "title: x\ntags: [a, b]\n" {
		t.Fatalf("--plain: exit %d, %q, want the block without \\r", code, stdout)
	}
}

// Defends the delimiter rule the headers scanner already uses: trailing
// spaces or tabs on a --- line do not stop it being a delimiter.
func TestMarkdownFrontmatterDelimitersTolerateTrailingSpaces(t *testing.T) {
	path := writeMarkdown(t, "doc.md", "--- \t\ntitle: x\n---  \n# Body\n")
	fm := frontmatterJSON(t, path)
	if fm["title"] != "x" {
		t.Fatalf("title = %#v, want x", fm["title"])
	}
}

// Defends against a duplicate key silently taking the last value: YAML
// forbids duplicate mapping keys, and goccy/go-yaml refuses them, so the
// caller learns the file is broken instead of reading half its keys.
func TestMarkdownFrontmatterDuplicateKeysAreInvalid(t *testing.T) {
	path := writeMarkdown(t, "doc.md", "---\na: 1\na: 2\n---\n")
	code, stdout, _ := runFrontmatterCLI(t, "markdown", "frontmatter", "--json", path)
	if code != 1 || !strings.Contains(stdout, "invalid frontmatter YAML") {
		t.Fatalf("exit %d, want 1 naming invalid frontmatter YAML: %q", code, stdout)
	}
}

// Defends against a silent partial read: yaml.Unmarshal on a block with a
// ... document-end marker returns only the first document and exit 0, so
// every key after the marker vanished without an error. More than one
// document is not frontmatter; a trailing ... with nothing after it is one
// document and stays valid.
func TestMarkdownFrontmatterMultipleDocumentsAreInvalid(t *testing.T) {
	path := writeMarkdown(t, "doc.md", "---\na: 1\n...\nb: 2\n---\n")
	code, stdout, _ := runFrontmatterCLI(t, "markdown", "frontmatter", "--json", path)
	if code != 1 || !strings.Contains(stdout, "invalid frontmatter YAML") || !strings.Contains(stdout, "document") {
		t.Fatalf("exit %d, want 1 naming invalid frontmatter YAML and documents: %q", code, stdout)
	}

	path = writeMarkdown(t, "ok.md", "---\na: 1\n...\n---\n")
	if fm := frontmatterJSON(t, path); fm["a"] != float64(1) {
		t.Fatalf("a = %#v, want 1", fm["a"])
	}

	// A second document that does not even parse is the same failure, not
	// a reason to print the first one.
	path = writeMarkdown(t, "bad2.md", "---\na: 1\n...\nkey: [unclosed\n---\n")
	code, stdout, _ = runFrontmatterCLI(t, "markdown", "frontmatter", "--json", path)
	if code != 1 || !strings.Contains(stdout, "invalid frontmatter YAML") {
		t.Fatalf("broken second document: exit %d, want 1 naming invalid frontmatter YAML: %q", code, stdout)
	}
}

// Defends against a YAML value JSON cannot encode (.inf) exiting 0 with an
// empty or partial document: the encode failure must be an error naming the
// file whose frontmatter could not be printed.
func TestMarkdownFrontmatterUnencodableValueIsAnError(t *testing.T) {
	path := writeMarkdown(t, "doc.md", "---\nx: .inf\n---\n")
	for _, mode := range []string{"", "--json"} {
		args := []string{"markdown", "frontmatter"}
		if mode != "" {
			args = append(args, mode)
		}
		args = append(args, path)
		code, stdout, _ := runFrontmatterCLI(t, args...)
		if code != 1 || !strings.Contains(stdout, path) {
			t.Errorf("%s: exit %d, want 1 naming %s: %q", mode, code, path, stdout)
		}
	}
}
