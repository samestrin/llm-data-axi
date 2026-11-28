package commands

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/llm-data-axi/v2/pkg/output"
)

// runMarkdownTasksCLI is runMarkdownCLI with the tasks subcommand added to
// the markdown parent. newMarkdownTasksCmd is not added to newMarkdownCmd
// until epic 20 T6, so the tests build the tree T6 will build.
func runMarkdownTasksCLI(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	restoreOutputMode(t)
	var out, errOut bytes.Buffer
	root := NewRootCmd()
	md := newMarkdownCmd()
	md.AddCommand(newMarkdownTasksCmd())
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

// tasksJSON runs markdown tasks under --json and decodes the result.
func tasksJSON(t *testing.T, args ...string) MarkdownTasksResult {
	t.Helper()
	code, stdout, stderr := runMarkdownTasksCLI(t, append([]string{"markdown", "tasks", "--json"}, args...)...)
	if code != 0 || stderr != "" {
		t.Fatalf("markdown tasks %v: exit %d, stderr %q, stdout %q", args, code, stderr, stdout)
	}
	var r MarkdownTasksResult
	if err := json.Unmarshal([]byte(stdout), &r); err != nil {
		t.Fatalf("stdout is not a tasks document: %v: %q", err, stdout)
	}
	return r
}

func tasksEqual(got, want []MarkdownTask) bool {
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

// Defends against the llm-support regex over raw text: a `- [ ]` line in a
// fenced code block was listed as a task. Both fence characters are covered.
func TestMarkdownTasksSkipsFencedCode(t *testing.T) {
	path := writeMarkdown(t, "doc.md", strings.Join([]string{
		"- [ ] real one",
		"```md",
		"- [ ] not a task",
		"* [x] not a task either",
		"```",
		"- [x] real two",
		"~~~",
		"+ [ ] tilde code",
		"~~~",
		"- [ ] real three",
	}, "\n")+"\n")
	r := tasksJSON(t, path)
	want := []MarkdownTask{
		{Line: 1, Done: false, Text: "real one"},
		{Line: 6, Done: true, Text: "real two"},
		{Line: 10, Done: false, Text: "real three"},
	}
	if !tasksEqual(r.Tasks, want) || r.Total != 3 || r.Completed != 1 || r.Incomplete != 2 {
		t.Fatalf("tasks = %+v total %d completed %d incomplete %d, want %+v",
			r.Tasks, r.Total, r.Completed, r.Incomplete, want)
	}
}

// Defends against reading YAML list items in the leading frontmatter block
// as tasks: the shared scanner marks them as not prose.
func TestMarkdownTasksSkipsLeadingFrontmatter(t *testing.T) {
	path := writeMarkdown(t, "plan.md", "---\nchecklist:\n- [ ] a yaml list item\n---\n- [x] the task\n")
	r := tasksJSON(t, path)
	want := []MarkdownTask{{Line: 5, Done: true, Text: "the task"}}
	if !tasksEqual(r.Tasks, want) || r.Total != 1 {
		t.Fatalf("tasks = %+v, want only the task on line 5", r.Tasks)
	}
}

// Defends Decision 5: `*` and `+` markers and indented items count, [x] and
// [X] are both done, and the text is the item without its marker, trimmed.
func TestMarkdownTasksMarkersAndIndent(t *testing.T) {
	path := writeMarkdown(t, "doc.md", strings.Join([]string{
		"- [ ] dash",
		"* [ ] star",
		"+ [x] plus",
		"  - [X] two spaces",
		"\t* [x] tab indent",
		"        + [ ] deep indent",
		"- [ ]   padded text   ",
	}, "\n")+"\n")
	r := tasksJSON(t, path)
	want := []MarkdownTask{
		{Line: 1, Done: false, Text: "dash"},
		{Line: 2, Done: false, Text: "star"},
		{Line: 3, Done: true, Text: "plus"},
		{Line: 4, Done: true, Text: "two spaces"},
		{Line: 5, Done: true, Text: "tab indent"},
		{Line: 6, Done: false, Text: "deep indent"},
		{Line: 7, Done: false, Text: "padded text"},
	}
	if !tasksEqual(r.Tasks, want) || r.Total != 7 || r.Completed != 3 || r.Incomplete != 4 {
		t.Fatalf("tasks = %+v total %d completed %d incomplete %d, want %+v",
			r.Tasks, r.Total, r.Completed, r.Incomplete, want)
	}
}

// Defends Decision 5's exclusions: [/] partial is not a task state here
// (count keeps it), an unknown state letter is not a task, and a marker or
// bracket without its separating whitespace is not a task list item.
func TestMarkdownTasksNotTaskItems(t *testing.T) {
	path := writeMarkdown(t, "doc.md", strings.Join([]string{
		"- [/] partial",
		"- [y] unknown state",
		"-[ ] no space after marker",
		"- [ ]text right after bracket",
		"1. [ ] ordered marker",
		"prose - [ ] inline is not a list item",
		"- [ ] the only task",
	}, "\n")+"\n")
	r := tasksJSON(t, path)
	want := []MarkdownTask{{Line: 7, Done: false, Text: "the only task"}}
	if !tasksEqual(r.Tasks, want) || r.Total != 1 {
		t.Fatalf("tasks = %+v, want only line 7", r.Tasks)
	}
}

// Defends the empty state on a zero-byte file: tasks [] and total 0, exit 0,
// not a null or dropped list.
func TestMarkdownTasksZeroByteFile(t *testing.T) {
	code, stdout, _ := runMarkdownTasksCLI(t, "markdown", "tasks", "--json", writeMarkdown(t, "empty.md", ""))
	var doc map[string]any
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil || code != 0 || !jsonEmpty(doc, "tasks") || !jsonEmpty(doc, "total") {
		t.Fatalf("exit %d, want tasks [] and total 0: %v: %q", code, err, stdout)
	}
}

// Defends the empty state in every format: a file with no tasks says so with
// an empty list and zero counts, exit 0, rather than dropping the key.
func TestMarkdownTasksEmptyFileStatesNone(t *testing.T) {
	path := writeMarkdown(t, "plain.md", "just text\n```\n- [ ] code\n```\n")

	code, stdout, stderr := runMarkdownTasksCLI(t, "markdown", "tasks", path)
	if code != 0 || stderr != "" {
		t.Fatalf("TOON: exit %d, stderr %q", code, stderr)
	}
	for _, key := range []string{"tasks", "total", "completed", "incomplete", "percent"} {
		if !toonEmpty(stdout, key) {
			t.Errorf("TOON must carry %s empty or 0, got %q", key, stdout)
		}
	}

	code, stdout, _ = runMarkdownTasksCLI(t, "markdown", "tasks", "--json", path)
	var doc map[string]any
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil || code != 0 {
		t.Fatalf("--json: exit %d, %v: %q", code, err, stdout)
	}
	if !jsonEmpty(doc, "tasks") || !jsonEmpty(doc, "total") {
		t.Errorf("--json must carry tasks [] and total 0, got %q", stdout)
	}

	code, stdout, _ = runMarkdownTasksCLI(t, "markdown", "tasks", "--plain", path)
	if code != 0 || !hasLine(stdout, "TOTAL: 0") {
		t.Errorf("--plain: exit %d, want the line TOTAL: 0, got %q", code, stdout)
	}
}

// Defends --summary: the list is dropped and the counts stay, in every
// format. A summary that kept the list would save no tokens.
func TestMarkdownTasksSummaryDropsTheList(t *testing.T) {
	path := writeMarkdown(t, "doc.md", "- [x] a\n- [ ] b\n- [ ] c\n- [ ] d\n")

	code, stdout, _ := runMarkdownTasksCLI(t, "markdown", "tasks", "--json", "--summary", path)
	var doc map[string]any
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil || code != 0 {
		t.Fatalf("--json --summary: exit %d, %v: %q", code, err, stdout)
	}
	if _, ok := doc["tasks"]; ok {
		t.Errorf("--summary must drop the tasks key, got %q", stdout)
	}
	for key, want := range map[string]float64{"total": 4, "completed": 1, "incomplete": 3, "percent": 25} {
		if v, ok := doc[key].(float64); !ok || v != want {
			t.Errorf("--summary %s = %v, want %v: %q", key, doc[key], want, stdout)
		}
	}

	code, stdout, _ = runMarkdownTasksCLI(t, "markdown", "tasks", "--summary", path)
	if code != 0 || strings.Contains(stdout, "tasks") {
		t.Errorf("TOON --summary: exit %d, want no tasks key: %q", code, stdout)
	}
	for _, want := range []string{"total: 4", "completed: 1", "incomplete: 3", "percent: 25"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("TOON --summary missing %q:\n%s", want, stdout)
		}
	}

	code, stdout, _ = runMarkdownTasksCLI(t, "markdown", "tasks", "--plain", "--summary", path)
	if code != 0 || strings.Contains(stdout, "[ ]") || strings.Contains(stdout, "[x]") {
		t.Errorf("--plain --summary: exit %d, want no item lines: %q", code, stdout)
	}
	for _, want := range []string{"TOTAL: 4", "COMPLETED: 1", "INCOMPLETE: 3", "PERCENT: 25%"} {
		if !hasLine(stdout, want) {
			t.Errorf("--plain --summary missing the line %q:\n%s", want, stdout)
		}
	}
}

// Defends the percent arithmetic: 1 of 4 done is 25, not 0.25 and not the
// incomplete share; an empty file is 0, not a division by zero.
func TestMarkdownTasksPercent(t *testing.T) {
	r := tasksJSON(t, writeMarkdown(t, "doc.md", "- [x] a\n- [ ] b\n- [ ] c\n- [ ] d\n"))
	if r.Percent != 25 {
		t.Fatalf("percent = %v, want 25", r.Percent)
	}
	r = tasksJSON(t, writeMarkdown(t, "none.md", "text\n"))
	if r.Percent != 0 {
		t.Fatalf("percent on no tasks = %v, want 0", r.Percent)
	}
}

// Defends the three output modes listing line, state and text for each task,
// plus the counts, so a caller in any mode can jump to an item.
func TestMarkdownTasksOutputModes(t *testing.T) {
	path := writeMarkdown(t, "doc.md", "- [x] ship it\n\ntext\n- [ ] write tests\n")

	code, stdout, _ := runMarkdownTasksCLI(t, "markdown", "tasks", path)
	if code != 0 {
		t.Fatalf("TOON: exit %d: %q", code, stdout)
	}
	for _, want := range []string{"tasks[2]{line,done,text}:", "1,true,ship it", "4,false,write tests", "total: 2", "completed: 1", "incomplete: 1", "percent: 50"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("TOON missing %q:\n%s", want, stdout)
		}
	}

	r := tasksJSON(t, path)
	want := []MarkdownTask{{Line: 1, Done: true, Text: "ship it"}, {Line: 4, Done: false, Text: "write tests"}}
	if !tasksEqual(r.Tasks, want) || r.Total != 2 || r.Completed != 1 || r.Incomplete != 1 || r.Percent != 50 {
		t.Errorf("--json = %+v, want %+v with counts 2/1/1 and percent 50", r, want)
	}

	code, stdout, _ = runMarkdownTasksCLI(t, "markdown", "tasks", "--plain", path)
	if code != 0 {
		t.Fatalf("--plain: exit %d: %q", code, stdout)
	}
	for _, wantLine := range []string{"1: [x] ship it", "4: [ ] write tests", "TOTAL: 2", "COMPLETED: 1", "INCOMPLETE: 1", "PERCENT: 50%"} {
		if !hasLine(stdout, wantLine) {
			t.Errorf("--plain missing the line %q:\n%s", wantLine, stdout)
		}
	}

	code, stdout, _ = runMarkdownTasksCLI(t, "markdown", "tasks", "--json", "--min", path)
	if code != 0 || !strings.Contains(stdout, `"total":2`) || strings.Contains(stdout, "\n  ") {
		t.Errorf("--json --min: exit %d, want compact JSON with the counts: %q", code, stdout)
	}
}

