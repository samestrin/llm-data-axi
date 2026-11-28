package commands

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/samestrin/llm-data-axi/v2/pkg/output"
	"github.com/spf13/cobra"
)

var (
	countMode      string
	countPath      string
	countRecursive bool
	countPattern   string
	countStyle     string
	countJSON      bool
	countMinimal   bool
	// Legacy flag aliases for backwards compatibility
	countCheckboxes bool
	countLines      bool
	countFiles      bool
)

// CountCheckboxResult holds checkbox count results
type CountCheckboxResult struct {
	Count     int     `json:"count"`
	Checked   int     `json:"checked"`
	Unchecked int     `json:"unchecked"`
	Partial   int     `json:"partial"`
	Percent   float64 `json:"percent"`
	// Skipped counts the FIFOs, devices and sockets a walk passed over:
	// reading one blocked or never ended.
	Skipped int `json:"skipped,omitempty"`
	// Help carries the next steps when the count is zero, nil otherwise.
	Help []string `json:"help,omitempty"`
}

// CountLinesResult holds line count results. Skipped is as in
// CountCheckboxResult.
type CountLinesResult struct {
	Count   int `json:"count"`
	Skipped int `json:"skipped,omitempty"`
	// Help carries the next steps when the count is zero, nil otherwise.
	Help []string `json:"help,omitempty"`
}

// CountFilesResult holds file count results
type CountFilesResult struct {
	Count int `json:"count"`
	// Help carries the next steps when the count is zero, nil otherwise.
	Help []string `json:"help,omitempty"`
}

// newCountCmd creates the count command
func newCountCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "count",
		Short: "Count checkboxes, lines, or files",
		Long: `Count checkboxes, lines, or files in a path.

Modes (use --mode or legacy flags):
  --mode checkboxes  or  --checkboxes  - Count [ ], [/], and [x] checkboxes in markdown files
  --mode lines       or  --lines       - Count lines in files
  --mode files       or  --files       - Count files matching pattern

Output fields (TOON and --json):
  count      the total
  checked, unchecked, partial, percent   checkboxes mode only
  skipped    FIFOs, devices and sockets passed over, when there were any
  help       next steps when the count is zero

Examples:
  llm-data count --mode checkboxes --path PLAN.md
  llm-data count --mode lines --path src -r
  llm-data count --mode files --path src --pattern "*.go" -r`,
		RunE: runCount,
	}
	cmd.Flags().StringVar(&countPath, "path", "", "Path to count in (required)")
	cmd.Flags().StringVar(&countMode, "mode", "", "Count mode: checkboxes, lines, files")
	cmd.Flags().BoolVarP(&countRecursive, "recursive", "r", false, "Recursive search")
	cmd.Flags().StringVar(&countPattern, "pattern", "", "Glob pattern for files mode")
	cmd.Flags().StringVar(&countStyle, "style", "all", "Checkbox style: all, list, heading, table")
	// Legacy flags for backwards compatibility with Python version
	cmd.Flags().BoolVar(&countCheckboxes, "checkboxes", false, "Count checkboxes (legacy, use --mode checkboxes)")
	cmd.Flags().BoolVar(&countLines, "lines", false, "Count lines (legacy, use --mode lines)")
	cmd.Flags().BoolVar(&countFiles, "files", false, "Count files (legacy, use --mode files)")
	// Output format flags
	cmd.Flags().BoolVar(&countJSON, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&countMinimal, "min", false, "Minimal --json or --plain output (no effect on TOON)")
	cmd.MarkFlagRequired("path")
	return cmd
}

func runCount(cmd *cobra.Command, args []string) error {
	target, err := filepath.Abs(countPath)
	if err != nil {
		return fmt.Errorf("invalid path: %s", countPath)
	}

	info, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("path does not exist: %s", target)
	}
	if err := requireRegularFile(target); err != nil {
		return err
	}

	// Handle legacy flags
	mode := countMode
	if mode == "" {
		if countCheckboxes {
			mode = "checkboxes"
		} else if countLines {
			mode = "lines"
		} else if countFiles {
			mode = "files"
		}
	}

	if mode == "" {
		return &usageHelpError{
			err:  &output.UsageError{Msg: "missing --mode: pass --mode checkboxes, lines or files (or --checkboxes, --lines, --files)"},
			help: []string{"Pass --mode checkboxes, lines or files"},
		}
	}
	switch mode {
	case "checkboxes", "lines", "files":
	default:
		return invalidFlagValue("mode", mode, "checkboxes, lines or files")
	}
	switch countStyle {
	case "", "all", "list", "heading", "table":
	default:
		return invalidFlagValue("style", countStyle, "all, list, heading or table")
	}

	switch mode {
	case "checkboxes":
		return runCountCheckboxes(cmd, target, info)
	case "lines":
		return runCountLines(cmd, target, info)
	default: // "files"; the mode was validated above
		return runCountFiles(cmd, target, info)
	}
}

