package mcp_integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Defends epic 20 T6 end to end: each of the five new tools, asked for
// format "axi", must return the TOON the CLI itself prints for the same
// arguments, not JSON. The llm-tools server appended --json --min to every
// command, so a client asking for TOON got JSON with exit 0; comparing to the
// CLI's own output (and naming a key line) means an empty, JSON or garbled
// reply cannot pass.
func TestNewToolsReturnTheCLIsTOONOverStdio(t *testing.T) {
	cli, _ := builtBinaries(t)
	dir := resolvedTempDir(t)
	md := filepath.Join(dir, "plan.md")
	js := filepath.Join(dir, "data.json")
	plan := "---\ntitle: Plan\ntags: [a, b]\n---\n# Plan\n\n## Tasks\n\n- [x] one\n- [ ] two\n\n```\n# not a header\n- [ ] not a task\n```\n\n## Notes\n\nlast\n"
	if err := os.WriteFile(md, []byte(plan), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(js, []byte(`{"users":[{"name":"ada"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		tool    string
		args    map[string]interface{}
		cliArgs []string
		keyLine string
	}{
		{"llm_data_markdown_headers", map[string]interface{}{"file": md},
			[]string{"markdown", "headers", md}, "total: 3"},
		{"llm_data_markdown_section", map[string]interface{}{"file": md, "title": "Tasks"},
			[]string{"markdown", "section", md, "Tasks"}, "title: Tasks"},
		{"llm_data_markdown_frontmatter", map[string]interface{}{"file": md},
			[]string{"markdown", "frontmatter", md}, "title: Plan"},
		{"llm_data_markdown_tasks", map[string]interface{}{"file": md},
			[]string{"markdown", "tasks", md}, "total: 2"},
		{"llm_data_json_query", map[string]interface{}{"file": js, "path": "users[0].name"},
			[]string{"json", "query", js, "users[0].name"}, "value: ada"},
	}
	for _, c := range cases {
		t.Run(c.tool, func(t *testing.T) {
			want, err := exec.Command(cli, c.cliArgs...).Output()
			if err != nil {
				t.Fatalf("%s %q: %v", cli, c.cliArgs, err)
			}
			args := map[string]interface{}{"format": "axi"}
			for k, v := range c.args {
				args[k] = v
			}
			got, isError := callText(t, c.tool, args)
			if isError {
				t.Fatalf("%s returned an error: %q", c.tool, got)
			}
			if json.Valid([]byte(strings.TrimSpace(got))) {
				t.Errorf("%s with format=axi returned JSON: %q", c.tool, got)
			}
			if !strings.Contains(got, c.keyLine) {
				t.Errorf("%s output has no %q line: %q", c.tool, c.keyLine, got)
			}
			if strings.TrimSpace(got) != strings.TrimSpace(string(want)) {
				t.Errorf("%s over MCP differs from the CLI:\n--- mcp\n%s\n--- cli\n%s", c.tool, got, want)
			}
		})
	}
}
