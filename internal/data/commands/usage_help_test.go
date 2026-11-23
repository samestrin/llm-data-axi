package commands

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// Defends the next step on a usage error: `count` without --path and
// `grep x` printed cobra's bare "required flag(s)" and "requires at least 2
// arg(s)" with nothing naming what to pass or where to look. Each usage error
// carries a help list: the usage line and the --help command, plus the
// missing flag and what it takes.
func TestUsageErrorsCarryHelpLines(t *testing.T) {
	for _, c := range []struct {
		args []string
		want []string
	}{
		{[]string{"count", "--mode", "lines", "--json"}, []string{"Pass --path", "llm-data count --help"}},
		{[]string{"grep", "x", "--json"}, []string{"Usage: llm-data grep <pattern> <path>...", "llm-data grep --help"}},
		{[]string{"yaml", "get", "--file", "f.yaml", "--json"}, []string{"Usage: llm-data yaml get KEY", "llm-data yaml get --help"}},
		{[]string{"count", "--path", ".", "--json"}, []string{"Pass --mode", "llm-data count --help"}},
	} {
		code, stdout, _ := runCLI(t, c.args...)
		if code != 2 {
			t.Errorf("%v: exit %d, want 2; %s", c.args, code, stdout)
		}
		var got struct {
			Error   bool     `json:"error"`
			Message string   `json:"message"`
			Help    []string `json:"help"`
		}
		if err := json.Unmarshal([]byte(stdout), &got); err != nil {
			t.Errorf("%v: %v: %s", c.args, err, stdout)
			continue
		}
		joined := strings.Join(got.Help, "\n")
		for _, w := range c.want {
			if !strings.Contains(joined, w) {
				t.Errorf("%v: help %q does not contain %q", c.args, got.Help, w)
			}
		}
	}

	// TOON carries the same list.
	_, stdout, _ := runCLI(t, "grep", "x")
	if !strings.Contains(stdout, "help[") || !strings.Contains(stdout, "llm-data grep --help") {
		t.Errorf("TOON usage error has no help list: %q", stdout)
	}

	// An unknown flag keeps its valid_flags list beside the new help.
	_, stdout, _ = runCLI(t, "grep", "--nope", "--json")
	if !strings.Contains(stdout, `"valid_flags"`) || !strings.Contains(stdout, `"help"`) {
		t.Errorf("unknown flag lost valid_flags or has no help: %s", stdout)
	}
}

// Defends git-changes outside a repository: it echoed git's own "(or any
// parent up to mount point)" wording, which names no directory. It must name
// the directory it looked in and say what to do.
func TestGitChangesOutsideARepositoryNamesTheDirectory(t *testing.T) {
	dir := realTempDir(t)
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	t.Setenv("GIT_CEILING_DIRECTORIES", dir)

	code, stdout, _ := runCLI(t, "git-changes", "--json")
	if code != 1 {
		t.Errorf("exit %d, want 1: %s", code, stdout)
	}
	if strings.Contains(stdout, "mount point") || !strings.Contains(stdout, "not a git repository: "+dir) || !strings.Contains(stdout, `"help"`) {
		t.Errorf("want the directory named and a help list, got %s", stdout)
	}
}
