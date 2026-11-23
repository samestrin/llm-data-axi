package commands

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/samestrin/llm-data-axi/v2/internal/data/gitignore"
	"github.com/samestrin/llm-data-axi/v2/internal/data/utils"
	"github.com/samestrin/llm-data-axi/v2/pkg/output"
	"github.com/spf13/cobra"
)

var (
	catfilesMaxSize     int
	catfilesNoGitignore bool
	catfilesJSON        bool
	catfilesMinimal     bool
)

// CatFileEntry represents a single file in the catfiles result
type CatFileEntry struct {
	Path    string `json:"path,omitempty"`
	P       string `json:"p,omitempty"`
	Size    int64  `json:"size,omitempty"`
	S       *int64 `json:"s,omitempty"`
	Content string `json:"content,omitempty"`
	C       string `json:"c,omitempty"`
}

// CatfilesResult represents the catfiles result. The counts are always
// stated: a directory with no files is file_count 0, not an error and not a
// missing field.
type CatfilesResult struct {
	Files     []CatFileEntry `json:"files"`
	FileCount int            `json:"file_count"`
	TotalSize int64          `json:"total_size"`
	catfilesOmissions
}

// catfilesOmissions is what a run left out, on stdout beside the files. Both
// used to go to stderr only, so stdout read as every file in full. Truncated
// is a stop at --max-size, with NotRead the files after it; Skipped names each
// file passed over and why (binary, or not a regular file).
type catfilesOmissions struct {
	Truncated bool           `json:"truncated,omitempty"`
	NotRead   int            `json:"not_read,omitempty"`
	Skipped   []catfilesSkip `json:"skipped,omitempty"`
	Help      []string       `json:"help,omitempty"`
}

// catfilesSkip is one file catfiles passed over, and why.
type catfilesSkip struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// catfilesMinResult is CatfilesResult under --min, with only the short keys.
type catfilesMinResult struct {
	F  []CatFileEntry `json:"f,omitempty"`
	FC int            `json:"fc"`
	TS int64          `json:"ts"`
	catfilesOmissions
}

// newCatfilesCmd creates the catfiles command
func newCatfilesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "catfiles [paths...]",
		Short: "Concatenate multiple files with headers",
		Long: `Concatenate multiple files or directory contents with headers.
Each file is prefixed with a header showing the file path and size.

Respects .gitignore patterns by default.`,
		Args: cobra.MinimumNArgs(1),
		RunE: runCatfiles,
	}
	cmd.Flags().IntVar(&catfilesMaxSize, "max-size", 10, "Maximum total size in MB")
	cmd.Flags().BoolVar(&catfilesNoGitignore, "no-gitignore", false, "Disable .gitignore filtering")
	cmd.Flags().BoolVar(&catfilesJSON, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&catfilesMinimal, "min", false, "Minimal --json or --plain output (no effect on TOON)")
	return cmd
}

