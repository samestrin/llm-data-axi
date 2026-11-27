package commands

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"

	"github.com/samestrin/llm-data-axi/v2/pkg/output"
	"github.com/spf13/cobra"
)

// MarkdownHeader is one ATX header: its level (1 to 6), its text, and the
// 1-based line it is on.
type MarkdownHeader struct {
	Level int    `json:"level"`
	Title string `json:"title"`
	Line  int    `json:"line"`
}

// MarkdownHeadersResult is what markdown headers prints. Headers is never
// omitted: a file with none says headers: [] and total: 0.
type MarkdownHeadersResult struct {
	Headers []MarkdownHeader `json:"headers"`
	Total   int              `json:"total"`
}

// newMarkdownCmd creates the markdown parent command.
func newMarkdownCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "markdown",
		Short: "Read the structure of a markdown file",
		Long: `Read the structure of a markdown file without reading all of it.

Lines inside fenced code blocks (` + "```" + ` and ~~~) and inside a leading YAML
frontmatter block (--- on line 1 to the next ---) are never read as markdown.

Subcommands:
  headers   List the headers with their level and line`,
		// Runnable so cobra validates the argument instead of printing help
		// and exiting 0 for a removed or misspelt subcommand.
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return fmt.Errorf("unknown command %q for %q", args[0], cmd.CommandPath())
		},
	}
	cmd.AddCommand(newMarkdownHeadersCmd())
	return cmd
}

// newMarkdownHeadersCmd creates the markdown headers subcommand.
func newMarkdownHeadersCmd() *cobra.Command {
	var levels string
	cmd := &cobra.Command{
		Use:   "headers FILE",
		Short: "List the headers with their level and line",
		Long: `List the ATX headers (# Title to ###### Title) in a markdown file, in order.

A # line inside a fenced code block or the leading frontmatter block is not a
header. Setext headers (a line underlined with === or ---) are not read.

Output fields (TOON and --json):
  headers   each header's level, title and line (1-based)
  total     how many headers are listed

Examples:
  llm-data markdown headers README.md
  llm-data markdown headers PLAN.md --level 1,2
  llm-data markdown headers PLAN.md --plain`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			want, err := parseHeaderLevels(cmd, levels)
			if err != nil {
				return err
			}
			content, err := readMarkdownFile(args[0])
			if err != nil {
				return err
			}
			result := MarkdownHeadersResult{Headers: []MarkdownHeader{}}
			for _, h := range markdownHeaders(scanMarkdown(content)) {
				if want == nil || want[h.Level] {
					result.Headers = append(result.Headers, h)
				}
			}
			result.Total = len(result.Headers)
			return output.New(GlobalJSONOutput, GlobalMinOutput, cmd.OutOrStdout()).Print(result, printMarkdownHeaders)
		},
	}
	cmd.Flags().StringVar(&levels, "level", "", "Only these header levels, comma-separated (e.g. 1,2)")
	return cmd
}

func printMarkdownHeaders(w io.Writer, data interface{}) {
	r := data.(MarkdownHeadersResult)
	for _, h := range r.Headers {
		fmt.Fprintf(w, "%d: h%d %s\n", h.Line, h.Level, h.Title)
	}
	fmt.Fprintf(w, "TOTAL: %d\n", r.Total)
}

// parseHeaderLevels reads --level: nil when it was not given, else the set of
// levels it names. A value that names no level 1 to 6 is a usage error;
// running anyway would print an empty list as if the file had none.
func parseHeaderLevels(cmd *cobra.Command, value string) (map[int]bool, error) {
	if !cmd.Flags().Changed("level") {
		return nil, nil
	}
	want := map[int]bool{}
	for _, part := range strings.Split(value, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || n < 1 || n > 6 {
			return nil, invalidFlagValue("level", value, "header levels 1 to 6, comma-separated")
		}
		want[n] = true
	}
	return want, nil
}

// readMarkdownFile reads the file every markdown subcommand works on. Each
// error names the path as the caller gave it. A FIFO or device is refused
// before anything opens it, and a directory is an error, not an empty file.
func readMarkdownFile(path string) (string, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("file not found: %s", path)
	}
	if err == nil && info.IsDir() {
		return "", fmt.Errorf("is a directory, not a file: %s", path)
	}
	if err := requireRegularFile(path); err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		var pathErr *fs.PathError
		if errors.As(err, &pathErr) {
			err = pathErr.Err
		}
		return "", fmt.Errorf("cannot read %s: %v", path, err)
	}
	return string(data), nil
}

