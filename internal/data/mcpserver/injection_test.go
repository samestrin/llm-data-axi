package mcpserver

import (
	"strings"
	"testing"
)

// hostile is a tool value spelled like an option. Every builder passes tool
// values to the CLI; one that lands where cobra still parses options is read
// as an option. The review's reproduction: yaml_multiset with the pair
// {"--file=h.yaml": "--create"} ran `yaml multiset --file g.yaml
// --file=h.yaml --create a 9` and wrote h.yaml, a file the caller never named.
const hostile = "--file=h.yaml"

// separatorIndex returns the index of the "--" that ends option parsing, or -1.
func separatorIndex(args []string) int {
	for i, a := range args {
		if a == "--" {
			return i
		}
	}
	return -1
}

// Defends every MCP tool that sends a positional: each value from tool input
// must follow a "--", and no option may follow it. A builder that appends a
// flag after the separator turns that flag into a positional instead.
func TestEveryPositionalFromToolInputFollowsTheSeparator(t *testing.T) {
	cases := []struct {
		tool        string
		args        map[string]interface{}
		positionals []string
	}{
		{"grep", map[string]interface{}{"pattern": "-v", "paths": []interface{}{hostile}, "ignore_case": true}, []string{"-v", hostile}},
		{"multiexists", map[string]interface{}{"paths": []interface{}{hostile}, "verbose": true}, []string{hostile}},
		{"template", map[string]interface{}{"file": hostile}, []string{hostile}},
		{"yaml_get", map[string]interface{}{"file": "g.yaml", "key": hostile, "default": "d"}, []string{hostile}},
		{"yaml_set", map[string]interface{}{"file": "g.yaml", "key": hostile, "value": "--create", "dry_run": true}, []string{hostile, "--create"}},
		{"yaml_multiget", map[string]interface{}{"file": "g.yaml", "keys": []interface{}{hostile, "--create"}}, []string{hostile, "--create"}},
		{"yaml_multiset", map[string]interface{}{"file": "g.yaml", "pairs": map[string]interface{}{hostile: "--create", "a": "9"}, "quiet": true}, []string{hostile, "--create", "a", "9"}},
		{"catfiles", map[string]interface{}{"paths": []interface{}{hostile}, "no_gitignore": true}, []string{hostile}},
		{"math", map[string]interface{}{"expression": "-1+2"}, []string{"-1+2"}},
	}
	for _, c := range cases {
		t.Run(c.tool, func(t *testing.T) {
			got, err := buildArgs(c.tool, c.args)
			if err != nil {
				t.Fatal(err)
			}
			sep := separatorIndex(got)
			if sep < 0 {
				t.Fatalf("buildArgs(%s) = %q: no \"--\" before the tool's positional values", c.tool, got)
			}
			tail := got[sep+1:]
			if strings.Join(tail, "\x00") != strings.Join(c.positionals, "\x00") {
				t.Errorf("buildArgs(%s) after \"--\" = %q, want exactly the positionals %q", c.tool, tail, c.positionals)
			}
		})
	}
}

// Defends the encoding flag: ExecuteHandler appended --axi (or --json --min)
// at the end of the line. Behind a "--" that is a positional, so the CLI
// would get an extra argument and no encoding.
func TestEncodingFlagsGoBeforeTheSeparator(t *testing.T) {
	got := withEncoding([]string{"yaml", "get", "--file", "g.yaml", "--", "k"}, []string{"--axi"})
	want := []string{"yaml", "get", "--axi", "--file", "g.yaml", "--", "k"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("withEncoding = %q, want %q", got, want)
	}
	got = withEncoding([]string{"repo-root"}, []string{"--json", "--min"})
	if strings.Join(got, " ") != "repo-root --json --min" {
		t.Errorf("withEncoding(no flags) = %q", got)
	}
	// A positional spelled like the command must not move the insertion point.
	got = withEncoding([]string{"math", "--", "1+1"}, []string{"--axi"})
	if strings.Join(got, " ") != "math --axi -- 1+1" {
		t.Errorf("withEncoding(math) = %q", got)
	}
}
