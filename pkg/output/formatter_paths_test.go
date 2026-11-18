package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// A map reaches processMap, which is the one branch of processForMinimalJSON
// that no command exercised. Unlike a struct, a map has no omitempty tag, so a
// zero value must SURVIVE minimal mode — count=0 is an answer, not an absence.
func TestMinimalJSONKeepsZeroValuesInsideAMap(t *testing.T) {
	buf := new(bytes.Buffer)
	f := New(true, true, buf)

	err := f.Print(map[string]interface{}{
		"count":   0,
		"name":    "",
		"matches": []interface{}{},
	}, nil)
	if err != nil {
		t.Fatalf("Print: %v", err)
	}

	var got map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, buf.String())
	}

	if len(got) != 3 {
		t.Errorf("a map lost keys in minimal mode: %v", got)
	}
	if v, ok := got["count"]; !ok || v.(float64) != 0 {
		t.Errorf("count=0 was dropped from a map: %v", got)
	}
}

func TestProcessMapAbbreviatesKnownKeys(t *testing.T) {
	// The abbreviation table is the reason minimal mode is worth anything, and
	// processMap applies it on a different code path from processStruct.
	var key, abbrev string
	for k, v := range KeyAbbreviations {
		if k != v {
			key, abbrev = k, v
			break
		}
	}
	if key == "" {
		t.Skip("KeyAbbreviations has no entry that actually shortens a key")
	}

	buf := new(bytes.Buffer)
	f := New(true, true, buf)
	if err := f.Print(map[string]interface{}{key: "x"}, nil); err != nil {
		t.Fatalf("Print: %v", err)
	}

	var got map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
	if _, ok := got[abbrev]; !ok {
		t.Errorf("key %q was not abbreviated to %q: %v", key, abbrev, got)
	}
}

func TestProcessMapRecursesIntoNestedValues(t *testing.T) {
	buf := new(bytes.Buffer)
	f := New(true, true, buf)

	err := f.Print(map[string]interface{}{
		"outer": map[string]interface{}{
			"inner": []interface{}{1, 2, 3},
		},
	}, nil)
	if err != nil {
		t.Fatalf("Print: %v", err)
	}

	var got map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, buf.String())
	}
	outer, ok := got["outer"].(map[string]interface{})
	if !ok {
		t.Fatalf("nested map did not survive: %v", got)
	}
	inner, ok := outer["inner"].([]interface{})
	if !ok || len(inner) != 3 {
		t.Errorf("nested slice did not survive: %v", outer)
	}
}

func TestRelativePathCwdShortensAPathUnderTheWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	// t.TempDir can sit behind a symlink on macOS (/var -> /private/var), and
	// os.Getwd resolves it. Comparing against the resolved form is what makes
	// this test mean the same thing on both platforms.
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}

	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(resolved); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })

	if got := RelativePathCwd(filepath.Join(resolved, "a", "b.go")); got != filepath.Join("a", "b.go") {
		t.Errorf("RelativePathCwd = %q, want %q", got, filepath.Join("a", "b.go"))
	}
}

func TestRelativePathCwdPassesAnEmptyPathThrough(t *testing.T) {
	if got := RelativePathCwd(""); got != "" {
		t.Errorf("RelativePathCwd(\"\") = %q, want \"\"", got)
	}
}

// A path far outside the working directory stays absolute rather than becoming
// an unreadable ../../../.. chain.
func TestRelativePathKeepsAFarAwayPathAbsolute(t *testing.T) {
	far := filepath.Join("/", "a", "b", "c", "d", "e", "f.go")
	base := filepath.Join("/", "x", "y", "z", "w")
	if got := RelativePath(far, base); got != far {
		t.Errorf("RelativePath = %q, want the original %q", got, far)
	}
}

func TestPrintSectionIsSilentInMinimalMode(t *testing.T) {
	buf := new(bytes.Buffer)
	New(false, true, buf).PrintSection("Results")
	if buf.Len() != 0 {
		t.Errorf("minimal mode printed a section header: %q", buf.String())
	}
}

func TestPrintSectionPrintsTheHeaderInTextMode(t *testing.T) {
	buf := new(bytes.Buffer)
	New(false, false, buf).PrintSection("Results")
	got := buf.String()
	if !strings.Contains(got, "Results") {
		t.Errorf("PrintSection dropped the title: %q", got)
	}
	if !strings.HasPrefix(got, "---") {
		t.Errorf("PrintSection lost its separator: %q", got)
	}
}

func TestPrintErrorInTextModeReturnsAnExitCode(t *testing.T) {
	buf := new(bytes.Buffer)
	if code := New(false, false, buf).PrintError(errors.New("nope")); code == 0 {
		t.Error("PrintError returned 0 for a real error")
	}
}

func TestPrintErrorInJSONModeIsParseable(t *testing.T) {
	for _, minimal := range []bool{false, true} {
		buf := new(bytes.Buffer)
		code := New(true, minimal, buf).PrintError(errors.New("nope"))
		if code == 0 {
			t.Errorf("minimal=%v: PrintError returned 0", minimal)
		}
		var got map[string]interface{}
		if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
			t.Errorf("minimal=%v: error output is not JSON: %v\n%s", minimal, err, buf.String())
		}
	}
}

// fieldsErr is an error that carries data, as an unknown-flag error carries
// its valid flags. It also tries to overwrite "message", which must not win.
type fieldsErr struct{}

