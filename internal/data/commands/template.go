package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/samestrin/llm-data-axi/v2/pkg/output"
	"github.com/spf13/cobra"
)

var (
	templateData    string
	templateVars    []string
	templateEnv     bool
	templateStrict  bool
	templateSyntax  string
	templateOutput  string
	templateStrip   bool
	templateJSON    bool
	templateMinimal bool
)

// TemplateResult holds the template processing result
type TemplateResult struct {
	OutputFile string `json:"output_file,omitempty"`
	OF         string `json:"of,omitempty"`
	Content    string `json:"content,omitempty"`
	C          string `json:"c,omitempty"`
	Status     string `json:"status,omitempty"`
	S          string `json:"s,omitempty"`
}

// newTemplateCmd creates the template command
func newTemplateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "template <file>",
		Short: "Variable substitution in templates",
		Long: `Perform variable substitution in template files.

Supports {{variable}} or [[variable]] syntax with optional defaults.
Example: {{name|Guest}} uses "Guest" if name is not defined.

--var KEY=@PATH reads the value from the file at PATH, any file the process
can read, and --env makes every environment variable a template variable.
Both are opt-in; pass them only with templates and paths you trust.

Examples:
  llm-data template greeting.txt --var name=Ada
  llm-data template prompt.md --syntax brackets --var body=@notes.txt
  llm-data template config.tmpl --data values.json --strict -o config.yaml`,
		Args: cobra.ExactArgs(1),
		RunE: runTemplate,
	}

	cmd.Flags().StringVar(&templateData, "data", "", "JSON file with variable values")
	cmd.Flags().StringArrayVar(&templateVars, "var", nil, "Variable in KEY=VALUE format")
	cmd.Flags().BoolVar(&templateEnv, "env", false, "Include environment variables")
	cmd.Flags().BoolVar(&templateStrict, "strict", false, "Error on undefined variables")
	cmd.Flags().StringVar(&templateSyntax, "syntax", "braces", "Syntax: braces ({{var}}) or brackets ([[var]])")
	cmd.Flags().StringVarP(&templateOutput, "output", "o", "", "Output file (default: stdout)")
	cmd.Flags().BoolVar(&templateStrip, "strip", false, "Strip whitespace from file values")
	cmd.Flags().BoolVar(&templateJSON, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&templateMinimal, "min", false, "Minimal --json or --plain output (no effect on TOON)")

	return cmd
}

func runTemplate(cmd *cobra.Command, args []string) error {
	// Read template
	var template string
	if args[0] == "-" {
		// Read from stdin (not implemented for simplicity)
		return fmt.Errorf("stdin not supported, use file path")
	}

	if err := requireRegularFile(args[0]); err != nil {
		return err
	}
	content, err := os.ReadFile(args[0])
	if err != nil {
		return fmt.Errorf("failed to read template: %w", err)
	}
	template = string(content)

	// Collect variables
	variables := make(map[string]string)

	// From JSON file (lowest priority)
	if templateData != "" {
		if err := requireRegularFile(templateData); err != nil {
			return err
		}
		data, err := os.ReadFile(templateData)
		if err != nil {
			return fmt.Errorf("failed to read data file: %w", err)
		}
		var jsonData map[string]interface{}
		if err := json.Unmarshal(data, &jsonData); err != nil {
			return fmt.Errorf("invalid JSON in data file: %w", err)
		}
		for k, v := range jsonData {
			variables[k] = fmt.Sprintf("%v", v)
		}
	}

	// From environment (medium priority)
	if templateEnv {
		for _, env := range os.Environ() {
			if idx := strings.Index(env, "="); idx != -1 {
				variables[env[:idx]] = env[idx+1:]
			}
		}
	}

	// From command line (highest priority)
	for _, v := range templateVars {
		if idx := strings.Index(v, "="); idx != -1 {
			key := v[:idx]
			value := v[idx+1:]

			// Check if value is a file reference (@path)
			if strings.HasPrefix(value, "@") {
				filePath := value[1:]
				if err := requireRegularFile(filePath); err != nil {
					return err
				}
				fileContent, err := os.ReadFile(filePath)
				if err != nil {
					return fmt.Errorf("failed to read file for variable %s: %w", key, err)
				}
				value = string(fileContent)
				if templateStrip {
					value = strings.TrimSpace(value)
				}
			}
			variables[key] = value
		}
	}

	// Undefined names under --strict, once each in order of appearance.
	var undefined []string
	seenUndefined := make(map[string]bool)

	// Replace function
	replaceVar := func(match string, varContent string) string {
		varName := varContent
		defaultValue := ""
		hasDefault := false

		// Check for default value syntax: var|default
		if idx := strings.Index(varContent, "|"); idx != -1 {
			varName = strings.TrimSpace(varContent[:idx])
			defaultValue = strings.TrimSpace(varContent[idx+1:])
			hasDefault = true
		} else {
			varName = strings.TrimSpace(varContent)
		}

		if val, ok := variables[varName]; ok {
			return val
		} else if hasDefault {
			return defaultValue
		} else if templateStrict && !seenUndefined[varName] {
			seenUndefined[varName] = true
			undefined = append(undefined, varName)
		}
		return match // Keep placeholder if not found
	}

	// Replace variables based on syntax
	var result string
	if templateSyntax == "brackets" {
		pattern := regexp.MustCompile(`\[\[([^\]]+)\]\]`)
		result = pattern.ReplaceAllStringFunc(template, func(match string) string {
			content := match[2 : len(match)-2] // Remove [[ and ]]
			return replaceVar(match, content)
		})
	} else {
		pattern := regexp.MustCompile(`\{\{([^}]+)\}\}`)
		result = pattern.ReplaceAllStringFunc(template, func(match string) string {
			content := match[2 : len(match)-2] // Remove {{ and }}
			return replaceVar(match, content)
		})
	}

	// --strict fails before anything is written: it printed an ERROR line on
	// stderr and then status: success with exit 0.
	if len(undefined) > 0 {
		return &dataError{
			msg:    "undefined variables with --strict: " + strings.Join(undefined, ", "),
			fields: map[string]any{"undefined": undefined},
		}
	}

	// Output
	if templateOutput != "" {
		if err := os.WriteFile(templateOutput, []byte(result), 0644); err != nil {
			return fmt.Errorf("failed to write output: %w", err)
		}

		// Build result struct
		var res TemplateResult
		if templateMinimal {
			res = TemplateResult{
				OF: templateOutput,
				S:  "success",
			}
		} else {
			res = TemplateResult{
				OutputFile: templateOutput,
				Status:     "success",
			}
		}

		formatter := output.New(templateJSON, templateMinimal, cmd.OutOrStdout())
		return formatter.Print(res, func(w io.Writer, data interface{}) {
			fmt.Fprintf(w, "✓ Template written to: %s\n", templateOutput)
		})
	}

	// Stdout output - use Formatter for TOON/JSON/minimal, otherwise raw
	// content. TOON is the default output, so only --plain reaches the raw
	// branch; before it was listed here, --axi printed raw text with exit 0.
	// --min does not select a format: listed here, --plain --min printed JSON.
	if GlobalAXIOutput || templateJSON {
		var res TemplateResult
		if templateMinimal {
			res = TemplateResult{
				C: result,
				S: "success",
			}
		} else {
			res = TemplateResult{
				Content: result,
				Status:  "success",
			}
		}
		formatter := output.New(templateJSON, templateMinimal, cmd.OutOrStdout())
		return formatter.Print(res, nil)
	}

	// Default: raw output
	fmt.Fprint(cmd.OutOrStdout(), result)
	return nil
}