func runCountCheckboxes(cmd *cobra.Command, target string, info os.FileInfo) error {
	var filesToCheck []string

	if info.IsDir() {
		pattern := "*.md"
		if countRecursive {
			pattern = "**/*.md"
		}
		matches, _ := filepath.Glob(filepath.Join(target, pattern))
		filesToCheck = matches

		// For recursive, use Walk
		if countRecursive {
			filesToCheck = []string{}
			filepath.Walk(target, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return nil
				}
				if !info.IsDir() && strings.HasSuffix(path, ".md") {
					filesToCheck = append(filesToCheck, path)
				}
				return nil
			})
		}
	} else {
		filesToCheck = []string{target}
	}
	filesToCheck, skipped := dropSpecialFiles(filesToCheck)

	checked := 0
	unchecked := 0
	partial := 0

	// Regex patterns for checkboxes
	listCheckedRe := regexp.MustCompile(`- \[[xX]\]`)
	listUncheckedRe := regexp.MustCompile(`- \[ \]`)
	listPartialRe := regexp.MustCompile(`- \[/\]`)
	headingCheckedRe := regexp.MustCompile(`(?m)^#{1,6}\s+.*\[[xX]\]`)
	headingUncheckedRe := regexp.MustCompile(`(?m)^#{1,6}\s+.*\[ \]`)
	headingPartialRe := regexp.MustCompile(`(?m)^#{1,6}\s+.*\[/\]`)
	tableCheckedRe := regexp.MustCompile(`\|\s*\[[xX]\]\s*\|`)
	tableUncheckedRe := regexp.MustCompile(`\|\s*\[ \]\s*\|`)
	tablePartialRe := regexp.MustCompile(`\|\s*\[/\]\s*\|`)

	for _, filePath := range filesToCheck {
		content, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		contentStr := stripFencedCodeBlocks(string(content))

		style := countStyle
		if style == "" {
			style = "all"
		}

		if style == "all" || style == "list" {
			checked += len(listCheckedRe.FindAllString(contentStr, -1))
			unchecked += len(listUncheckedRe.FindAllString(contentStr, -1))
			partial += len(listPartialRe.FindAllString(contentStr, -1))
		}

		if style == "all" || style == "heading" {
			checked += len(headingCheckedRe.FindAllString(contentStr, -1))
			unchecked += len(headingUncheckedRe.FindAllString(contentStr, -1))
			partial += len(headingPartialRe.FindAllString(contentStr, -1))
		}

		if style == "all" || style == "table" {
			checked += len(tableCheckedRe.FindAllString(contentStr, -1))
			unchecked += len(tableUncheckedRe.FindAllString(contentStr, -1))
			partial += len(tablePartialRe.FindAllString(contentStr, -1))
		}
	}

	total := checked + unchecked + partial
	percent := 0.0
	if total > 0 {
		percent = float64(checked) / float64(total) * 100
	}

	result := CountCheckboxResult{
		Count:     total,
		Checked:   checked,
		Unchecked: unchecked,
		Partial:   partial,
		Percent:   percent,
		Skipped:   skipped,
		Help:      emptyHelp(total == 0, countCheckboxZeroHelp(info.IsDir())...),
	}

	formatter := output.New(countJSON, countMinimal, cmd.OutOrStdout())
	return formatter.Print(result, func(w io.Writer, data interface{}) {
		r := data.(CountCheckboxResult)
		fmt.Fprintf(w, "COUNT: %d\n", r.Count)
		fmt.Fprintf(w, "CHECKED: %d\n", r.Checked)
		fmt.Fprintf(w, "UNCHECKED: %d\n", r.Unchecked)
		fmt.Fprintf(w, "PARTIAL: %d\n", r.Partial)
		fmt.Fprintf(w, "PERCENT: %.0f%%\n", r.Percent)
		if r.Skipped > 0 {
			fmt.Fprintf(w, "SKIPPED: %d (not a regular file)\n", r.Skipped)
		}
	})
}

// countCheckboxZeroHelp is the next step a checkbox count of zero offers.
// It leaves out a flag the caller already passed, as grepZeroMatchHelp does:
// telling an agent to add -r when it did is advice it cannot act on. The
// last step is unconditional, so an empty result always has something to say.
func countCheckboxZeroHelp(isDir bool) []string {
	var help []string
	if isDir && !countRecursive {
		help = append(help, "Add -r to count checkboxes in subdirectories too")
	}
	if countStyle != "" && countStyle != "all" {
		help = append(help, "Add --style all to count list, heading and table checkboxes")
	}
	// '[[]' matches a literal '[' without a backslash: TOON escapes
	// backslashes inside quoted strings, so a `\[` hint would read
	// differently in --axi than in --json.
	return append(help, "Run `llm-data grep '[[]' <path>` to see the checkbox lines in a file")
}

