package commands

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/samestrin/llm-data-axi/v2/pkg/output"
	"github.com/spf13/cobra"
)

// newJSONCmd creates the json parent command. It holds only query.
func newJSONCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "json",
		Short: "Read values from JSON files",
		Long: `Read values from JSON files with dot notation access.

Subcommands:
  query     Retrieve a value by path`,
		// Runnable so cobra validates the argument instead of printing help
		// and exiting 0 for a misspelt subcommand.
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return fmt.Errorf("unknown command %q for %q", args[0], cmd.CommandPath())
		},
	}
	cmd.AddCommand(newJSONQueryCmd())
	return cmd
}

// newJSONQueryCmd creates the json query subcommand.
func newJSONQueryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "query <file> <path>",
		Short: "Retrieve a value from a JSON file by path",
		Long: `Retrieve a value from a JSON file by dot-notation path, with the path
syntax of yaml get.

Path Format:
  users[0].name   - array index, then key
  users[-1]       - negative index: -1 is the last element
  [0].name        - a file whose root is an array
  a\.b.c          - an escaped dot is part of the key

The first argument is always a file. A missing file, a directory, invalid
JSON and a path that is not in the file are each exit 1; an empty path is a
usage error (exit 2). A null at the path is a value and exits 0. Numbers are
printed with the digits the file has, so integers past 2^53 are not rounded.

Output Formats:
  default: TOON with path and value
  --plain: the bare value; a string unquoted, anything else as JSON
  --json:  {"path": "...", "value": ...}
  --min:   just the value, with --json. No effect on TOON.

Examples:
  llm-data json query package.json version
  llm-data json query data.json users[0].name --plain
  llm-data json query data.json users[-1] --json --min`,
		Args: cobra.ExactArgs(2),
		RunE: runJSONQueryCmd,
	}
}

// jsonQueryResult is what json query prints. Value has no omitempty: a null
// found at the path is printed as null, not dropped.
type jsonQueryResult struct {
	Path  string      `json:"path"`
	Value interface{} `json:"value"`
}

func runJSONQueryCmd(cmd *cobra.Command, args []string) error {
	file, path := args[0], args[1]

	// Checked before the file is read: a path with no segments walks nowhere
	// and would print the whole document as the answer.
	parts := parseDotPath(path)
	if len(parts) == 0 {
		return &output.UsageError{Msg: fmt.Sprintf("empty path %q: name a key or index, such as users[0].name", path)}
	}

	root, err := readJSONFile(file)
	if err != nil {
		return err
	}

	value, found := walkJSONPath(root, parts)
	if !found {
		return fmt.Errorf("path not found: %s", path)
	}

	w := cmd.OutOrStdout()
	result := jsonQueryResult{Path: path, Value: value}
	if GlobalAXIOutput {
		return output.EncodeAXI(w, result)
	}
	if GlobalJSONOutput {
		if GlobalMinOutput {
			return printJSONLine(w, "json query "+path, value)
		}
		return printJSONLine(w, "json query "+path, result)
	}
	if s, ok := value.(string); ok {
		_, err := fmt.Fprintln(w, s)
		return err
	}
	return printJSONLine(w, "json query "+path, value)
}

// readJSONFile reads and decodes one JSON document from file, named as
// given in every error. It never falls back to reading file as JSON text:
// the source did, so a mistyped path was reported as invalid JSON. Numbers
// are kept as json.Number so integers past 2^53 keep their digits.
func readJSONFile(file string) (interface{}, error) {
	if err := requireRegularFile(file); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(file)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("file not found: %s", file)
		}
		return nil, fmt.Errorf("failed to read %s: %w", file, err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var root interface{}
	if err := dec.Decode(&root); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("invalid JSON in %s: no JSON value", file)
		}
		return nil, fmt.Errorf("invalid JSON in %s: %w", file, err)
	}
	// One Decode stops after the first value, so `{} {}` or `{} junk` would
	// pass as the first document alone.
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("invalid JSON in %s: data after the first value", file)
	}
	return root, nil
}

// walkJSONPath follows parts from root by getValueAtPath's rules: a key on an
// object, an index on an array (negative counts from the end), and nothing
// on a scalar. It takes any root, so a file whose root is an array works;
// getValueAtPath takes only an object.
func walkJSONPath(root interface{}, parts []string) (interface{}, bool) {
	current := root
	for _, part := range parts {
		switch v := current.(type) {
		case map[string]interface{}:
			val, ok := v[part]
			if !ok {
				return nil, false
			}
			current = val
		case []interface{}:
			idx := parseArrayIndex(part)
			if idx == invalidArrayIndex {
				return nil, false
			}
			if idx < 0 {
				idx = len(v) + idx
			}
			if idx < 0 || idx >= len(v) {
				return nil, false
			}
			current = v[idx]
		default:
			return nil, false
		}
	}
	return current, true
}
