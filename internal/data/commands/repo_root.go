package commands

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/samestrin/llm-data-axi/pkg/output"
	"github.com/spf13/cobra"
)

var (
	repoRootValidate bool
	repoRootPath     string
	repoRootJSON     bool
	repoRootMinimal  bool
)

// RepoRootResult represents the repo root result
type RepoRootResult struct {
	Root  string `json:"root,omitempty"`
	Valid bool   `json:"valid,omitempty"`
	Error string `json:"error,omitempty"`
}

// newRepoRootCmd creates the repo-root command
func newRepoRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repo-root",
		Short: "Find and output git repository root",
		Long: `Find the git repository root for a given path.

Returns the absolute path to the repository root, suitable for anchoring
all subsequent path operations in LLM prompts.

Output format:
  ROOT: /absolute/path/to/repo
  VALID: TRUE|FALSE (with --validate)

Examples:
  llm-data repo-root
  llm-data repo-root --path /path/to/subdir
  llm-data repo-root --validate`,
		Args: cobra.NoArgs,
		RunE: runRepoRoot,
	}

	cmd.Flags().StringVar(&repoRootPath, "path", ".", "Starting path to search from")
	cmd.Flags().BoolVar(&repoRootValidate, "validate", false, "Also verify .git directory exists")
	cmd.Flags().BoolVar(&repoRootJSON, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&repoRootMinimal, "min", false, "Output in minimal/token-optimized format")

	return cmd
}

func runRepoRoot(cmd *cobra.Command, args []string) error {
	// Use path from flag
	startPath := repoRootPath

	// Resolve to absolute path
	absPath, err := filepath.Abs(startPath)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	// Verify path exists
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return fmt.Errorf("path does not exist: %s", absPath)
	}

	// Get repository root using git rev-parse
	root, err := repoRootOf(context.Background(), absPath)
	result := RepoRootResult{}

	// Only git's own answer is "not a git repository". Any other failure, git
	// missing or broken, is not an answer about the directory and exits
	// non-zero with its own reason.
	if err != nil && !errors.Is(err, errNotARepository) {
		return fmt.Errorf("repo-root: %w", err)
	}
	if err != nil {
		result.Error = "not a git repository"
		if repoRootValidate {
			result.Valid = false
		}
	} else {
		result.Root = root

		// Optionally validate .git exists
		if repoRootValidate {
			gitDir := filepath.Join(result.Root, ".git")
			if _, err := os.Stat(gitDir); err == nil {
				result.Valid = true
			} else {
				result.Valid = false
			}
		}
	}

	formatter := output.New(repoRootJSON, repoRootMinimal, cmd.OutOrStdout())
	return formatter.Print(result, printRepoRootText)
}

// errNotARepository is git's own answer that a directory is in no repository.
// Only that answer earns it: every other git failure keeps its own reason, so
// a caller can tell "no repository" from "git is broken".
var errNotARepository = errors.New("not a git repository")

// repoRootOf returns the top level of the repository containing dir.
func repoRootOf(ctx context.Context, dir string) (string, error) {
	out, err := runGitCtx(ctx, dir, "rev-parse", "--show-toplevel")
	return strings.TrimSpace(out), err
}

// runGitCtx runs git in dir (the working directory when empty) and names why
// it failed: git missing, ctx's deadline (wrapped, for errors.Is),
// errNotARepository, or git's own last line of stderr.
func runGitCtx(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	// The not-a-repository message is matched below, so it must not be
	// translated.
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	// Without this a child holding git's pipes keeps Wait open past the
	// deadline.
	cmd.WaitDelay = 100 * time.Millisecond
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err == nil {
		return string(out), nil
	}
	if errors.Is(err, exec.ErrNotFound) {
		return "", errors.New("git not found in PATH")
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return "", fmt.Errorf("git %s: %w", args[0], ctxErr)
	}
	reason := ""
	for _, line := range strings.Split(stderr.String(), "\n") {
		if line = strings.TrimSpace(line); line == "" {
			continue
		}
		if strings.HasPrefix(line, "fatal: not a git repository") {
			return "", errNotARepository
		}
		reason = line
	}
	if reason == "" {
		reason = fmt.Sprintf("git %s: %v", args[0], err)
	}
	return "", errors.New(reason)
}

func printRepoRootText(w io.Writer, data interface{}) {
	r := data.(RepoRootResult)
	fmt.Fprintf(w, "ROOT: %s\n", r.Root)
	if r.Error != "" {
		fmt.Fprintf(w, "ERROR: %s\n", r.Error)
	}
	if repoRootValidate {
		if r.Valid {
			fmt.Fprintf(w, "VALID: TRUE\n")
		} else {
			fmt.Fprintf(w, "VALID: FALSE\n")
		}
	}
}
