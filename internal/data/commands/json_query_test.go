package commands

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/samestrin/llm-data-axi/v2/pkg/output"
)

// runJSONQuery runs `llm-data json ...` through a root that has the json
// group added, as run does for a registered command: errors are printed on
// stdout in the run's format and the exit code is classified. newJSONCmd is
// not in NewRootCmd until epic 20 T6 wires it in.
func runJSONQuery(t *testing.T, args ...string) (string, int) {
	t.Helper()
	restoreOutputMode(t)
	root := NewRootCmd()
	jsonCmd := newJSONCmd()
	argErrorsAreUsage(jsonCmd)
	root.AddCommand(jsonCmd)
	full := append([]string{"json"}, args...)
	root.SetArgs(full)
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	cmd, err := root.ExecuteC()
	if err == nil {
		return stdout.String(), 0
	}
	axi, jsonOut, minOut := outputMode(cmd, full)
	output.SetDefaultAXI(axi)
	code := output.New(jsonOut, minOut, &stdout).PrintError(withUsageHelp(cmd, err))
	return stdout.String(), code
}

// writeJSONFixture writes content to a file in a fresh temp dir.
func writeJSONFixture(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "data.json")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const jsonQueryUsers = `{"users":[{"name":"ada","age":36},{"name":"grace","age":85}],"none":null,"big":9007199254740993}`

// jsonQueryValue decodes a --json result and returns its path and value.
func jsonQueryValue(t *testing.T, out string) (string, interface{}) {
	t.Helper()
	var got struct {
		Path  string      `json:"path"`
		Value interface{} `json:"value"`
	}
	dec := json.NewDecoder(strings.NewReader(out))
	dec.UseNumber()
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("not one JSON document: %v\n%s", err, out)
	}
	if !strings.Contains(out, `"value"`) {
		t.Fatalf("value key missing: %s", out)
	}
	return got.Path, got.Value
}

// Defends Decision 6: the source fell back to reading a missing file's name
// as a JSON string, so a typo was reported as "invalid JSON" (or parsed).
// A path that does not exist is exit 1 naming the path as given.
func TestJSONQueryMissingFileIsNotParsedAsJSON(t *testing.T) {
	for _, name := range []string{"nope.json", `{"a":1}`} {
		out, code := runJSONQuery(t, "query", name, "a", "--json")
		if code != 1 {
			t.Fatalf("%s: exit %d, want 1\n%s", name, code, out)
		}
		if !strings.Contains(out, "file not found") || !strings.Contains(out, strings.ReplaceAll(name, `"`, `\"`)) {
			t.Errorf("%s: error does not name the missing file: %s", name, out)
		}
		if strings.Contains(out, "invalid JSON") || strings.Contains(out, `"value"`) {
			t.Errorf("%s: the argument was read as JSON: %s", name, out)
		}
	}
}

// Defends Decision 7: a directory named as the file is an exit-1 error naming
// it, not a read of nothing.
func TestJSONQueryDirectoryIsExit1(t *testing.T) {
	dir := t.TempDir()
	out, code := runJSONQuery(t, "query", dir, "a", "--json")
	if code != 1 {
		t.Fatalf("exit %d, want 1\n%s", code, out)
	}
	if !strings.Contains(out, dir) || !strings.Contains(out, "directory") {
		t.Errorf("error does not name the directory: %s", out)
	}
}

// Defends Decision 6: dot and bracket paths, and negative indices, resolve
// as they do in yaml get.
func TestJSONQueryResolvesPaths(t *testing.T) {
	f := writeJSONFixture(t, jsonQueryUsers)
	cases := map[string]interface{}{
		"users[0].name": "ada",
		"users[-1]":     map[string]interface{}{"name": "grace", "age": json.Number("85")},
		"users.1.age":   json.Number("85"),
	}
	for path, want := range cases {
		out, code := runJSONQuery(t, "query", f, path, "--json")
		if code != 0 {
			t.Fatalf("%s: exit %d\n%s", path, code, out)
		}
		gotPath, got := jsonQueryValue(t, out)
		if gotPath != path || !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got path %q value %#v, want %#v", path, gotPath, got, want)
		}
	}
}

