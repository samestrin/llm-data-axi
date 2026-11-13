package commands

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/samestrin/llm-data-axi/internal/data/gitignore"
	"github.com/samestrin/llm-data-axi/pkg/output"
	"github.com/spf13/cobra"
)

var (
	grepIgnoreCase  bool
	grepLineNumbers bool
	grepFilesOnly   bool
	grepNoGitignore bool
	grepJSON        bool
	grepMinimal     bool
	grepFull        bool
)

// grepOpen opens a file for searching. It is a variable so a test can make a
// read fail part-way, which no file on disk does on demand.
var grepOpen = func(path string) (io.ReadCloser, error) { return os.Open(path) }

// grepTruncateLimit is the byte limit a matching line is cut to by default,
// in every output mode (AXI principle 3). A root-level --full flag was
// rejected on purpose: every subcommand would accept it and only a few
// honour it, and a flag that is accepted and silently ignored is the bug
// this repository exists to prevent. The flag is per-command instead.
const grepTruncateLimit = 200

// grepFullHint is the next step offered when a matching line was cut. Plain
// mode prints it too: without it a cut line ends in a bare "..." that reads
// like content.
const grepFullHint = "Whole lines: rerun the same grep with --full"

// grepMaxLineBytes caps how much of one line grep holds in memory. Without it
// a file with no newline, /proc/self/pagemap for one, was read as a single
// line until the kernel killed the process. It is a variable so a test can
// lower it.
var grepMaxLineBytes = 64 << 20

// errLineTooLong is a line over grepMaxLineBytes. It fails the run: skipping
// the rest of the file would be a short result reported as a whole one.
var errLineTooLong = errors.New("longer than the grep line limit")

// GrepMatch represents a single grep match. Truncated and FullLength are set
// only when the line was cut, so the minimal {file,text} schema is unchanged
// for every line that fits. FullLength is the whole line's byte count: the
// size hint that lets a caller decide whether --full is worth it.
type GrepMatch struct {
	File       string `json:"file,omitempty"`
	Line       int    `json:"line,omitempty"`
	Text       string `json:"text,omitempty"`
	Truncated  bool   `json:"truncated,omitempty"`
	FullLength int    `json:"full_length,omitempty"`
}

// GrepResult represents the complete grep result. TotalMatches has no
// omitempty on purpose (AXI principles 4 and 5): a zero must be stated, not
// indistinguishable from an absent field. With -l it counts files with
// matches, otherwise matching lines.
type GrepResult struct {
	Pattern      string      `json:"pattern,omitempty"`
	Matches      []GrepMatch `json:"matches,omitempty"`
	Files        []string    `json:"files,omitempty"`
	TotalMatches int         `json:"total_matches"`
	// BinarySkipped names each file passed over as binary: a skipped file
	// must not look like a searched file with no match.
	BinarySkipped []string `json:"binary_skipped,omitempty"`
	Help          []string `json:"help,omitempty"`
}

// newGrepCmd creates the grep command
func newGrepCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "grep [pattern] [paths...]",
		Short: "Search for pattern in files",
		Long: `Search for a pattern in files using regular expressions.
Respects .gitignore patterns by default.`,
		Args: cobra.MinimumNArgs(2),
		RunE: runGrep,
	}
	cmd.Flags().BoolVarP(&grepIgnoreCase, "ignore-case", "i", false, "Case insensitive search")
	cmd.Flags().BoolVarP(&grepLineNumbers, "line-number", "n", false, "Show line numbers")
	cmd.Flags().BoolVarP(&grepFilesOnly, "files-with-matches", "l", false, "Only show file names")
	cmd.Flags().BoolVar(&grepNoGitignore, "no-gitignore", false, "Disable .gitignore filtering")
	cmd.Flags().BoolVar(&grepJSON, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&grepMinimal, "min", false, "Output in minimal/token-optimized format")
	cmd.Flags().BoolVar(&grepFull, "full", false, "Emit whole matching lines instead of truncating them at 200 bytes")
	return cmd
}