// markdownLine is one line of a markdown file. Prose is false for a line
// inside a fenced code block (the fence lines included) or inside the
// leading frontmatter block (its --- lines included): no subcommand reads
// such a line as a header, a section boundary or a task.
type markdownLine struct {
	Number int    // 1-based
	Text   string // without its line ending
	Prose  bool
}

// scanMarkdown splits content into lines and marks which are prose. It is
// the one scanner every markdown subcommand uses, so they agree on what is
// code.
//
// A fence opens on a line of three or more backticks or tildes (after any
// indent, so a fence inside a list item counts) and closes on a line of the
// same character, at least as long, with nothing after it. An unclosed fence
// runs to the end of the file, as in CommonMark. Frontmatter is a --- on
// line 1 up to the next --- line; an unclosed one is not frontmatter, so it
// cannot hide every header after it.
func scanMarkdown(content string) []markdownLine {
	content = strings.TrimPrefix(content, "\ufeff")
	content = strings.TrimSuffix(content, "\n")
	if content == "" {
		return nil
	}
	raw := strings.Split(content, "\n")
	lines := make([]markdownLine, len(raw))
	for i, text := range raw {
		lines[i] = markdownLine{Number: i + 1, Text: strings.TrimSuffix(text, "\r"), Prose: true}
	}

	start := 0
	if strings.TrimRight(lines[0].Text, " \t") == "---" {
		for i := 1; i < len(lines); i++ {
			if strings.TrimRight(lines[i].Text, " \t") == "---" {
				for j := 0; j <= i; j++ {
					lines[j].Prose = false
				}
				start = i + 1
				break
			}
		}
	}

	var fence string // the opening fence run while inside one
	for i := start; i < len(lines); i++ {
		trimmed := strings.TrimLeft(lines[i].Text, " \t")
		if fence != "" {
			lines[i].Prose = false
			if closesFence(trimmed, fence) {
				fence = ""
			}
			continue
		}
		if run := fenceRun(trimmed); run != "" {
			lines[i].Prose = false
			fence = run
		}
	}
	return lines
}

// fenceRun returns the run of three or more backticks or tildes that opens a
// fence at the start of s, or "" when s does not open one. A backtick fence's
// info string may not hold a backtick (CommonMark), so ```foo``` is inline
// code, not a fence.
func fenceRun(s string) string {
	if len(s) < 3 || (s[0] != '`' && s[0] != '~') {
		return ""
	}
	n := 0
	for n < len(s) && s[n] == s[0] {
		n++
	}
	if n < 3 || (s[0] == '`' && strings.Contains(s[n:], "`")) {
		return ""
	}
	return s[:n]
}

// closesFence reports whether s, already trimmed of its indent, closes the
// fence opened by run.
func closesFence(s, run string) bool {
	n := 0
	for n < len(s) && s[n] == run[0] {
		n++
	}
	return n >= len(run) && strings.TrimSpace(s[n:]) == ""
}

// markdownHeaders returns the ATX headers among the prose lines.
func markdownHeaders(lines []markdownLine) []MarkdownHeader {
	var headers []MarkdownHeader
	for _, l := range lines {
		if !l.Prose {
			continue
		}
		if level, title, ok := parseATXHeader(l.Text); ok {
			headers = append(headers, MarkdownHeader{Level: level, Title: title, Line: l.Number})
		}
	}
	return headers
}

// parseATXHeader reads line as a CommonMark ATX header: up to three spaces of
// indent, one to six #, then a space, a tab or the end of the line. An
// optional closing run of # preceded by a space is not part of the title, so
// "## A ##" is "A" but "## C#" is "C#".
func parseATXHeader(line string) (level int, title string, ok bool) {
	indent := 0
	for indent < len(line) && line[indent] == ' ' {
		indent++
	}
	if indent > 3 {
		return 0, "", false
	}
	rest := line[indent:]
	for level < len(rest) && rest[level] == '#' {
		level++
	}
	if level == 0 || level > 6 {
		return 0, "", false
	}
	rest = rest[level:]
	if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
		return 0, "", false
	}
	title = strings.TrimSpace(rest)
	if stripped := strings.TrimRight(title, "#"); stripped != title {
		if stripped == "" {
			title = ""
		} else if last := stripped[len(stripped)-1]; last == ' ' || last == '\t' {
			title = strings.TrimSpace(stripped)
		}
	}
	return level, title, true
}
