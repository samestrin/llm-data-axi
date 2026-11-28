package commands

import (
	"fmt"
	"io"
	"strings"

	"github.com/samestrin/llm-data-axi/v2/pkg/output"
	"github.com/spf13/cobra"
)

// markdownSectionResult is what markdown section prints. Content has no
// omitempty: a section with no body says content "", not a dropped key.
type markdownSectionResult struct {
	Title   string `json:"title"`
	Level   int    `json:"level"`
	Line    int    `json:"line"`
	Content string `json:"content"`
}

// newMarkdownSectionCmd creates the markdown section subcommand. It is not
// added to the markdown parent until epic 20 T6 wires the subcommands in.
func newMarkdownSectionCmd() *cobra.Command {
	var includeHeader bool
	cmd := &cobra.Command{
		Use:   "section FILE TITLE",
		Short: "Extract one section's content by its header title",
		Long: `Extract the content under the first header whose title matches TITLE
(exact after trimming, case-insensitive).

The section ends at the next header of the same or a higher level, or at the
end of the file. A # line inside a fenced code block or the leading
frontmatter block is not a header and does not end the section. The content
is the lines between the header and that boundary, trimmed at its ends.

A title that is not found is exit 1 with the file's header list in the error
fields, so a caller can retry with a real title without another read.

Output fields (TOON and --json):
  title     the matched header's title as the file wrote it
  level     its level (1 to 6)
  line      its line (1-based)
  content   the section body, or "" when the section is empty

--plain prints the bare content.

Examples:
  llm-data markdown section README.md Installation
  llm-data markdown section PLAN.md "Task list" --include-header
  llm-data markdown section PLAN.md Decision --plain`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			content, err := readMarkdownFile(args[0])
			if err != nil {
				return err
			}
			lines := scanMarkdown(content)
			headers := markdownHeaders(lines)
			result, ok := extractMarkdownSection(lines, headers, args[1], includeHeader)
			if !ok {
				list := headers
				if list == nil {
					list = []MarkdownHeader{}
				}
				return &dataError{
					msg:    fmt.Sprintf("section not found: %s", args[1]),
					fields: map[string]any{"headers": list},
				}
			}
			// The shared formatter, as headers uses: --min abbreviates keys
			// but keeps content "" (no omitempty), so an empty section is
			// still stated.
			return output.New(GlobalJSONOutput, GlobalMinOutput, cmd.OutOrStdout()).Print(result, printMarkdownSection)
		},
	}
	cmd.Flags().BoolVar(&includeHeader, "include-header", false, "Include the header line itself in the content")
	return cmd
}

// extractMarkdownSection finds the first header whose title equals title
// after trimming, case-insensitively, and returns its section: the lines
// after the header up to the next header of the same or a higher level (or
// the end of the file), joined with blank lines dropped at the ends only:
// strings.TrimSpace would eat the indentation of a first line that is an
// indented code block, and the content is byte-for-byte. includeHeader
// keeps the header line itself at the front. ok is false when no header
// matches, so a missing title fails the caller rather than matching "".
func extractMarkdownSection(lines []markdownLine, headers []MarkdownHeader, title string, includeHeader bool) (markdownSectionResult, bool) {
	title = strings.TrimSpace(title)
	match := -1
	for i, h := range headers {
		if strings.EqualFold(h.Title, title) {
			match = i
			break
		}
	}
	if match < 0 {
		return markdownSectionResult{}, false
	}
	h := headers[match]
	end := len(lines)
	for _, next := range headers[match+1:] {
		if next.Level <= h.Level {
			end = next.Line - 1 // 0-based index of the boundary line
			break
		}
	}
	start := h.Line // 0-based index of the first body line
	if includeHeader {
		start = h.Line - 1
	}
	for start < end && strings.TrimSpace(lines[start].Text) == "" {
		start++
	}
	for end > start && strings.TrimSpace(lines[end-1].Text) == "" {
		end--
	}
	body := make([]string, 0, end-start)
	for _, l := range lines[start:end] {
		body = append(body, l.Text)
	}
	return markdownSectionResult{
		Title:   h.Title,
		Level:   h.Level,
		Line:    h.Line,
		Content: strings.Join(body, "\n"),
	}, true
}

// printMarkdownSection is the --plain printer: the bare content, so a
// caller can pipe the section body without a wrapper.
func printMarkdownSection(w io.Writer, data interface{}) {
	fmt.Fprintln(w, data.(markdownSectionResult).Content)
}