func runCatfiles(cmd *cobra.Command, args []string) error {
	maxSizeBytes := int64(catfilesMaxSize) * 1024 * 1024
	var totalSize int64

	// Collect files. Anything asked for that cannot be read is collected here
	// and fails the run: it used to be dropped with at most a stderr warning,
	// and an exit 0 that read as the whole input.
	var files []string
	shown := make(map[string]string) // file -> the path it is printed as
	var unreadable []pathFailure
	var omitted catfilesOmissions
	for _, pathStr := range args {
		absPath, err := filepath.Abs(pathStr)
		if err != nil {
			return fmt.Errorf("invalid path: %s", pathStr)
		}

		info, err := os.Stat(absPath)
		if err != nil {
			return fmt.Errorf("path not found: %s", pathStr)
		}

		shownAt := showPaths(absPath)
		if info.IsDir() {
			var ignorer *gitignore.Parser
			if !catfilesNoGitignore {
				ignorer, _ = gitignore.NewParser(absPath)
			}

			filepath.Walk(absPath, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					// An ignored entry was never part of the output, so it
					// cannot fail the run. A directory is probed through a
					// child path, because a "dir/" pattern matches only what
					// sits under it.
					if ignorer != nil && (ignorer.IsIgnored(path) || ignorer.IsIgnored(filepath.Join(path, "_"))) {
						return nil
					}
					unreadable = append(unreadable, pathFailure{Path: shownAt.show(path), Reason: failureReason(err)})
					return nil
				}
				if info.IsDir() {
					return nil
				}
				if !catfilesNoGitignore && strings.HasPrefix(info.Name(), ".") {
					return nil
				}
				if ignorer != nil && ignorer.IsIgnored(path) {
					return nil
				}
				if isSpecialFile(path, info) {
					omitted.Skipped = append(omitted.Skipped, catfilesSkip{Path: shownAt.show(path), Reason: notRegularReason})
					return nil
				}
				files = append(files, path)
				if _, ok := shown[path]; !ok {
					shown[path] = shownAt.show(path)
				}
				return nil
			})
		} else if !info.Mode().IsRegular() {
			// Named outright, a special file is a path that cannot be read.
			unreadable = append(unreadable, pathFailure{Path: shownAt.shown, Reason: notRegularReason})
		} else {
			files = append(files, absPath)
			if _, ok := shown[absPath]; !ok {
				shown[absPath] = shownAt.shown
			}
		}
	}

	// Deduplicate and sort
	seen := make(map[string]bool)
	uniqueFiles := []string{}
	for _, f := range files {
		if !seen[f] {
			seen[f] = true
			uniqueFiles = append(uniqueFiles, f)
		}
	}
	sort.Strings(uniqueFiles)

	// Process files and collect entries
	entries := []CatFileEntry{}
	for i, filePath := range uniqueFiles {
		info, err := os.Stat(filePath)
		if err != nil {
			unreadable = append(unreadable, pathFailure{Path: shown[filePath], Reason: failureReason(err)})
			continue
		}

		size := info.Size()
		if totalSize+size > maxSizeBytes {
			omitted.Truncated = true
			omitted.NotRead = len(uniqueFiles) - i
			omitted.Help = append(omitted.Help, fmt.Sprintf(
				"Stopped at the %dMB --max-size limit with %d file(s) not read: rerun with a larger --max-size, or name fewer paths", catfilesMaxSize, omitted.NotRead))
			break
		}

		content, err := os.ReadFile(filePath)
		if err != nil {
			unreadable = append(unreadable, pathFailure{Path: shown[filePath], Reason: failureReason(err)})
			continue
		}

		// Check if binary
		if !isTextFile(content) {
			omitted.Skipped = append(omitted.Skipped, catfilesSkip{Path: shown[filePath], Reason: "binary"})
			continue
		}

		entries = append(entries, CatFileEntry{
			Path:    shown[filePath],
			Size:    size,
			Content: string(content),
		})

		totalSize += size
	}

	if len(unreadable) > 0 {
		names := make([]string, len(unreadable))
		for i, u := range unreadable {
			names[i] = u.Path + ": " + u.Reason
		}
		return &dataError{
			msg:    fmt.Sprintf("cannot read %d of the requested paths: %s", len(unreadable), strings.Join(names, "; ")),
			fields: map[string]any{"unreadable": unreadable},
		}
	}

	// Build result
	fileCount := len(entries)
	var result interface{}
	if catfilesMinimal {
		minEntries := make([]CatFileEntry, len(entries))
		for i, e := range entries {
			size := e.Size
			minEntries[i] = CatFileEntry{
				P: e.Path,
				S: &size,
				C: e.Content,
			}
		}
		result = catfilesMinResult{
			F:                 minEntries,
			FC:                fileCount,
			TS:                totalSize,
			catfilesOmissions: omitted,
		}
	} else {
		result = CatfilesResult{
			Files:             entries,
			FileCount:         fileCount,
			TotalSize:         totalSize,
			catfilesOmissions: omitted,
		}
	}

	formatter := output.New(catfilesJSON, catfilesMinimal, cmd.OutOrStdout())
	return formatter.Print(result, func(w io.Writer, data interface{}) {
		defer func() {
			for _, s := range omitted.Skipped {
				fmt.Fprintf(w, "SKIPPED: %s (%s)\n", s.Path, s.Reason)
			}
			for _, h := range omitted.Help {
				fmt.Fprintf(w, "TRUNCATED: %s\n", h)
			}
		}()
		if len(entries) == 0 {
			fmt.Fprintln(w, "FILES: 0")
			return
		}
		for _, e := range entries {
			fmt.Fprintln(w, strings.Repeat("=", 60))
			fmt.Fprintf(w, "FILE: %s\n", e.Path)
			fmt.Fprintf(w, "SIZE: %s\n", utils.FormatSize(e.Size))
			fmt.Fprintln(w, strings.Repeat("=", 60))
			fmt.Fprintln(w, e.Content)
			fmt.Fprintln(w)
		}
	})
}

// failureReason is err without the path an *os.PathError repeats, such as
// "permission denied".
func failureReason(err error) string {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}

func isTextFile(content []byte) bool {
	// Check for null bytes (common in binary files)
	for i := 0; i < len(content) && i < 8000; i++ {
		if content[i] == 0 {
			return false
		}
	}
	return true
}