func runGrep(cmd *cobra.Command, args []string) error {
	pattern := args[0]
	paths := args[1:]

	// Compile regex
	flags := ""
	if grepIgnoreCase {
		flags = "(?i)"
	}
	re, err := regexp.Compile(flags + pattern)
	if err != nil {
		return fmt.Errorf("invalid pattern: %v", err)
	}

	// Collect files
	var files []string
	walkedDir := false
	for _, pathStr := range paths {
		absPath, err := filepath.Abs(pathStr)
		if err != nil {
			continue
		}

		// A path that cannot be read is an error, not an empty result: skipping
		// it made a mistyped path indistinguishable from a search with no match.
		info, err := os.Stat(absPath)
		if os.IsNotExist(err) {
			return fmt.Errorf("path not found: %s", pathStr)
		}
		if err != nil {
			return err
		}

		if info.IsDir() {
			walkedDir = true
			var ignorer *gitignore.Parser
			if !grepNoGitignore {
				ignorer, _ = gitignore.NewParser(absPath)
			}

			filepath.Walk(absPath, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return nil
				}
				if info.IsDir() {
					return nil
				}
				if !grepNoGitignore && strings.HasPrefix(info.Name(), ".") {
					return nil
				}
				if ignorer != nil && ignorer.IsIgnored(path) {
					return nil
				}
				// Walk reports a symlink as a file whatever it points at. A
				// link to a directory is not a file to search: opening it and
				// reading gives EISDIR, which would fail the whole run.
				if info.Mode()&os.ModeSymlink != 0 {
					if target, err := os.Stat(path); err == nil && target.IsDir() {
						return nil
					}
				}
				files = append(files, path)
				return nil
			})
		} else {
			files = append(files, absPath)
		}
	}

	// Search files
	result := GrepResult{
		Pattern: pattern,
	}
	matchedFiles := make(map[string]bool)
	truncatedLines := false

	for _, filePath := range files {
		file, err := grepOpen(filePath)
		if err != nil {
			continue
		}

		// A binary file is not text to search line by line: /proc/self/pagemap
		// read as text ran the process out of memory. The skip is reported, so
		// a file never searched is not a silent part of "total: 0".
		br := bufio.NewReaderSize(file, grepBinarySniff)
		if grepIsBinary(br) {
			file.Close()
			result.BinarySkipped = append(result.BinarySkipped, filePath)
			continue
		}

		lineNum := 0
		readErr := grepLines(br, func(line string) bool {
			lineNum++

			if re.MatchString(line) {
				if grepFilesOnly {
					if !matchedFiles[filePath] {
						result.Files = append(result.Files, filePath)
						matchedFiles[filePath] = true
					}
					return false
				}

				match := GrepMatch{
					File: filePath,
					Text: line,
				}
				// The truncation itself, not only the marker, must be here: the
				// structured modes marshal this struct directly, so a whole line
				// stored here is a whole line emitted in --json and --axi.
				if !grepFull && len(line) > grepTruncateLimit {
					match.Text = truncate(line, grepTruncateLimit)
					match.Truncated = true
					match.FullLength = len(line)
					truncatedLines = true
				}
				if grepLineNumbers {
					match.Line = lineNum
				}
				result.Matches = append(result.Matches, match)
			}
			return true
		})

		file.Close()
		// A read that failed part-way is an error, not a file with no more
		// matches: reporting it as the end of the file is a false zero.
		if readErr != nil {
			// An *os.PathError already names the file; say it once.
			var pathErr *os.PathError
			if errors.As(readErr, &pathErr) {
				return readErr
			}
			return fmt.Errorf("read %s: %w", filePath, readErr)
		}
	}

	if len(result.Matches) == 0 && len(result.Files) == 0 {
		result.Help = grepZeroMatchHelp(walkedDir)
	}
	if truncatedLines {
		// The caller cannot guess the flag exists; the hint is the next step.
		result.Help = append(result.Help, grepFullHint)
	}
	if grepFilesOnly {
		result.TotalMatches = len(result.Files)
	} else {
		result.TotalMatches = len(result.Matches)
	}

	formatter := output.New(grepJSON, grepMinimal, cmd.OutOrStdout())
	return formatter.Print(result, printGrepText)
}