// Defends Decision 6: a null at the path is a found value, exit 0 with the
// value key present; only a missing key is "path not found".
func TestJSONQueryNullIsFoundMissingIsNot(t *testing.T) {
	f := writeJSONFixture(t, jsonQueryUsers)
	out, code := runJSONQuery(t, "query", f, "none", "--json")
	if code != 0 {
		t.Fatalf("null: exit %d\n%s", code, out)
	}
	if _, v := jsonQueryValue(t, out); v != nil {
		t.Errorf("null: value %#v, want nil", v)
	}

	for _, path := range []string{"missing", "users[5]", "users[0].name.deeper", "none.x"} {
		out, code = runJSONQuery(t, "query", f, path, "--json")
		if code != 1 || !strings.Contains(out, "path not found: "+path) {
			t.Errorf("%s: exit %d, want 1 with path not found\n%s", path, code, out)
		}
	}
}

// Defends Decision 6: encoding/json without UseNumber rounds 2^53+1 to 2^53,
// so the number printed would not be the number in the file. Checked in every
// output mode.
func TestJSONQueryLargeNumberUnrounded(t *testing.T) {
	f := writeJSONFixture(t, jsonQueryUsers)
	for _, mode := range [][]string{{"--json"}, {"--plain"}, {}, {"--json", "--min"}} {
		args := append([]string{"query", f, "big"}, mode...)
		out, code := runJSONQuery(t, args...)
		if code != 0 || !strings.Contains(out, "9007199254740993") {
			t.Errorf("%v: exit %d, want the digits unrounded\n%s", mode, code, out)
		}
	}
}

// Defends Decision 6: invalid JSON is exit 1, and so is a file holding a
// second document after the first, which a single Decode would ignore.
func TestJSONQueryInvalidJSONIsExit1(t *testing.T) {
	for _, content := range []string{`{"a":`, ``, `{"a":1} {"a":2}`, `{"a":1} trailing`} {
		f := writeJSONFixture(t, content)
		out, code := runJSONQuery(t, "query", f, "a", "--json")
		if code != 1 || !strings.Contains(out, "invalid JSON") || !strings.Contains(out, f) {
			t.Errorf("%q: exit %d, want 1 naming invalid JSON in %s\n%s", content, code, f, out)
		}
	}
}

// Defends Decision 6: an empty path would walk nowhere and print the whole
// root as if it were the answer. It is a usage error, exit 2, before the
// file is read: the missing file here is not what is reported.
func TestJSONQueryEmptyPathIsUsage(t *testing.T) {
	for _, path := range []string{"", ".", ".."} {
		out, code := runJSONQuery(t, "query", "does-not-exist.json", path, "--json")
		if code != 2 || !strings.Contains(out, "empty path") {
			t.Errorf("%q: exit %d, want 2 for an empty path\n%s", path, code, out)
		}
	}
}

// Defends AC: a wrong argument count is a usage error, exit 2.
func TestJSONQueryArgCountIsUsage(t *testing.T) {
	f := writeJSONFixture(t, jsonQueryUsers)
	for _, args := range [][]string{{"query", f}, {"query", f, "a", "b"}} {
		if out, code := runJSONQuery(t, args...); code != 2 {
			t.Errorf("%v: exit %d, want 2\n%s", args, code, out)
		}
	}
}

// Defends the parent: an unknown subcommand is an error, not help and exit 0.
func TestJSONParentRejectsUnknownSubcommand(t *testing.T) {
	out, code := runJSONQuery(t, "parse")
	if code == 0 || !strings.Contains(out, "unknown command") {
		t.Errorf("exit %d, want an unknown command error\n%s", code, out)
	}
}

