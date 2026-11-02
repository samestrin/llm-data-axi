package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/samestrin/llm-data-axi/pkg/output"
	"github.com/spf13/cobra"
)

// newYamlCmd creates the yaml parent command
func newYamlCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "yaml",
		Short: "Manage YAML configuration files",
		Long: `Manage YAML configuration files with dot notation access.

The yaml command provides a way to read, write, and manage structured
YAML configuration files using dot notation for nested keys.

Subcommands:
  get       Retrieve a value by key
  set       Store a value at a key
  multiget  Retrieve multiple values
  multiset  Store multiple values atomically`,
	}

	// Add subcommands
	cmd.AddCommand(newYamlGetCmd())
	cmd.AddCommand(newYamlSetCmd())
	cmd.AddCommand(newYamlMultigetCmd())
	cmd.AddCommand(newYamlMultisetCmd())

	return cmd
}

// newYamlGetCmd creates the yaml get subcommand
func newYamlGetCmd() *cobra.Command {
	var file string
	var defaultValue string
	var jsonOutput bool
	var minOutput bool

	cmd := &cobra.Command{
		Use:   "get KEY",
		Short: "Retrieve a value by key",
		Long: `Retrieve a value from the YAML config file by dot-notation key.

Key Format:
  helper.llm      - nested key
  items[0]        - array index
  deeply.nested.value - deep nesting

Output Formats:
  default: key: value
  --json:  {"key": "...", "value": "..."}
  --min:   value (just the value)

Examples:
  yaml get --file config.yaml helper.llm
  yaml get --file config.yaml helper.llm --min
  yaml get --file config.yaml missing.key --default "fallback"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if file == "" {
				return fmt.Errorf("--file flag is required")
			}

			key := args[0]

			// Acquire read lock
			lock, err := yamlFileLock(file, false)
			if err != nil {
				return err
			}
			defer lock.Unlock()

			// Read file
			data, err := readYAMLAsMap(file)
			if err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("config file not found: %s (hint: run 'llm-data yaml init --file %s' first)", file, file)
				}
				return fmt.Errorf("failed to read config file: %w", err)
			}

			// Get value
			value, found := getValueAtPath(data, key)
			if !found {
				if defaultValue != "" {
					value = defaultValue
				} else {
					return fmt.Errorf("key not found: %s", key)
				}
			}

			// Format output
			if GlobalAXIOutput {
				return output.EncodeAXI(cmd.OutOrStdout(), map[string]interface{}{"key": key, "value": value})
			}
			if jsonOutput {
				if minOutput {
					// --json --min: just the value as JSON
					jsonBytes, _ := json.Marshal(value)
					fmt.Fprintln(cmd.OutOrStdout(), string(jsonBytes))
				} else {
					// --json: full JSON with key and value
					output := map[string]interface{}{"key": key, "value": value}
					jsonBytes, _ := json.Marshal(output)
					fmt.Fprintln(cmd.OutOrStdout(), string(jsonBytes))
				}
			} else if minOutput {
				fmt.Fprintln(cmd.OutOrStdout(), formatValue(value))
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", key, formatValue(value))
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&file, "file", "", "Path to YAML config file (required)")
	cmd.Flags().StringVar(&defaultValue, "default", "", "Default value if key not found")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&minOutput, "min", false, "Minimal output (just value)")
	cmd.MarkFlagRequired("file")

	return cmd
}

// newYamlSetCmd creates the yaml set subcommand
func newYamlSetCmd() *cobra.Command {
	var file string
	var create bool
	var jsonOutput bool
	var minOutput bool
	var dryRun bool
	var quiet bool

	cmd := &cobra.Command{
		Use:   "set KEY VALUE",
		Short: "Store a value at a key",
		Long: `Store a value in the YAML config file at the specified key.

Creates intermediate keys if they don't exist.
Preserves comments where possible.

Use '-' as VALUE to read from stdin (for piping values or multi-line input).
Use --dry-run to preview changes without writing to the file.
Use --quiet to suppress success messages (errors still output).

Examples:
  yaml set --file config.yaml helper.llm claude
  yaml set --file config.yaml helper.max_lines 2500
  yaml set --file config.yaml new.nested.key value --create
  echo "piped" | yaml set --file config.yaml key -
  yaml set --file config.yaml key value --dry-run`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if file == "" {
				return fmt.Errorf("--file flag is required")
			}

			key := args[0]
			value := args[1]

			// Handle stdin input when value is "-"
			if value == "-" {
				stdinValue, err := readFromStdin(cmd)
				if err != nil {
					return fmt.Errorf("failed to read value from stdin: %w", err)
				}
				value = stdinValue
			}

			// Check if file exists
			if _, err := os.Stat(file); os.IsNotExist(err) {
				if !create {
					return fmt.Errorf("config file not found: %s (hint: use --create to create it, or run 'llm-data yaml init --file %s' first)", file, file)
				}
				// Create empty file (unless dry-run)
				if !dryRun {
					if err := os.WriteFile(file, []byte(""), 0644); err != nil {
						return fmt.Errorf("failed to create config file: %w", err)
					}
				}
			}

			// Set value (convert numeric strings to numbers)
			var typedValue interface{} = value
			if num, err := parseNumber(value); err == nil {
				typedValue = num
			}

			// Handle dry-run mode - preview without writing
			if dryRun {
				var oldValue interface{}
				if _, err := os.Stat(file); err == nil {
					// Acquire read lock to prevent race condition with concurrent writes
					lock, err := yamlFileLock(file, false) // false = read lock
					if err != nil {
						return fmt.Errorf("failed to acquire read lock for dry-run: %w", err)
					}
					data, err := readYAMLAsMap(file)
					lock.Unlock()
					if err == nil {
						oldValue, _ = getValueAtPath(data, key)
					}
				}
				return outputDryRunPreview(cmd, file, key, oldValue, typedValue, jsonOutput, minOutput)
			}

			// Acquire write lock
			lock, err := yamlFileLock(file, true)
			if err != nil {
				return err
			}
			defer lock.Unlock()

			// Use comment-preserving set (uses AST manipulation)
			if err := setValuePreservingComments(file, key, typedValue); err != nil {
				return fmt.Errorf("failed to set value: %w", err)
			}

			// Skip success output if quiet
			if quiet {
				return nil
			}

			// Output
			if GlobalAXIOutput {
				return output.EncodeAXI(cmd.OutOrStdout(), map[string]interface{}{
					"key": key, "value": value, "status": "set",
				})
			}
			if jsonOutput {
				if minOutput {
					// --json --min: minimal JSON
					output := map[string]interface{}{"k": key, "s": "set"}
					jsonBytes, _ := json.Marshal(output)
					fmt.Fprintln(cmd.OutOrStdout(), string(jsonBytes))
				} else {
					// --json: full JSON
					output := map[string]interface{}{"key": key, "value": value, "status": "set"}
					jsonBytes, _ := json.Marshal(output)
					fmt.Fprintln(cmd.OutOrStdout(), string(jsonBytes))
				}
			} else if minOutput {
				fmt.Fprintln(cmd.OutOrStdout(), key)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "SET: %s = %s\n", key, value)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&file, "file", "", "Path to YAML config file (required)")
	cmd.Flags().BoolVar(&create, "create", false, "Create file if it doesn't exist")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&minOutput, "min", false, "Minimal output")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview changes without writing to file")
	cmd.Flags().BoolVar(&quiet, "quiet", false, "Suppress success messages (errors still output)")
	cmd.MarkFlagRequired("file")

	return cmd
}

// newYamlMultigetCmd creates the yaml multiget subcommand
func newYamlMultigetCmd() *cobra.Command {
	var file string
	var defaults string
	var separator string
	var jsonOutput bool
	var minOutput bool
	var requiredFile string

	cmd := &cobra.Command{
		Use:   "multiget [KEY1 KEY2 ...]",
		Short: "Retrieve multiple values",
		Long: `Retrieve multiple values from the YAML config file in a single operation.

Values are returned in argument order.

Keys can be specified via:
  - Positional arguments
  - --required-file (one key per line, # comments supported)

Output Formats:
  default: key=value (one per line)
  --json:  {"key1": "value1", "key2": "value2"}
  --min:   value1\nvalue2 (values only)

Examples:
  yaml multiget --file config.yaml helper.llm project.type
  yaml multiget --file config.yaml helper.llm missing.key --defaults '{"missing.key": "default"}'
  yaml multiget --file config.yaml helper.llm project.type --min
  yaml multiget --file config.yaml --required-file keys.txt`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if file == "" {
				return fmt.Errorf("--file flag is required")
			}

			// Collect keys from all sources
			var keys []string

			// From positional args
			keys = append(keys, args...)

			// From --required-file
			if requiredFile != "" {
				fileKeys, err := parseRequiredKeysFile(requiredFile)
				if err != nil {
					return fmt.Errorf("failed to read required keys file: %w", err)
				}
				keys = append(keys, fileKeys...)
			}

			// Deduplicate keys while preserving order
			keys = yamlUniqueStrings(keys)

			if len(keys) == 0 {
				return fmt.Errorf("no keys specified (use positional args or --required-file)")
			}

			// Parse defaults if provided
			var defaultsMap map[string]string
			if defaults != "" {
				if err := json.Unmarshal([]byte(defaults), &defaultsMap); err != nil {
					return fmt.Errorf("invalid --defaults JSON: %w", err)
				}
			}

			// Acquire read lock
			lock, err := yamlFileLock(file, false)
			if err != nil {
				return err
			}
			defer lock.Unlock()

			// Read file
			data, err := readYAMLAsMap(file)
			if err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("config file not found: %s", file)
				}
				return fmt.Errorf("failed to read config file: %w", err)
			}

			// Get all values
			results := make(map[string]string)
			var orderedKeys []string
			for _, key := range keys {
				orderedKeys = append(orderedKeys, key)
				value, found := getValueAtPath(data, key)
				if found {
					results[key] = formatValue(value)
				} else if defaultsMap != nil {
					if def, ok := defaultsMap[key]; ok {
						results[key] = def
					} else {
						return fmt.Errorf("key not found: %s", key)
					}
				} else {
					return fmt.Errorf("key not found: %s", key)
				}
			}

			// Output
			if GlobalAXIOutput {
				return output.EncodeAXI(cmd.OutOrStdout(), results)
			}
			if jsonOutput {
				if minOutput {
					// --json --min: values as JSON array (preserves order)
					orderedValues := make([]string, len(orderedKeys))
					for i, key := range orderedKeys {
						orderedValues[i] = results[key]
					}
					jsonBytes, _ := json.Marshal(orderedValues)
					fmt.Fprintln(cmd.OutOrStdout(), string(jsonBytes))
				} else {
					// --json: full JSON object
					jsonBytes, _ := json.Marshal(results)
					fmt.Fprintln(cmd.OutOrStdout(), string(jsonBytes))
				}
			} else if minOutput {
				for _, key := range orderedKeys {
					fmt.Fprintln(cmd.OutOrStdout(), results[key])
				}
			} else {
				for _, key := range orderedKeys {
					fmt.Fprintf(cmd.OutOrStdout(), "%s=%s\n", key, results[key])
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&file, "file", "", "Path to YAML config file (required)")
	cmd.Flags().StringVar(&defaults, "defaults", "", "JSON object of default values")
	cmd.Flags().StringVar(&separator, "separator", "\n", "Value separator for --min output")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&minOutput, "min", false, "Minimal output (values only)")
	cmd.Flags().StringVar(&requiredFile, "required-file", "", "File containing required keys (one per line, # comments supported)")
	cmd.MarkFlagRequired("file")

	return cmd
}

// newYamlMultisetCmd creates the yaml multiset subcommand
func newYamlMultisetCmd() *cobra.Command {
	var file string
	var create bool
	var jsonOutput bool
	var minOutput bool
	var dryRun bool
	var quiet bool

	cmd := &cobra.Command{
		Use:   "multiset KEY1 VALUE1 [KEY2 VALUE2 ...]",
		Short: "Store multiple values atomically",
		Long: `Store multiple key-value pairs in the YAML config file in a single atomic operation.

All keys are validated before any writes occur.
Arguments must be in KEY VALUE pairs.
Use --dry-run to preview changes without writing to the file.
Use --quiet to suppress success messages (errors still output).

Examples:
  yaml multiset --file config.yaml helper.llm claude helper.max_lines 2500
  yaml multiset --file config.yaml --create new.key value1 other.key value2
  yaml multiset --file config.yaml key1 val1 key2 val2 --dry-run`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if file == "" {
				return fmt.Errorf("--file flag is required")
			}

			// Validate even number of arguments
			if len(args)%2 != 0 {
				return fmt.Errorf("multiset requires KEY VALUE pairs (got %d arguments)", len(args))
			}

			// Parse key-value pairs
			pairs := make([]struct{ key, value string }, len(args)/2)
			for i := 0; i < len(args); i += 2 {
				pairs[i/2] = struct{ key, value string }{
					key:   args[i],
					value: args[i+1],
				}
			}

			// Check if file exists
			if _, err := os.Stat(file); os.IsNotExist(err) {
				if !create {
					return fmt.Errorf("config file not found: %s (hint: use --create)", file)
				}
				// Create empty file (unless dry-run)
				if !dryRun {
					if err := os.WriteFile(file, []byte(""), 0644); err != nil {
						return fmt.Errorf("failed to create config file: %w", err)
					}
				}
			}

			// Handle dry-run mode - preview without writing
			if dryRun {
				var data map[string]interface{}
				if _, statErr := os.Stat(file); statErr == nil {
					// Acquire read lock to prevent race condition with concurrent writes
					lock, lockErr := yamlFileLock(file, false) // false = read lock
					if lockErr != nil {
						return fmt.Errorf("failed to acquire read lock for dry-run: %w", lockErr)
					}
					var readErr error
					data, readErr = readYAMLAsMap(file)
					lock.Unlock()
					if readErr != nil {
						return fmt.Errorf("failed to read config file: %w", readErr)
					}
				} else {
					data = make(map[string]interface{})
				}

				var changes []dryRunChange
				for _, pair := range pairs {
					var typedValue interface{} = pair.value
					if num, parseErr := parseNumber(pair.value); parseErr == nil {
						typedValue = num
					}

					oldValue, _ := getValueAtPath(data, pair.key)
					changes = append(changes, dryRunChange{
						Key:      pair.key,
						OldValue: oldValue,
						NewValue: typedValue,
					})
				}
				return outputMultiDryRunPreview(cmd, file, changes, jsonOutput, minOutput)
			}

			// Read file for actual update
			var data map[string]interface{}
			var err error
			if _, statErr := os.Stat(file); statErr == nil {
				data, err = readYAMLAsMap(file)
				if err != nil {
					return fmt.Errorf("failed to read config file: %w", err)
				}
			} else {
				data = make(map[string]interface{})
			}

			// Acquire write lock
			lock, err := yamlFileLock(file, true)
			if err != nil {
				return err
			}
			defer lock.Unlock()

			// Type the values once; both paths below need them.
			var keys []string
			var values []interface{}
			for _, pair := range pairs {
				var typedValue interface{} = pair.value
				if num, parseErr := parseNumber(pair.value); parseErr == nil {
					typedValue = num
				}
				keys = append(keys, pair.key)
				values = append(values, typedValue)
			}

			// Edit the syntax tree so the file keeps its comments and its key
			// order. This reports handled=false without changing anything when
			// a key cannot be expressed that way, leaving the batch intact for
			// the map path below, which rewrites the whole file.
			handled, astErr := setValuesPreservingComments(file, keys, values)
			if astErr != nil {
				return fmt.Errorf("failed to write config file: %w", astErr)
			}

			if !handled {
				for i, key := range keys {
					if err := setValueAtPath(data, key, values[i]); err != nil {
						return fmt.Errorf("failed to set %s: %w", key, err)
					}
				}

				// Write file atomically
				if err := writeYAMLFile(file, data); err != nil {
					return fmt.Errorf("failed to write config file: %w", err)
				}
			}

			// Skip success output if quiet
			if quiet {
				return nil
			}

			// Output
			// NOTE: Output format intentionally matches context_multiset for consistency
			if GlobalAXIOutput {
				return output.EncodeAXI(cmd.OutOrStdout(), map[string]interface{}{
					"keys": keys, "count": len(keys), "status": "ok",
				})
			}
			if jsonOutput {
				if minOutput {
					// --json --min: minimal JSON with count and status
					output := map[string]interface{}{"count": len(keys), "status": "ok"}
					jsonBytes, _ := json.Marshal(output)
					fmt.Fprintln(cmd.OutOrStdout(), string(jsonBytes))
				} else {
					// --json: full JSON with keys, count, and status
					output := map[string]interface{}{"keys": keys, "count": len(keys), "status": "ok"}
					jsonBytes, _ := json.Marshal(output)
					fmt.Fprintln(cmd.OutOrStdout(), string(jsonBytes))
				}
			} else if minOutput {
				fmt.Fprintln(cmd.OutOrStdout(), strings.Join(keys, ","))
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "SET: %s\n", strings.Join(keys, ", "))
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&file, "file", "", "Path to YAML config file (required)")
	cmd.Flags().BoolVar(&create, "create", false, "Create file if it doesn't exist")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&minOutput, "min", false, "Minimal output")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview changes without writing to file")
	cmd.Flags().BoolVar(&quiet, "quiet", false, "Suppress success messages (errors still output)")
	cmd.MarkFlagRequired("file")

	return cmd
}

// formatValue converts a value to string representation
func formatValue(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}

// parseNumber attempts to parse a string as int or float.
//
// Coercion is only applied when the parsed numeric value round-trips back to
// the exact input string. This prevents silent truncation of version-like
// strings (e.g. "4.30" -> float 4.3 -> emitted as "4.3") and similar values
// where the input carries information that the numeric representation cannot.
func parseNumber(s string) (interface{}, error) {
	// Try int first
	var i int
	if _, err := fmt.Sscanf(s, "%d", &i); err == nil && fmt.Sprintf("%d", i) == s {
		return i, nil
	}

	// Try float, but only coerce if the float round-trips to the input string
	// through YAML marshaling. Values like "1.0", "4.30", "01.5" parse as
	// floats but lose their original representation when serialized, so we
	// keep them as strings.
	var f float64
	if _, err := fmt.Sscanf(s, "%f", &f); err == nil {
		marshaled, mErr := yaml.Marshal(f)
		if mErr == nil && strings.TrimSpace(string(marshaled)) == s {
			return f, nil
		}
	}

	return nil, fmt.Errorf("not a number")
}

func init() {
	RootCmd.AddCommand(newYamlCmd())
}