// grepLines calls fn with each line of r, without its "\n" or "\r\n", until
// fn returns false or r is exhausted. It replaces bufio.Scanner, whose 64 KiB
// token cap stopped the scan at the first longer line: with Err() unchecked,
// a match in a minified file read as `total: 0` and exit 0. A line up to
// grepMaxLineBytes is read whole; a longer one is errLineTooLong, read no
// further than the cap, and a read error is returned, never dropped.
func grepLines(r io.Reader, fn func(line string) bool) error {
	br := bufio.NewReader(r)
	var long []byte
	lineNum := 0
	for {
		chunk, err := br.ReadSlice('\n')
		n := len(long) + len(chunk)
		if err == nil {
			n-- // the "\n" is not part of the line
		}
		if n > grepMaxLineBytes {
			return fmt.Errorf("line %d: %w (%d bytes)", lineNum+1, errLineTooLong, grepMaxLineBytes)
		}
		if err == bufio.ErrBufferFull {
			long = append(long, chunk...)
			continue
		}
		line := chunk
		if len(long) > 0 {
			line = append(long, chunk...)
			long = long[:0]
		}
		if len(line) > 0 {
			lineNum++
			if !fn(strings.TrimSuffix(strings.TrimSuffix(string(line), "\n"), "\r")) {
				return nil
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// grepBinarySniff is how much of a file grep checks for a NUL byte before
// treating it as binary: the same 8000 bytes git and GNU grep look at.
const grepBinarySniff = 8000

// grepBinarySkippedShown is how many skipped binary files plain output names
// before it counts the rest, so a tree of images does not bury the matches.
const grepBinarySkippedShown = 5

// grepIsBinary reports whether br's file starts like a binary one. It peeks,
// so the bytes are still there for grepLines. A peek error is not an answer:
// the file is read as text and grepLines meets the same error.
func grepIsBinary(br *bufio.Reader) bool {
	head, _ := br.Peek(grepBinarySniff)
	return bytes.IndexByte(head, 0) >= 0
}

// grepZeroMatchHelp is the next step a search that found nothing offers. It
// leaves out a flag the caller already passed, and the .gitignore hint when no
// directory was walked, because only a walk applies the filter.
func grepZeroMatchHelp(walkedDir bool) []string {
	var help []string
	if !grepIgnoreCase {
		help = append(help, "Add -i to match case-insensitively")
	}
	if walkedDir && !grepNoGitignore {
		help = append(help, "Add --no-gitignore to also search ignored and hidden files")
	}
	return append(help, "Run `llm-data multigrep --path <dir> --keywords <a,b>` to search several literal keywords at once")
}

func printGrepText(w io.Writer, data interface{}) {
	r := data.(GrepResult)

	// Always stated, including 0: plain mode used to print nothing at all
	// for a search with no matches, which read as "something went wrong"
	// rather than "zero matches" (AXI principles 4 and 5).
	defer fmt.Fprintf(w, "total: %d\n", r.TotalMatches)
	defer func() {
		for i, f := range r.BinarySkipped {
			if i == grepBinarySkippedShown {
				fmt.Fprintf(w, "binary skipped: %d more\n", len(r.BinarySkipped)-i)
				break
			}
			fmt.Fprintf(w, "binary skipped: %s\n", f)
		}
	}()

	if grepFilesOnly {
		for _, f := range r.Files {
			fmt.Fprintln(w, f)
		}
		return
	}

	truncated := false
	for _, m := range r.Matches {
		if grepLineNumbers && m.Line > 0 {
			fmt.Fprintf(w, "%s:%d:%s\n", m.File, m.Line, m.Text)
		} else {
			fmt.Fprintf(w, "%s:%s\n", m.File, m.Text)
		}
		truncated = truncated || m.Truncated
	}
	if truncated {
		fmt.Fprintln(w, grepFullHint)
	}
}