// countLinesZeroHelp is the next step a line count of zero offers: the
// files are empty or the walk missed them.
func countLinesZeroHelp(isDir bool) []string {
	var help []string
	if isDir && !countRecursive {
		help = append(help, "Add -r to count lines in subdirectories too")
	}
	return append(help, "Run `llm-data catfiles <path>` to check the files have content")
}

// countFilesZeroHelp is the next step a file count of zero offers, given the
// effective pattern ("*" when the caller passed none). A pattern containing
// ** already walks recursively, so -r would add nothing there.
func countFilesZeroHelp(pattern string) []string {
	var help []string
	if pattern != "*" {
		help = append(help, "Drop --pattern to count every file")
	}
	if !countRecursive && !strings.Contains(pattern, "**") {
		help = append(help, "Add -r to count files in subdirectories too")
	}
	return append(help, "Run `llm-data tree --path <dir>` to see what the directory holds")
}

// dropSpecialFiles removes the FIFOs, devices and sockets from files and
// returns how many it removed. A file that cannot be stat'ed stays, as before.
func dropSpecialFiles(files []string) ([]string, int) {
	kept := files[:0]
	skipped := 0
	for _, f := range files {
		if info, err := os.Lstat(f); err == nil && isSpecialFile(f, info) {
			skipped++
			continue
		}
		kept = append(kept, f)
	}
	return kept, skipped
}

// stripFencedCodeBlocks removes content inside fenced code blocks (``` or ~~~)
// so that checkboxes inside code examples are not counted.
func stripFencedCodeBlocks(content string) string {
	lines := strings.Split(content, "\n")
	var result []string
	inFence := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if !inFence {
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

func runCountLines(cmd *cobra.Command, target string, info os.FileInfo) error {
	var filesToCheck []string

	if info.IsDir() {
		if countRecursive {
			filepath.Walk(target, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return nil
				}
				if !info.IsDir() {
					filesToCheck = append(filesToCheck, path)
				}
				return nil
			})
		} else {
			entries, _ := os.ReadDir(target)
			for _, e := range entries {
				if !e.IsDir() {
					filesToCheck = append(filesToCheck, filepath.Join(target, e.Name()))
				}
			}
		}
	} else {
		filesToCheck = []string{target}
	}
	filesToCheck, skipped := dropSpecialFiles(filesToCheck)

	totalLines := 0
	for _, filePath := range filesToCheck {
		content, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}
		lines := strings.Count(string(content), "\n")
		// If file doesn't end with newline but has content, count as 1 line
		if len(content) > 0 && content[len(content)-1] != '\n' {
			lines++
		}
		totalLines += lines
	}

	result := CountLinesResult{
		Count:   totalLines,
		Skipped: skipped,
		Help:    emptyHelp(totalLines == 0, countLinesZeroHelp(info.IsDir())...),
	}
	formatter := output.New(countJSON, countMinimal, cmd.OutOrStdout())
	return formatter.Print(result, func(w io.Writer, data interface{}) {
		r := data.(CountLinesResult)
		fmt.Fprintf(w, "COUNT: %d\n", r.Count)
		if r.Skipped > 0 {
			fmt.Fprintf(w, "SKIPPED: %d (not a regular file)\n", r.Skipped)
		}
	})
}

func runCountFiles(cmd *cobra.Command, target string, info os.FileInfo) error {
	if !info.IsDir() {
		result := CountFilesResult{Count: 1}
		formatter := output.New(countJSON, countMinimal, cmd.OutOrStdout())
		return formatter.Print(result, func(w io.Writer, data interface{}) {
			r := data.(CountFilesResult)
			fmt.Fprintf(w, "COUNT: %d\n", r.Count)
		})
	}

	pattern := countPattern
	if pattern == "" {
		pattern = "*"
	}

	count := 0
	if countRecursive || strings.Contains(pattern, "**") {
		filepath.Walk(target, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if !info.IsDir() {
				if pattern == "*" || pattern == "**/*" {
					count++
				} else {
					matched, _ := filepath.Match(filepath.Base(pattern), filepath.Base(path))
					if matched {
						count++
					}
				}
			}
			return nil
		})
		_ = pattern // silence unused variable warning in cases where we don't use it
	} else {
		matches, _ := filepath.Glob(filepath.Join(target, pattern))
		for _, m := range matches {
			info, err := os.Stat(m)
			if err == nil && !info.IsDir() {
				count++
			}
		}
	}

	result := CountFilesResult{
		Count: count,
		Help:  emptyHelp(count == 0, countFilesZeroHelp(pattern)...),
	}
	formatter := output.New(countJSON, countMinimal, cmd.OutOrStdout())
	return formatter.Print(result, func(w io.Writer, data interface{}) {
		r := data.(CountFilesResult)
		fmt.Fprintf(w, "COUNT: %d\n", r.Count)
	})
}
