package commands

import (
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/samestrin/llm-data-axi/v2/pkg/output"
	"github.com/spf13/cobra"
)

// MarkdownTask is one task list item: the 1-based line it is on, whether it
// is done ([x] or [X]), and its text without the marker, trimmed.
type MarkdownTask struct {
	Line int    `json:"line"`
	Done bool   `json:"done"`
	Text string `json:"text"`
}

// MarkdownTasksResult is what markdown tasks prints. Tasks is never
// omitted: a file with none says tasks: [] and total: 0.
type MarkdownTasksResult struct {
	Tasks      []MarkdownTask `json:"tasks"`
	Total      int            `json:"total"`
	Completed  int            `json:"completed"`
	Incomplete int            `json:"incomplete"`
	Percent    float64        `json:"percent"`
}

// MarkdownTasksSummary is what --summary prints: the counts without the
// list. It is a separate type so the tasks key is absent, not empty.
type MarkdownTasksSummary struct {
	Total      int     `json:"total"`
	Completed  int     `json:"completed"`
	Incomplete int     `json:"incomplete"`
	Percent    float64 `json:"percent"`
}

// newMarkdownTasksCmd creates the markdown tasks subcommand. It is added to
// newMarkdownCmd by epic 20 T6, not here.
func newMarkdownTasksCmd() *cobra.Command {
	var summary bool
	cmd := &cobra.Command{
		Use:   "tasks FILE",
		Short: "List the task list items with their state and line",
		Long: `List the task list items (- [ ], - [x], - [X]; also * and + markers, any
indent) in a markdown file, in order. A [/] partial item is not a task state
here; count --mode checkboxes counts it.

An item inside a fenced code block or the leading frontmatter block is not a
task.

Output fields (TOON and --json):
  tasks       each item's line (1-based), done and text
  total       how many items are listed
  completed   how many are done
  incomplete  how many are not done
  percent     completed as a percentage of total (0 when there are none)

Examples:
  llm-data markdown tasks PLAN.md
  llm-data markdown tasks PLAN.md --summary
  llm-data markdown tasks PLAN.md --plain`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			content, err := readMarkdownFile(args[0])
			if err != nil {
				return err
			}
			result := MarkdownTasksResult{Tasks: []MarkdownTask{}}
			for _, l := range scanMarkdown(content) {
				if !l.Prose {
					continue
				}
				if done, text, ok := parseTaskItem(l.Text); ok {
					result.Tasks = append(result.Tasks, MarkdownTask{Line: l.Number, Done: done, Text: text})
					if done {
						result.Completed++
					} else {
						result.Incomplete++
					}
				}
			}
			result.Total = len(result.Tasks)
			if result.Total > 0 {
				result.Percent = float64(result.Completed) / float64(result.Total) * 100
			}
			if summary {
				s := MarkdownTasksSummary{
					Total:      result.Total,
					Completed:  result.Completed,
					Incomplete: result.Incomplete,
					Percent:    result.Percent,
				}
				return output.New(GlobalJSONOutput, GlobalMinOutput, cmd.OutOrStdout()).Print(s, printMarkdownTaskCounts)
			}
			return output.New(GlobalJSONOutput, GlobalMinOutput, cmd.OutOrStdout()).Print(result, printMarkdownTasks)
		},
	}
	cmd.Flags().BoolVar(&summary, "summary", false, "Only the counts, without the item list")
	return cmd
}

// markdownTaskRe matches a task list item: any indent, a -, * or + marker,
// whitespace, [ ], [x] or [X], then the end of the line or whitespace and
// the text. [/] and other bracket contents are not task states, and a
// marker or bracket without its separating whitespace is not a list item.
var markdownTaskRe = regexp.MustCompile(`^[ \t]*[-*+][ \t]+\[( |x|X)\](?:[ \t]+(.*))?$`)

// parseTaskItem reads one prose line as a task list item.
func parseTaskItem(line string) (done bool, text string, ok bool) {
	m := markdownTaskRe.FindStringSubmatch(line)
	if m == nil {
		return false, "", false
	}
	return m[1] != " ", strings.TrimSpace(m[2]), true
}

func printMarkdownTasks(w io.Writer, data interface{}) {
	r := data.(MarkdownTasksResult)
	for _, t := range r.Tasks {
		mark := " "
		if t.Done {
			mark = "x"
		}
		line := fmt.Sprintf("%d: [%s]", t.Line, mark)
		if t.Text != "" {
			line += " " + t.Text
		}
		fmt.Fprintln(w, line)
	}
	printMarkdownTaskCounts(w, MarkdownTasksSummary{
		Total:      r.Total,
		Completed:  r.Completed,
		Incomplete: r.Incomplete,
		Percent:    r.Percent,
	})
}

func printMarkdownTaskCounts(w io.Writer, data interface{}) {
	s := data.(MarkdownTasksSummary)
	fmt.Fprintf(w, "TOTAL: %d\n", s.Total)
	fmt.Fprintf(w, "COMPLETED: %d\n", s.Completed)
	fmt.Fprintf(w, "INCOMPLETE: %d\n", s.Incomplete)
	fmt.Fprintf(w, "PERCENT: %.0f%%\n", s.Percent)
}