func (fieldsErr) Error() string { return "unknown flag: --nope" }
func (fieldsErr) ErrorFields() map[string]any {
	return map[string]any{"valid_flags": []string{"--a", "--b"}, "message": "spoofed"}
}

// Defends an error's data reaching the caller as data: valid_flags sits next
// to the message in TOON and JSON, wrapped or not, and the message, the error
// flag and the exit code are the formatter's, whatever the fields say.
func TestPrintErrorAddsErrorFields(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", fieldsErr{})
	for _, minimal := range []bool{false, true} {
		buf := new(bytes.Buffer)
		if code := New(true, minimal, buf).PrintError(err); code != 2 {
			t.Errorf("minimal=%v: exit %d, want 2", minimal, code)
		}
		var got map[string]any
		if e := json.Unmarshal(buf.Bytes(), &got); e != nil {
			t.Fatalf("minimal=%v: not JSON: %v", minimal, e)
		}
		if flags, _ := got["valid_flags"].([]any); len(flags) != 2 || flags[0] != "--a" {
			t.Errorf("minimal=%v: valid_flags = %v", minimal, got["valid_flags"])
		}
		if strings.Contains(buf.String(), "spoofed") {
			t.Errorf("minimal=%v: a field overwrote the message: %s", minimal, buf.String())
		}
	}

	buf := new(bytes.Buffer)
	f := New(false, false, buf)
	f.AXI = true
	f.PrintError(err)
	want := "error: true\nmessage: \"wrapped: unknown flag: --nope\"\nvalid_flags[2]: \"--a\",\"--b\"\n"
	if buf.String() != want {
		t.Errorf("TOON: got %q, want %q", buf.String(), want)
	}

	buf.Reset()
	New(false, false, buf).PrintError(err)
	if buf.String() != "Error: wrapped: unknown flag: --nope\n" {
		t.Errorf("text mode prints the message only, got %q", buf.String())
	}
}

func TestPrintLineSkipsEmptyValuesOnlyInMinimalMode(t *testing.T) {
	minimalBuf := new(bytes.Buffer)
	New(false, true, minimalBuf).PrintLine("name", "")
	if minimalBuf.Len() != 0 {
		t.Errorf("minimal mode printed an empty value: %q", minimalBuf.String())
	}

	textBuf := new(bytes.Buffer)
	New(false, false, textBuf).PrintLine("name", "")
	if !strings.Contains(textBuf.String(), "name") {
		t.Errorf("text mode dropped an empty value: %q", textBuf.String())
	}
}

func TestFilterEmptyKeepsOnlyMeaningfulValues(t *testing.T) {
	got := FilterEmpty(map[string]interface{}{
		"keep_string": "x",
		"keep_int":    1,
		"keep_bool":   true,
		"keep_slice":  []string{"a"},
		"drop_string": "",
		"drop_int":    0,
		"drop_uint":   uint(0),
		"drop_float":  0.0,
		"drop_bool":   false,
		"drop_slice":  []string{},
		"drop_map":    map[string]string{},
		"drop_nil":    nil,
		"drop_ptr":    (*int)(nil),
	})

	want := map[string]interface{}{
		"keep_string": "x",
		"keep_int":    1,
		"keep_bool":   true,
		"keep_slice":  []string{"a"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FilterEmpty() = %v, want %v", got, want)
	}
}

func TestProcessForMinimalJSONFollowsPointers(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
	}

	buf := new(bytes.Buffer)
	if err := New(true, true, buf).Print(&payload{Name: "x"}, nil); err != nil {
		t.Fatalf("Print: %v", err)
	}
	if !strings.Contains(buf.String(), "x") {
		t.Errorf("a pointed-to struct lost its content: %q", buf.String())
	}

	nilBuf := new(bytes.Buffer)
	if err := New(true, true, nilBuf).Print((*payload)(nil), nil); err != nil {
		t.Fatalf("Print(nil pointer): %v", err)
	}
	if strings.TrimSpace(nilBuf.String()) != "null" {
		t.Errorf("a nil pointer rendered as %q, want null", nilBuf.String())
	}
}

func TestProcessForMinimalJSONPassesScalarsThrough(t *testing.T) {
	buf := new(bytes.Buffer)
	if err := New(true, true, buf).Print(42, nil); err != nil {
		t.Fatalf("Print: %v", err)
	}
	if strings.TrimSpace(buf.String()) != "42" {
		t.Errorf("a bare scalar rendered as %q", buf.String())
	}
}

// Defends the rule epic 11.2 states for relative output paths: a path under
// the base is shortened, and anything outside it, even one level up, is
// refused rather than turned into a "../" chain.
func TestRelativePathWithinRefusesAnythingOutsideTheBase(t *testing.T) {
	base := filepath.Join("/", "work", "proj")
	for _, tc := range []struct {
		path, want string
		ok         bool
	}{
		{base, ".", true},
		{filepath.Join(base, "src", "a.go"), filepath.Join("src", "a.go"), true},
		{filepath.Join("/", "work"), "", false},
		{filepath.Join("/", "work", "other"), "", false},
		{filepath.Join("/", "work", "proj2"), "", false},
		{filepath.Join(base, "..foo"), "..foo", true},
		{"relative", "", false},
		{"", "", false},
	} {
		got, ok := RelativePathWithin(tc.path, base)
		if got != tc.want || ok != tc.ok {
			t.Errorf("RelativePathWithin(%q) = %q, %v; want %q, %v", tc.path, got, ok, tc.want, tc.ok)
		}
	}
	if _, ok := RelativePathWithin(base, ""); ok {
		t.Error("an empty base must refuse")
	}
}
