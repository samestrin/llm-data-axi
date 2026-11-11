package commands

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestMultigrepCommand(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files
	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(`package main

func processData(input string) string {
	return input
}

func main() {
	result := processData("test")
	println(result)
}
`), 0644)

	os.WriteFile(filepath.Join(tmpDir, "utils.go"), []byte(`package main

const MaxRetries = 3

func helper() {
	// uses MaxRetries
	for i := 0; i < MaxRetries; i++ {
		processData("retry")
	}
}
`), 0644)

	tests := []struct {
		name     string
		args     []string
		expected []string
	}{
		{
			name:     "search single keyword",
			args:     []string{"--path", tmpDir, "--keywords", "processData"},
			expected: []string{"processData", "DEFINITIONS:", "main.go"},
		},
		{
			name:     "search multiple keywords",
			args:     []string{"--path", tmpDir, "--keywords", "processData,MaxRetries"},
			expected: []string{"processData", "MaxRetries", "KEYWORDS_SEARCHED: 2"},
		},
		{
			name:     "filter by extension",
			args:     []string{"--path", tmpDir, "--keywords", "func", "--extensions", "go"},
			expected: []string{"func", ".go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newMultigrepCmd()
			buf := new(bytes.Buffer)
			cmd.SetOut(buf)
			cmd.SetArgs(tt.args)

			err := cmd.Execute()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			output := buf.String()
			for _, exp := range tt.expected {
				if !strings.Contains(output, exp) {
					t.Errorf("output should contain %q, got: %s", exp, output)
				}
			}
		})
	}
}

func TestMultigrepDefinitionPriority(t *testing.T) {
	tmpDir := t.TempDir()

	os.WriteFile(filepath.Join(tmpDir, "code.ts"), []byte(`
export const ApiEndpoint = "/api/v1";

function useApiEndpoint() {
	return ApiEndpoint;
}

// ApiEndpoint is used here
console.log(ApiEndpoint);
`), 0644)

	cmd := newMultigrepCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--path", tmpDir, "--keywords", "ApiEndpoint"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	// Should show definitions first
	if !strings.Contains(output, "DEFINITIONS:") {
		t.Errorf("output should contain DEFINITIONS section: %s", output)
	}
}

func TestMultigrepJSON(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "test.go"), []byte("func test() {}"), 0644)

	cmd := newMultigrepCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--path", tmpDir, "--keywords", "test", "--json"})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, `"keywords_searched"`) {
		t.Errorf("JSON output should contain keywords_searched: %s", output)
	}
}

// TestMultigrepExplicitZeroCounts defends AXI principles 4 and 5: the count
// fields used omitempty, so a keyword with no matches printed `zzq:` with
// nothing under it in --axi and dropped `total_matches` in --json. A zero must
// be stated, not indistinguishable from an absent field.
func TestMultigrepExplicitZeroCounts(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "test.go"), []byte("func test() {}"), 0644)

	for _, tt := range []struct {
		name string
		args []string
		want []string
	}{
		{"json", []string{"--json"}, []string{`"total_matches": 0`, `"match_count": 0`, `"keywords_with_matches": 0`, `"keywords_searched": 1`, `"files_searched":`}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newMultigrepCmd()
			buf := new(bytes.Buffer)
			cmd.SetOut(buf)
			cmd.SetArgs(append([]string{"--path", tmpDir, "--keywords", "zzq"}, tt.args...))
			if err := cmd.Execute(); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			out := buf.String()
			for _, w := range tt.want {
				if !strings.Contains(out, w) {
					t.Errorf("output missing %q:\n%s", w, out)
				}
			}
		})
	}

	// --axi over the real binary: the zero must survive TOON too.
	stdout, _, code := runGrepCLI(t, "multigrep", "--path", tmpDir, "--keywords", "zzq", "--axi")
	if code != 0 {
		t.Fatalf("exit %d; stdout:\n%s", code, stdout)
	}
	if !strings.Contains(stdout, "total_matches: 0") || !strings.Contains(stdout, "match_count: 0") {
		t.Errorf("--axi dropped a zero count:\n%s", stdout)
	}
}

