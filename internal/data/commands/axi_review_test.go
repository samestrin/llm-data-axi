package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samestrin/llm-data-axi/pkg/output"
)

// Findings from the atcr reviewer panel on the --axi change
// (review 2026-09-12_feat-axi-output-mode, 13 of 14 agents reporting).

// --- HIGH (brad, otto): AXI mode leaks into later invocations

// runRoot drives a fresh command tree, as Execute does, and returns what it wrote.
func runRoot(t *testing.T, args ...string) string {
	t.Helper()
	root := NewRootCmd()
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("%v: %v", args, err)
	}
	return buf.String()
}

func TestSecondInvocationDoesNotInheritAXI(t *testing.T) {
	// Reproduced first, through the old package-singleton RootCmd:
	//
	//   first  (--axi):  "expression: 2+2\nresult: 4\n"
	//   second (--json): "expression: 3+3\nresult: 6\n"   <- TOON, not JSON
	//
	// pflag leaves a bound variable UNTOUCHED when its flag is absent, so the
	// second invocation inherited the first one's mode. Each invocation now
	// builds its own tree, and nothing is reset in between.
	t.Cleanup(func() {
		GlobalAXIOutput, GlobalJSONOutput, GlobalMinOutput = false, false, false
		output.SetDefaultAXI(false)
	})

	first := runRoot(t, "math", "2+2", "--axi")
	if strings.HasPrefix(strings.TrimSpace(first), "{") {
		t.Fatalf("--axi did not take effect: %q", first)
	}

	second := runRoot(t, "math", "3+3", "--json")
	if !strings.HasPrefix(strings.TrimSpace(second), "{") {
		t.Errorf("--axi leaked into the next invocation:\nfirst  = %q\nsecond = %q", first, second)
	}
}

func TestSecondInvocationWithoutFlagsIsPlain(t *testing.T) {
	// The case the task names: a run with --axi, then a run with no output flag
	// at all, must not print TOON. The unconditional publish in PersistentPreRun
	// is what clears the package default; this pins it together with the fresh
	// bound variable.
	t.Cleanup(func() {
		GlobalAXIOutput, GlobalJSONOutput, GlobalMinOutput = false, false, false
		output.SetDefaultAXI(false)
	})

	axi := runRoot(t, "math", "2+2", "--axi")
	plain := runRoot(t, "math", "2+2")
	if plain == axi {
		t.Errorf("a run without --axi printed the previous run's TOON:\n%q", plain)
	}
	if GlobalAXIOutput {
		t.Error("GlobalAXIOutput is still set after a run without --axi")
	}
}

// --- MED (greta, dax): yaml paths that return before the AXI guard

func writeYAMLFixture(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(p, []byte("name: demo\nnested:\n  inner: value\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func runYAMLWithAXI(t *testing.T, args ...string) string {
	t.Helper()
	t.Cleanup(func() {
		GlobalAXIOutput = false
		output.SetDefaultAXI(false)
	})
	GlobalAXIOutput = true
	output.SetDefaultAXI(true)

	cmd := newYamlCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute %v: %v", args, err)
	}
	return buf.String()
}

// dryRunBanner is the first line the human preview prints, captured from the
// binary rather than guessed. An earlier version of these tests asserted on
// "DRY-RUN" with a hyphen; the real text has a space, so the assertion matched
// nothing and both tests passed while the bug was live. A test that cannot fail
// is worse than no test, because it is counted as coverage.
const dryRunBanner = "DRY RUN - No changes written"

func TestYAMLDryRunHonoursAXI(t *testing.T) {
	// `yaml set --dry-run` returns through outputDryRunPreview before reaching
	// the AXI guard, so it accepted --axi and printed its ordinary preview.
	// Verified through the binary: the output is byte-identical with and
	// without the flag.
	got := runYAMLWithAXI(t, "set", "--file", writeYAMLFixture(t), "name", "other", "--dry-run")
	if strings.Contains(got, dryRunBanner) {
		t.Errorf("--axi was ignored on the dry-run path:\n%s", got)
	}
	if !strings.Contains(got, "other") {
		t.Errorf("the new value did not survive:\n%s", got)
	}
}

func TestYAMLMultisetDryRunHonoursAXI(t *testing.T) {
	// Same shape, via outputMultiDryRunPreview.
	got := runYAMLWithAXI(t, "multiset", "--file", writeYAMLFixture(t), "name", "other", "--dry-run")
	if strings.Contains(got, dryRunBanner) {
		t.Errorf("--axi was ignored on the multiset dry-run path:\n%s", got)
	}
	if !strings.Contains(got, "other") {
		t.Errorf("the new value did not survive:\n%s", got)
	}
}