// Defends the bad-path contract: a missing file and a directory are exit 1
// with an error document naming the path as the caller gave it, never an
// empty task list.
func TestMarkdownTasksBadPathNamesThePath(t *testing.T) {
	dir := realTempDir(t)
	missing := filepath.Join(dir, "nope.md")
	for _, tc := range []struct{ name, path, reason string }{
		{"missing", missing, "not found"},
		{"directory", dir, "directory"},
	} {
		for _, mode := range []string{"", "--json", "--plain"} {
			args := []string{"markdown", "tasks", tc.path}
			if mode != "" {
				args = append(args, mode)
			}
			code, stdout, stderr := runMarkdownTasksCLI(t, args...)
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
func TestMarkdownTasksMissingRelativePathIsReportedAsGiven(t *testing.T) {
	t.Chdir(realTempDir(t))
	code, stdout, _ := runMarkdownTasksCLI(t, "markdown", "tasks", "--json", "docs/nope.md")
	if code != 1 || !strings.Contains(stdout, `"file not found: docs/nope.md"`) {
		t.Fatalf("exit %d, want 1 with file not found: docs/nope.md: %q", code, stdout)
	}
}

// Defends the argument count being a usage error (exit 2), not a runtime
// failure or a help page with exit 0.
func TestMarkdownTasksArgumentErrors(t *testing.T) {
	if code, stdout, _ := runMarkdownTasksCLI(t, "markdown", "tasks"); code != 2 {
		t.Errorf("no file: exit %d, want 2: %q", code, stdout)
	}
	if code, stdout, _ := runMarkdownTasksCLI(t, "markdown", "tasks", "a.md", "b.md"); code != 2 {
		t.Errorf("two files: exit %d, want 2: %q", code, stdout)
	}
}

// Defends Decision 7: tasks prints no path, so --absolute is refused as a
// usage error rather than accepted and ignored.
func TestMarkdownTasksRejectsAbsolute(t *testing.T) {
	path := writeMarkdown(t, "doc.md", "- [ ] a\n")
	if code, stdout, _ := runMarkdownTasksCLI(t, "markdown", "tasks", "--absolute", path); code != 2 {
		t.Errorf("--absolute: exit %d, want 2: %q", code, stdout)
	}
}

// Defends the issue's parity requirement: on a fixture with no partial
// items, the tasks counts agree with count --mode checkboxes. The fixture
// uses only `-` markers (count's list regex does not know `*` and `+`) and
// no heading or table checkboxes, so both commands count the same items.
func TestMarkdownTasksCountsAgreeWithCount(t *testing.T) {
	path := writeMarkdown(t, "plan.md", strings.Join([]string{
		"# Plan",
		"- [x] done one",
		"- [ ] open one",
		"```",
		"- [ ] fenced, counted by neither",
		"```",
		"  - [X] indented done",
		"- [ ] open two",
		"- [x] done three",
	}, "\n")+"\n")

	code, stdout, _ := runMarkdownTasksCLI(t, "count", "--mode", "checkboxes", "--json", "--path", path)
	if code != 0 {
		t.Fatalf("count: exit %d: %q", code, stdout)
	}
	var c CountCheckboxResult
	if err := json.Unmarshal([]byte(stdout), &c); err != nil {
		t.Fatalf("count stdout is not JSON: %v: %q", err, stdout)
	}
	if c.Partial != 0 {
		t.Fatalf("fixture must have no partial items, count says %d", c.Partial)
	}

	r := tasksJSON(t, path)
	if r.Total != c.Count || r.Completed != c.Checked || r.Incomplete != c.Unchecked || r.Percent != c.Percent {
		t.Fatalf("tasks total/completed/incomplete/percent = %d/%d/%d/%v, count count/checked/unchecked/percent = %d/%d/%d/%v",
			r.Total, r.Completed, r.Incomplete, r.Percent, c.Count, c.Checked, c.Unchecked, c.Percent)
	}
	if r.Total != 5 {
		t.Fatalf("fixture sanity: total = %d, want 5", r.Total)
	}
}