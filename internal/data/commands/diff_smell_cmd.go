package commands

import (
	"fmt"
	"io"
	"os"

	"github.com/samestrin/llm-data-axi/v2/pkg/output"
	"github.com/spf13/cobra"
)

var (
	dsDiffPath string
	dsRepo     string
	dsRev      string
	dsFull     bool
)

func newDiffSmellCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff-smell",
		Short: "Scan a diff for over-simplification / reward-hack fingerprints",
		Long: `Scan a unified diff for mechanical signs that a "fix" over-simplified rather
than addressed the problem: the change touched only tests (test_only), a test
removed assertions without replacing them (weakened_assertion), or an added line
suppressed a linter/type-checker (suppression), swallowed an exception
(empty_catch), or stubbed a body (stub_body / TODO / not-implemented).

Source is either a unified diff (--diff <path>, or '-' for stdin) or a commit
(--repo <path> --rev <sha>, scanned via 'git show'). Deterministic and
model-independent — designed as the over-simplification gate for /resolve-td,
the inverse of its existing too-complex SAFE_SCOPE gate.

Output: files, smells and summary. Each smell row is type, file, line and
severity; line is 0 for a smell about the whole file (test_only,
weakened_assertion). --full adds evidence to each row: the added line that
matched, or the reason for a whole-file smell.
summary.verdict is "hard" (any test_only/weakened_assertion → force review),
"soft_only" (only suppression/empty_catch/stub_body → adjudicate), or "clean".`,
		RunE: runDiffSmell,
	}
	cmd.Flags().StringVar(&dsDiffPath, "diff", "", "Path to a unified diff file ('-' for stdin); takes precedence over --repo/--rev")
	cmd.Flags().StringVar(&dsRepo, "repo", ".", "Repository to read the commit diff from when --diff is not given")
	cmd.Flags().StringVar(&dsRev, "rev", "HEAD", "Commit to scan via 'git show <rev>' when --diff is not given")
	cmd.Flags().BoolVar(&dsFull, "full", false, "Add each smell's evidence to its row")
	return cmd
}

func runDiffSmell(cmd *cobra.Command, _ []string) error {
	var diffText string
	if dsDiffPath != "" {
		if dsDiffPath == "-" {
			if err := refuseTerminalStdin(cmd.InOrStdin()); err != nil {
				return err
			}
			b, err := io.ReadAll(cmd.InOrStdin())
			if err != nil {
				return fmt.Errorf("read stdin: %w", err)
			}
			diffText = string(b)
		} else {
			b, err := os.ReadFile(dsDiffPath)
			if err != nil {
				return fmt.Errorf("read diff %q: %w", dsDiffPath, err)
			}
			diffText = string(b)
		}
	} else {
		// 'git show <rev>' yields the commit's diff and handles root commits.
		out, err := runGitOutput(dsRepo, "show", "--format=", "--no-color", dsRev)
		if err != nil {
			return fmt.Errorf("git show %s in %s: %w", dsRev, dsRepo, err)
		}
		diffText = out
	}

	result := analyzeDiff(diffText)
	if !dsFull && len(result.Smells) > 0 {
		for i := range result.Smells {
			result.Smells[i].Evidence = ""
		}
		result.Help = []string{"Evidence for each smell: rerun with --full"}
	}
	formatter := output.New(GlobalJSONOutput, GlobalMinOutput, cmd.OutOrStdout())
	return formatter.Print(result, func(w io.Writer, data interface{}) {
		r := data.(*SmellResult)
		fmt.Fprintf(w, "verdict: %s (%d hard, %d soft) across %d impl / %d test file(s)\n",
			r.Summary.Verdict, r.Summary.Hard, r.Summary.Soft, r.Summary.ImplFiles, r.Summary.TestFiles)
	})
}
