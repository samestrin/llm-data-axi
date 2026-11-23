package commands

import (
	"strings"
	"testing"
)

// Defends the README's exit-2 promise for a value the command cannot honour.
// Each of these used to run anyway: count --mode bogus exited 1 as if the run
// had failed, highest --type bogus was treated as "both" with exit 0, tree
// --depth -1 printed an empty tree, git-context --max-commits -3 returned
// every commit, and catfiles --max-size 0 printed no files, all exit 0.
func TestInvalidFlagValuesAreUsageErrors(t *testing.T) {
	dir := realTempDir(t)
	for _, c := range []struct {
		args []string
		flag string
	}{
		{[]string{"count", "--mode", "bogus", "--path", dir}, "--mode"},
		{[]string{"count", "--mode", "checkboxes", "--style", "bogus", "--path", dir}, "--style"},
		{[]string{"highest", "--type", "bogus", "--path", dir}, "--type"},
		{[]string{"tree", "--depth", "-1", "--path", dir}, "--depth"},
		{[]string{"tree", "--max-entries", "-1", "--path", dir}, "--max-entries"},
		{[]string{"git-context", "--max-commits", "-3", "--path", dir}, "--max-commits"},
		{[]string{"catfiles", "--max-size", "0", dir}, "--max-size"},
		{[]string{"catfiles", "--max-size", "-1", dir}, "--max-size"},
	} {
		code, stdout, _ := runCLI(t, c.args...)
		if code != 2 {
			t.Errorf("%v: exit %d, want 2; stdout %q", c.args, code, stdout)
		}
		if !strings.Contains(stdout, "error: true") || !strings.Contains(stdout, c.flag) {
			t.Errorf("%v: want an error naming %s, got %q", c.args, c.flag, stdout)
		}
	}
}
