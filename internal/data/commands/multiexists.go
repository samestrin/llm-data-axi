package commands

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/samestrin/llm-data-axi/pkg/output"
	"github.com/spf13/cobra"
)

var (
	multiexistsVerbose bool
	multiexistsNoFail  bool
	multiexistsJSON    bool
	multiexistsMinimal bool
)

// MultiexistsEntry represents a single path check result
type MultiexistsEntry struct {
	Path   string `json:"path,omitempty"`
	Exists bool   `json:"exists"`
	Type   string `json:"type,omitempty"`
}

// MultiexistsResult represents the complete check result
type MultiexistsResult struct {
	Entries      []MultiexistsEntry `json:"entries,omitempty"`
	AllExist     bool               `json:"all_exist"`
	ExistCount   int                `json:"exist_count"`
	MissingCount int                `json:"missing_count"`
}

// newMultiexistsCmd creates the multiexists command
func newMultiexistsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "multiexists [paths...]",
		Short: "Check if multiple files/directories exist",
		Long: `Check if multiple files or directories exist.

Output format:
  ✓ path: EXISTS [type]
  ✗ path: MISSING

Summary:
  ALL_EXIST: TRUE/FALSE
  MISSING_COUNT: N
  EXIST_COUNT: N

A missing path is an error (exit 1) that lists the missing paths as
"missing", with exist_count and missing_count. With --no-fail it is a result
instead: the full listing with all_exist false, exit 0.`,
		Args: cobra.MinimumNArgs(1),
		RunE: runMultiexists,
	}
	cmd.Flags().BoolVar(&multiexistsVerbose, "verbose", false, "Show file types")
	cmd.Flags().BoolVar(&multiexistsNoFail, "no-fail", false, "Report missing paths as a result with exit 0, instead of an error with exit 1")
	cmd.Flags().BoolVar(&multiexistsJSON, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&multiexistsMinimal, "min", false, "Minimal --json or --plain output (no effect on TOON)")
	return cmd
}

func runMultiexists(cmd *cobra.Command, args []string) error {
	var entries []MultiexistsEntry
	for _, pathStr := range args {
		entry := MultiexistsEntry{Path: pathStr}

		info, err := os.Stat(pathStr)
		if err == nil {
			entry.Exists = true
			if info.IsDir() {
				entry.Type = "directory"
			} else if info.Mode()&os.ModeSymlink != 0 {
				entry.Type = "symlink"
			} else {
				entry.Type = "file"
			}
		}

		entries = append(entries, entry)
	}

	// Calculate summary
	existCount := 0
	for _, e := range entries {
		if e.Exists {
			existCount++
		}
	}
	missingCount := len(entries) - existCount
	allExist := missingCount == 0

	result := MultiexistsResult{
		Entries:      entries,
		AllExist:     allExist,
		ExistCount:   existCount,
		MissingCount: missingCount,
	}

	// A missing path fails the run (unless --no-fail) with one error document
	// that lists what is missing. It used to print the full result and then
	// return an error, which put two documents on stdout and broke any parser
	// reading it.
	if !allExist && !multiexistsNoFail {
		missing := make([]string, 0, missingCount)
		for _, e := range entries {
			if !e.Exists {
				missing = append(missing, e.Path)
			}
		}
		return &dataError{
			msg: fmt.Sprintf("%d of %d paths missing: %s", missingCount, len(entries), strings.Join(missing, ", ")),
			fields: map[string]any{
				"missing":       missing,
				"exist_count":   existCount,
				"missing_count": missingCount,
			},
		}
	}

	formatter := output.New(multiexistsJSON, multiexistsMinimal, cmd.OutOrStdout())
	return formatter.Print(result, printMultiexistsText)
}

func printMultiexistsText(w io.Writer, data interface{}) {
	result := data.(MultiexistsResult)

	// Print entries
	for _, e := range result.Entries {
		check := "✓"
		status := "EXISTS"
		if !e.Exists {
			check = "✗"
			status = "MISSING"
		}

		if multiexistsVerbose && e.Type != "" {
			fmt.Fprintf(w, "%s %s: %s (%s)\n", check, e.Path, status, e.Type)
		} else if e.Type == "directory" {
			// Always show directories with type
			fmt.Fprintf(w, "%s %s/: %s (directory)\n", check, e.Path, status)
		} else {
			fmt.Fprintf(w, "%s %s: %s\n", check, e.Path, status)
		}
	}

	// Print summary
	fmt.Fprintln(w)
	if result.AllExist {
		fmt.Fprintln(w, "ALL_EXIST: TRUE")
	} else {
		fmt.Fprintln(w, "ALL_EXIST: FALSE")
	}
	fmt.Fprintf(w, "MISSING_COUNT: %d\n", result.MissingCount)
	fmt.Fprintf(w, "EXIST_COUNT: %d\n", result.ExistCount)
}