// TestMultigrepTruncatesContentInEveryMode defends AXI principle 3: multigrep
// used to cut match content at 80 characters in plain mode only, so --json and
// --axi emitted whole lines. The cut now applies in every mode, says it was
// cut, and --full opts out.
func TestMultigrepTruncatesContentInEveryMode(t *testing.T) {
	tmpDir := t.TempDir()
	longLine := "needle " + strings.Repeat("y", 300)
	os.WriteFile(filepath.Join(tmpDir, "test.go"), []byte(longLine+"\n"), 0644)

	for _, tt := range []struct {
		name    string
		args    []string
		fullGot bool
	}{
		{"json", []string{"--json"}, false},
		{"plain", nil, false},
		{"full", []string{"--json", "--full"}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newMultigrepCmd()
			buf := new(bytes.Buffer)
			cmd.SetOut(buf)
			cmd.SetArgs(append([]string{"--path", tmpDir, "--keywords", "needle"}, tt.args...))
			if err := cmd.Execute(); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			out := buf.String()
			if tt.name == "plain" {
				// Plain mode is not JSON: assert on the printed line and its markers.
				if strings.Count(out, "y") > 100 {
					t.Errorf("plain content emitted nearly whole (%d y's):\n%s", strings.Count(out, "y"), out)
				}
				if !strings.Contains(out, "...") || !strings.Contains(out, "--full") {
					t.Errorf("a cut plain line gave no marker or next step:\n%s", out)
				}
				return
			}
			var doc struct {
				Results map[string]struct {
					OtherMatches []struct {
						Content string `json:"content"`
					} `json:"other_matches"`
				} `json:"results"`
			}
			if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
				t.Fatalf("parse JSON: %v\n%s", err, buf.String())
			}
			r := doc.Results["needle"]
			if len(r.OtherMatches) != 1 {
				t.Fatalf("expected one match, got %d:\n%s", len(r.OtherMatches), buf.String())
			}
			got := len(r.OtherMatches[0].Content)
			if tt.fullGot && got != 307 {
				t.Errorf("--full returned %d of 300 bytes:\n%s", got, out)
			}
			if !tt.fullGot {
				if got > 100 {
					t.Errorf("content emitted nearly whole (%d y's):\n%s", got, out)
				}
				if !strings.Contains(out, "truncated") {
					t.Errorf("a cut line did not say so:\n%s", out)
				}
				if !strings.Contains(out, "--full") {
					t.Errorf("no --full next step offered:\n%s", out)
				}
			}
		})
	}
}

// TestMultigrepMinimalCountsAgree defends the --min schema: #29 dropped
// omitempty from the long count fields while --min fills only the short
// aliases, so `--json --min` (what the MCP server sends) printed `"tm":2`
// next to `"total_matches":0`. A long field in minimal output must be absent
// or agree with its alias, never a contradicting 0.
func TestMultigrepMinimalCountsAgree(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "test.go"), []byte("alpha\nbeta\n"), 0644)

	cmd := newMultigrepCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--path", tmpDir, "--keywords", "alpha,beta", "--json", "--min"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, buf.String())
	}
	for short, want := range map[string]string{"tm": "2", "ks": "2", "kwm": "2", "fs": "1"} {
		if got := string(doc[short]); got != want {
			t.Errorf("%s = %q, want %s:\n%s", short, got, want, buf.String())
		}
	}
	for _, long := range []string{"total_matches", "keywords_searched", "keywords_with_matches", "files_searched"} {
		if v, ok := doc[long]; ok {
			t.Errorf("--min emitted long field %s=%s beside its short alias:\n%s", long, v, buf.String())
		}
	}
}

// TestMultigrepTruncationHintPerKeyword defends the plain-mode hint: it
// tested every keyword's results, so a keyword with 0 matches still printed
// "(long lines truncated; rerun with --full)" when another keyword was cut.
func TestMultigrepTruncationHintPerKeyword(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "test.go"), []byte("needle "+strings.Repeat("y", 300)+"\n"), 0644)

	cmd := newMultigrepCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--path", tmpDir, "--keywords", "needle,zzz"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	needle, zzz, ok := strings.Cut(out, "KEYWORD: zzz")
	if !ok {
		t.Fatalf("no zzz section:\n%s", out)
	}
	if !strings.Contains(needle, "(long lines truncated; rerun with --full)") {
		t.Errorf("cut keyword lost its hint:\n%s", out)
	}
	if strings.Contains(zzz, "(long lines truncated") {
		t.Errorf("0-match keyword printed the truncation hint:\n%s", out)
	}
}

// TestMultigrepTruncationKeepsUTF8Valid defends the 80-byte content cut
// against splitting a multi-byte rune, which --json turned into U+FFFD.
func TestMultigrepTruncationKeepsUTF8Valid(t *testing.T) {
	tmpDir := t.TempDir()
	// 79 ASCII bytes, then 2-byte runes: the first "é" spans bytes 79-80.
	line := "needle " + strings.Repeat("y", 72) + strings.Repeat("é", 50)
	os.WriteFile(filepath.Join(tmpDir, "test.go"), []byte(line+"\n"), 0644)

	cmd := newMultigrepCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--path", tmpDir, "--keywords", "needle", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var doc struct {
		Results map[string]struct {
			OtherMatches []struct {
				Content string `json:"content"`
			} `json:"other_matches"`
		} `json:"results"`
	}
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, buf.String())
	}
	r := doc.Results["needle"]
	if len(r.OtherMatches) != 1 {
		t.Fatalf("expected one match:\n%s", buf.String())
	}
	content := r.OtherMatches[0].Content
	if !utf8.ValidString(content) || strings.ContainsRune(content, utf8.RuneError) {
		t.Errorf("cut split a rune: %q", content)
	}
	if !strings.HasSuffix(content, "...") {
		t.Errorf("cut content lost its ... marker: %q", content)
	}
}

