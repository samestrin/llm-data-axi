package commands

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/samestrin/llm-data-axi/v2/pkg/output"
	"github.com/spf13/cobra"
)

// markdownFrontmatterResult is what markdown frontmatter prints. The parsed
// mapping hangs under a frontmatter key rather than being the document
// itself: a block with no keys is an empty mapping, and TOON encodes a bare
// empty mapping as nothing at all — an empty document with exit 0, which
// reads as "nothing happened". Under the key, the empty block still says
// frontmatter: (TOON) and frontmatter: {} (--json).
type markdownFrontmatterResult struct {
	Frontmatter map[string]interface{} `json:"frontmatter"`
}

// newMarkdownFrontmatterCmd creates the markdown frontmatter subcommand.
// It is not added to newMarkdownCmd until epic 20 T6 wires the tree.
func newMarkdownFrontmatterCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "frontmatter FILE",
		Short: "Read the YAML frontmatter of a markdown file",
		Long: `Read the leading YAML frontmatter block of a markdown file: --- on line 1
up to the next --- line. The block is parsed as YAML, so nested keys and
lists keep their structure instead of being flattened to strings.

No block, an unterminated block (no closing ---) and YAML that does not
parse are three different errors, each exit 1 naming the file and the cause.
A block whose root is not a mapping of keys to values, or that holds more
than one YAML document, is invalid YAML too.
A terminated block with no keys is empty frontmatter, not an error.

Output Formats:
  default: TOON with the frontmatter mapping
  --plain: the raw block without the --- lines (CRLF becomes LF)
  --json:  {"frontmatter": {...}}
  --min:   abbreviated keys and no empty values, with --json

Examples:
  llm-data markdown frontmatter PLAN.md
  llm-data markdown frontmatter PLAN.md --json
  llm-data markdown frontmatter PLAN.md --plain`,
		Args: cobra.ExactArgs(1),
		RunE: runMarkdownFrontmatterCmd,
	}
}

func runMarkdownFrontmatterCmd(cmd *cobra.Command, args []string) error {
	path := args[0]
	content, err := readMarkdownFile(path)
	if err != nil {
		return err
	}
	block, err := frontmatterBlock(content, path)
	if err != nil {
		return err
	}
	data, err := parseFrontmatter(block, path)
	if err != nil {
		return err
	}
	result := markdownFrontmatterResult{Frontmatter: data}
	printResult := output.New(GlobalJSONOutput, GlobalMinOutput, cmd.OutOrStdout()).Print
	if err := printResult(result, func(w io.Writer, _ interface{}) {
		if block != "" {
			fmt.Fprintln(w, block)
		}
	}); err != nil {
		// A value YAML allows but JSON cannot encode (.inf, .nan) fails
		// here; name the file so the error says whose frontmatter it was.
		return fmt.Errorf("cannot print frontmatter of %s: %w", path, err)
	}
	return nil
}

// frontmatterBlock returns the raw lines between the leading --- markers,
// without the markers, joined with \n. It follows the same delimiter rule
// as scanMarkdown, so headers and frontmatter agree on what the block is:
// a byte order mark is ignored, --- may carry trailing spaces or tabs, and
// only a block opening on line 1 counts. No block and an unterminated one
// are different errors: the first file never had frontmatter, the second
// has a broken block the caller must fix.
func frontmatterBlock(content, path string) (string, error) {
	content = strings.TrimPrefix(content, "\ufeff")
	content = strings.TrimSuffix(content, "\n")
	var lines []string
	if content != "" {
		lines = strings.Split(content, "\n")
	}
	isMarker := func(s string) bool {
		return strings.TrimRight(strings.TrimSuffix(s, "\r"), " \t") == "---"
	}
	if len(lines) == 0 || !isMarker(lines[0]) {
		return "", fmt.Errorf("no frontmatter block in %s: the first line is not ---", path)
	}
	for i := 1; i < len(lines); i++ {
		if isMarker(lines[i]) {
			body := make([]string, i-1)
			for j, line := range lines[1:i] {
				body[j] = strings.TrimSuffix(line, "\r")
			}
			return strings.Join(body, "\n"), nil
		}
	}
	return "", fmt.Errorf("unterminated frontmatter block in %s: no closing --- line", path)
}

// parseFrontmatter decodes the block with go-yaml into nested values. The
// source split lines on ":" into a map[string]string, which turned lists
// and nested mappings into wrong strings; a real parser keeps them.
//
// One Decode stops at the first document, so a ... end marker followed by
// more keys would silently drop them (probed: yaml.Unmarshal returned only
// the first document, exit 0). A second Decode must say EOF, as in
// readJSONFile; anything else is more than one document, not frontmatter.
//
// An empty or comments-only block decodes to EOF and becomes an empty
// mapping, so the empty state is stated, not dropped. A root that is not a
// mapping (a list, a bare scalar) is not frontmatter: it is refused as
// invalid YAML rather than printed as a document the caller cannot read as
// keys.
func parseFrontmatter(block, path string) (map[string]interface{}, error) {
	dec := yaml.NewDecoder(strings.NewReader(block))
	var v interface{}
	if err := dec.Decode(&v); err != nil {
		if errors.Is(err, io.EOF) {
			return map[string]interface{}{}, nil
		}
		return nil, fmt.Errorf("invalid frontmatter YAML in %s: %w", path, err)
	}
	var extra interface{}
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("invalid frontmatter YAML in %s: more than one YAML document in the block", path)
	}
	switch data := v.(type) {
	case nil:
		return map[string]interface{}{}, nil
	case map[string]interface{}:
		return data, nil
	case []interface{}:
		return nil, fmt.Errorf("invalid frontmatter YAML in %s: expected key: value mappings, got a list", path)
	default:
		return nil, fmt.Errorf("invalid frontmatter YAML in %s: expected key: value mappings, got a scalar", path)
	}
}