// Defends Decision 6: getValueAtPath takes only an object, so a file whose
// root is an array needs this command's own walk.
func TestJSONQueryArrayRoot(t *testing.T) {
	f := writeJSONFixture(t, `[{"name":"ada"},{"name":"grace"}]`)
	cases := map[string]interface{}{
		"[0].name": "ada",
		"[-1]":     map[string]interface{}{"name": "grace"},
		"1.name":   "grace",
	}
	for path, want := range cases {
		out, code := runJSONQuery(t, "query", f, path, "--json")
		if code != 0 {
			t.Fatalf("%s: exit %d\n%s", path, code, out)
		}
		if _, got := jsonQueryValue(t, out); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %#v, want %#v", path, got, want)
		}
	}
	if out, code := runJSONQuery(t, "query", f, "[2]", "--json"); code != 1 || !strings.Contains(out, "path not found") {
		t.Errorf("[2]: exit %d, want path not found\n%s", code, out)
	}
}

// Defends Decision 6 and 7: TOON by default with path and value, --plain the
// bare value (a string unquoted, anything else as JSON), --json --min just
// the value, and --min no effect on TOON.
func TestJSONQueryOutputModes(t *testing.T) {
	f := writeJSONFixture(t, jsonQueryUsers)
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"users[0].name"}, "path: users[0].name\nvalue: ada\n"},
		{[]string{"users[0].name", "--min"}, "path: users[0].name\nvalue: ada\n"},
		{[]string{"users[0].name", "--plain"}, "ada\n"},
		{[]string{"users[0].name", "--plain", "--min"}, "ada\n"},
		{[]string{"users[0]", "--plain"}, `{"age":36,"name":"ada"}` + "\n"},
		{[]string{"none", "--plain"}, "null\n"},
		{[]string{"users[0].name", "--json"}, `{"path":"users[0].name","value":"ada"}` + "\n"},
		{[]string{"users[0].name", "--json", "--min"}, `"ada"` + "\n"},
		{[]string{"none", "--json", "--min"}, "null\n"},
	}
	for _, c := range cases {
		args := append([]string{"query", f}, c.args...)
		out, code := runJSONQuery(t, args...)
		if code != 0 || out != c.want {
			t.Errorf("%v: exit %d, got %q, want %q", c.args, code, out, c.want)
		}
	}
}

// Defends Decision 6: the command's own walk follows getValueAtPath's rules
// on every object root, so `json query` and `yaml get` agree on a path.
func TestJSONWalkParityWithGetValueAtPath(t *testing.T) {
	const doc = `{
		"a": {"b": {"c": 1}},
		"list": [10, [20, 21, [30]], {"k": "v"}],
		"dotted.key": {"x": "escaped"},
		"n": null,
		"s": "str",
		"0": "zero-key"
	}`
	dec := json.NewDecoder(strings.NewReader(doc))
	dec.UseNumber()
	var root interface{}
	if err := dec.Decode(&root); err != nil {
		t.Fatal(err)
	}
	obj := root.(map[string]interface{})

	paths := []string{
		"a", "a.b", "a.b.c", "a.b.c.d", "a.missing",
		"list[0]", "list[1][0]", "list[1][2][0]", "list[1].1", "list[2].k",
		"list[-1]", "list[-1].k", "list[1][-1][-1]", "list[-3]",
		"list[3]", "list[-4]", "list[99]",
		"list[x]", "list[]", "list.k", "list[1.5]",
		`dotted\.key.x`, "dotted.key.x",
		"n", "n.x", "s", "s.x", "s[0]",
		"0", "missing", "missing.deeper",
	}
	for _, p := range paths {
		wantV, wantOK := getValueAtPath(obj, p)
		gotV, gotOK := walkJSONPath(root, parseDotPath(p))
		if gotOK != wantOK || !reflect.DeepEqual(gotV, wantV) {
			t.Errorf("%s: walk = (%#v, %v), getValueAtPath = (%#v, %v)", p, gotV, gotOK, wantV, wantOK)
		}
	}
}