// TestMultigrepDefinitionsHintOnlyForDisplayedLines defends the -d hint: in
// definitions mode the plain printer shows only definitions, but the hint
// counted OtherMatches too, so a short definition plus a long usage printed
// "(long lines truncated; rerun with --full)" even though no displayed line
// was cut and --full -d changed nothing.
func TestMultigrepDefinitionsHintOnlyForDisplayedLines(t *testing.T) {
	run := func(dir string, extra ...string) string {
		t.Helper()
		cmd := newMultigrepCmd()
		buf := new(bytes.Buffer)
		cmd.SetOut(buf)
		cmd.SetArgs(append([]string{"--path", dir, "--keywords", "needle"}, extra...))
		if err := cmd.Execute(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return buf.String()
	}

	// Short definition, long usage: -d displays only the definition, which is
	// not cut, so no hint may print and --full must be a no-op.
	shortDef := t.TempDir()
	os.WriteFile(filepath.Join(shortDef, "test.go"),
		[]byte("func needle() {}\nneedle "+strings.Repeat("y", 300)+"\n"), 0644)

	defs := run(shortDef, "-d")
	if strings.Contains(defs, "(long lines truncated") {
		t.Errorf("-d printed the truncation hint for a hidden, cut usage:\n%s", defs)
	}
	if full := run(shortDef, "-d", "--full"); full != defs {
		t.Errorf("--full -d changed the output although no displayed line was cut:\n%s\n---\n%s", defs, full)
	}
	// Without -d the long usage IS displayed and cut, so the hint must stay.
	if all := run(shortDef); !strings.Contains(all, "(long lines truncated; rerun with --full)") {
		t.Errorf("a displayed, cut usage lost its hint:\n%s", all)
	}

	// A cut definition is still displayed under -d, so the hint must stay.
	longDef := t.TempDir()
	os.WriteFile(filepath.Join(longDef, "test.go"),
		[]byte("func needle() { "+strings.Repeat("y", 300)+" }\n"), 0644)
	if out := run(longDef, "-d"); !strings.Contains(out, "(long lines truncated; rerun with --full)") {
		t.Errorf("-d dropped the hint for a displayed, cut definition:\n%s", out)
	}
}

// TestMultigrepCutLinesCarryFullLength defends the size hint on a cut line:
// matchInfo gained only `truncated`, so a caller could not tell how much of
// the line was missing (grep carries full_length). A cut line must state the
// whole trimmed line's byte count; --full and uncut lines carry neither field.
func TestMultigrepCutLinesCarryFullLength(t *testing.T) {
	tmpDir := t.TempDir()
	longLine := "needle " + strings.Repeat("y", 300) // 307 bytes
	os.WriteFile(filepath.Join(tmpDir, "test.go"),
		[]byte(longLine+"\nneedle short\n"), 0644)

	run := func(extra ...string) []map[string]any {
		t.Helper()
		cmd := newMultigrepCmd()
		buf := new(bytes.Buffer)
		cmd.SetOut(buf)
		cmd.SetArgs(append([]string{"--path", tmpDir, "--keywords", "needle", "--json"}, extra...))
		if err := cmd.Execute(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		var doc struct {
			Results map[string]struct {
				OtherMatches []map[string]any `json:"other_matches"`
			} `json:"results"`
		}
		if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
			t.Fatalf("parse JSON: %v\n%s", err, buf.String())
		}
		return doc.Results["needle"].OtherMatches
	}

	matches := run()
	if len(matches) != 2 {
		t.Fatalf("expected two matches, got %d", len(matches))
	}
	cut, short := matches[0], matches[1]
	if cut["truncated"] != true {
		t.Errorf("cut line did not say truncated: %v", cut)
	}
	if got, ok := cut["full_length"].(float64); !ok || int(got) != len(longLine) {
		t.Errorf("full_length = %v, want %d", cut["full_length"], len(longLine))
	}
	for _, field := range []string{"truncated", "full_length"} {
		if _, ok := short[field]; ok {
			t.Errorf("uncut line carries %s: %v", field, short)
		}
	}

	for _, m := range run("--full") {
		for _, field := range []string{"truncated", "full_length"} {
			if _, ok := m[field]; ok {
				t.Errorf("--full line carries %s: %v", field, m)
			}
		}
	}
}
