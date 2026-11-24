package commands

import (
	"strings"
	"testing"
)

// commentedYAML is a hand-maintained file: a header comment, a line comment,
// a comment inside a block, and keys deliberately out of alphabetical order.
const commentedYAML = "# header: do not remove\nzeta: 1 # zz\nalpha:\n  # inner note\n  x: 2\n"

// assertKeptComments fails when the write dropped a comment or sorted the
// keys: zeta must still come before alpha.
func assertKeptComments(t *testing.T, got string) {
	t.Helper()
	for _, want := range []string{"# header: do not remove", "zeta: 1 # zz", "# inner note"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
	if z, a := strings.Index(got, "zeta:"), strings.Index(got, "alpha:"); z < 0 || a < 0 || z > a {
		t.Errorf("keys reordered (zeta must stay before alpha):\n%s", got)
	}
}

// Defends a hand-maintained file against a new key: a key whose parents did
// not exist yet, a dotted key, a hyphenated key in multiset or a key in a file
// holding only a comment went through the map fallback, which re-encoded the
// whole file, dropping every comment (the header too) and sorting the keys,
// while the run reported success.
func TestYamlNewKeyKeepsCommentsAndOrder(t *testing.T) {
	cases := []struct {
		name  string
		args  []string
		wants []string
	}{
		{"set new nested parents", []string{"yaml", "set", "new.deep.key", "5"}, []string{"new:", "deep:", "key: 5"}},
		{"set escaped dot", []string{"yaml", "set", `a\.b`, "v"}, []string{"a.b: v"}},
		{"set below existing parent", []string{"yaml", "set", "alpha.yy.z", "6"}, []string{"yy:", "z: 6"}},
		{"multiset new nested parents", []string{"yaml", "multiset", "new.deep", "1", "other", "2"}, []string{"new:", "deep: 1", "other: 2"}},
		{"multiset hyphenated key", []string{"yaml", "multiset", "my-key", "v", "alpha.x", "3"}, []string{"my-key: v", "x: 3"}},
		{"multiset two keys under one new parent", []string{"yaml", "multiset", "p.a", "1", "p.b", "2"}, []string{"p:", "a: 1", "b: 2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, file := yamlDir(t, commentedYAML, 0o644)
			full := append(append([]string{}, tc.args[:2]...), append([]string{"--file", file}, tc.args[2:]...)...)
			if code, stdout, _ := runCLI(t, full...); code != 0 {
				t.Fatalf("exit %d: %s", code, stdout)
			}
			got := readString(t, file)
			assertKeptComments(t, got)
			for _, want := range tc.wants {
				if !strings.Contains(got, want) {
					t.Errorf("missing %q:\n%s", want, got)
				}
			}
			// The value must be where get looks for it, not merely in the text.
			key := tc.args[2]
			if code, stdout, _ := runCLI(t, "yaml", "get", "--file", file, key); code != 0 || !strings.Contains(stdout, tc.args[3]) {
				t.Errorf("get %s after write: exit %d %q", key, code, stdout)
			}
		})
	}
}

// Defends the header of a file that holds only a comment: the first key
// written into it replaced the comment.
func TestYamlNewKeyInACommentOnlyFileKeepsTheComment(t *testing.T) {
	for _, args := range [][]string{
		{"yaml", "set", "k", "v"},
		{"yaml", "multiset", "k", "v", "n.m", "1"},
	} {
		t.Run(args[1], func(t *testing.T) {
			_, file := yamlDir(t, "# only a header\n", 0o644)
			full := append(append([]string{}, args[:2]...), append([]string{"--file", file}, args[2:]...)...)
			if code, stdout, _ := runCLI(t, full...); code != 0 {
				t.Fatalf("exit %d: %s", code, stdout)
			}
			got := readString(t, file)
			if !strings.HasPrefix(got, "# only a header\n") || !strings.Contains(got, "k: v") {
				t.Errorf("comment or key lost:\n%s", got)
			}
		})
	}
}

// Defends negative indices once "-" stopped meaning "use the fallback":
// multiset must still resolve items[-1] against the data, and now keeps the
// file's comments while doing it.
func TestYamlMultisetNegativeIndexKeepsComments(t *testing.T) {
	_, file := yamlDir(t, "# head\nitems:\n  - a # first\n  - b\n", 0o644)
	if code, stdout, _ := runCLI(t, "yaml", "multiset", "--file", file, "items[-1]", "z", "my-key", "v"); code != 0 {
		t.Fatalf("exit %d: %s", code, stdout)
	}
	got := readString(t, file)
	for _, want := range []string{"# head", "- a # first", "- z", "my-key: v"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "- b") {
		t.Errorf("items[-1] was not replaced:\n%s", got)
	}
}

// Defends the negative-index test both ways: every "-" used to count, so a
// hyphenated key such as my-key took the comment-dropping path.
func TestHasNegativeIndex(t *testing.T) {
	for _, key := range []string{"items[-1]", "items.-2", "a[-1].b", "-1", "a[0][-3]"} {
		if !hasNegativeIndex(key) {
			t.Errorf("hasNegativeIndex(%q) = false, want true", key)
		}
	}
	for _, key := range []string{"my-key", "a.my-key", "a-1", "items[0]", "x.y-2.z", `a\.b-1`} {
		if hasNegativeIndex(key) {
			t.Errorf("hasNegativeIndex(%q) = true, want false", key)
		}
	}
}

// Defends the escaped dot through a hyphen: resolving negative indices split
// the key on every dot, escaped or not, so a\.my-key was written as a nested
// a: {my-key: ...} instead of the single key "a.my-key".
func TestYamlSetEscapedDotWithHyphenStaysOneKey(t *testing.T) {
	_, file := yamlDir(t, "# head\nz: 1\n", 0o644)
	if code, stdout, _ := runCLI(t, "yaml", "set", "--file", file, `a\.my-key`, "v"); code != 0 {
		t.Fatalf("exit %d: %s", code, stdout)
	}
	if got := readString(t, file); !strings.Contains(got, "a.my-key: v") || !strings.HasPrefix(got, "# head\n") {
		t.Errorf("want the single key a.my-key with the header kept:\n%s", got)
	}
}

// Defends a new key under an existing array element (items[0].newkey): it
// needs the [n] index parsed into the path, and a parser change that broke
// that would send the write to the map fallback, which drops every comment
// and sorts the keys while still reporting success.
func TestYamlNewKeyUnderAnArrayElementKeepsComments(t *testing.T) {
	const src = "# header\nzeta: 1\nitems:\n  - name: a # first\n    size: 2\n"
	for _, args := range [][]string{
		{"yaml", "set", "items[0].newkey", "v"},
		{"yaml", "multiset", "items[0].newkey", "v", "zeta", "3"},
	} {
		t.Run(args[1], func(t *testing.T) {
			_, file := yamlDir(t, src, 0o644)
			full := append(append([]string{}, args[:2]...), append([]string{"--file", file}, args[2:]...)...)
			if code, stdout, _ := runCLI(t, full...); code != 0 {
				t.Fatalf("exit %d: %s", code, stdout)
			}
			got := readString(t, file)
			for _, want := range []string{"# header", "- name: a # first", "    newkey: v"} {
				if !strings.Contains(got, want) {
					t.Errorf("missing %q:\n%s", want, got)
				}
			}
			if z, i, s, n := strings.Index(got, "zeta:"), strings.Index(got, "items:"), strings.Index(got, "size: 2"), strings.Index(got, "newkey:"); z < 0 || z > i || s > n {
				t.Errorf("keys reordered:\n%s", got)
			}
			if code, stdout, _ := runCLI(t, "yaml", "get", "--file", file, "items[0].newkey"); code != 0 || !strings.Contains(stdout, "v") {
				t.Errorf("get items[0].newkey: exit %d %q", code, stdout)
			}
		})
	}
}
