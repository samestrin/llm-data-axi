package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// paramAliases maps canonical parameter names to their accepted aliases.
// This makes the MCP tools more forgiving when LLMs use alternative parameter names.
var paramAliases = map[string][]string{
	"path":     {"target", "file", "input", "dir", "directory", "file_path"},
	"file":     {"path", "input"},
	"manifest": {"path", "file", "package"},
	"pattern":  {"regex", "search"},
	"context":  {"description"},
}

// normalizeArgs converts aliased parameter names to their canonical forms.
// It returns a new map with normalized keys while preserving original values.
func normalizeArgs(args map[string]interface{}) map[string]interface{} {
	if args == nil {
		return args
	}

	// Build set of canonical names (these should never be remapped)
	canonicalNames := make(map[string]bool)
	for canonical := range paramAliases {
		canonicalNames[canonical] = true
	}

	// Build reverse lookup: alias -> canonical
	// Skip aliases that are themselves canonical names to avoid conflicts
	aliasToCanonical := make(map[string]string)
	for canonical, aliases := range paramAliases {
		for _, alias := range aliases {
			// Don't add if alias is itself a canonical name (avoid conflicts)
			if !canonicalNames[alias] {
				aliasToCanonical[alias] = canonical
			}
		}
	}

	normalized := make(map[string]interface{})
	for key, value := range args {
		// Check if this key should be normalized
		if canonical, isAlias := aliasToCanonical[key]; isAlias {
			// Only normalize if canonical key not already present in input
			if _, hasCanonical := args[canonical]; !hasCanonical {
				normalized[canonical] = value
				continue
			}
		}
		normalized[key] = value
	}

	return normalized
}

// BinaryPath is the path to the llm-data binary.
//
// Resolved from PATH at startup, falling back to the standard install location.
// llm-tools hardcoded "/usr/local/bin/llm-support" with no lookup, so the
// server refused to start for anyone who installed the CLI anywhere else — and
// the repo's own end-to-end tests could not run without installing first.
// LLM_DATA_BINARY overrides both, which is what lets CI point at ./build.
var BinaryPath = resolveBinaryPath()

func resolveBinaryPath() string {
	if override := os.Getenv("LLM_DATA_BINARY"); override != "" {
		return override
	}
	if resolved, err := exec.LookPath("llm-data"); err == nil {
		return resolved
	}
	return "/usr/local/bin/llm-data"
}

// CommandTimeout is the default timeout for command execution
var CommandTimeout = 60 * time.Second

// LLMTimeout is the timeout for commands that call an external LLM API, which
// can take far longer than a local operation.
var LLMTimeout = 300 * time.Second // 5 minutes

// llmCommands lists the commands that reach pkg/llmapi and therefore need
// LLMTimeout rather than CommandTimeout.
//
// llm-tools' copy of this map omitted both of these, so the two commands that
// actually make network LLM calls were the two capped at 60 seconds.
// extract_links only calls out under --context, but a map cannot express that,
// and over-granting the timeout is the harmless direction.
var llmCommands = map[string]bool{
	"extract_relevant": true,
	"extract_links":    true,
}

// Debug enables verbose logging for debugging MCP tool execution
var Debug = os.Getenv("LLM_DATA_MCP_DEBUG") == "true"

// AXIFormat selects TOON output for every command this server runs.
//
// llm-tools appended "--json --min" unconditionally and no handler ever passed
// --axi, so the CLI's AXI output was unreachable through MCP no matter what the
// binary supported. The default stays --json --min, because that is what the
// registered MCP clients parse today; set LLM_DATA_MCP_AXI=true, or pass
// "format": "axi" on a single call, to get TOON instead.
var AXIFormat = os.Getenv("LLM_DATA_MCP_AXI") == "true"

// wantsAXI reports whether this call should ask the CLI for TOON.
// A per-call "format" argument wins over the server-wide default.
func wantsAXI(args map[string]interface{}) bool {
	if f, ok := args["format"].(string); ok && f != "" {
		return strings.EqualFold(f, "axi") || strings.EqualFold(f, "toon")
	}
	return AXIFormat
}

// ExecuteHandler executes the appropriate command for a tool
func ExecuteHandler(toolName string, args map[string]interface{}) (string, error) {
	// Strip prefix
	cmdName := strings.TrimPrefix(toolName, ToolPrefix)

	axi := wantsAXI(args)

	// Build command args
	cmdArgs, err := buildArgs(cmdName, args)
	if err != nil {
		return "", err
	}

	// Ask for a machine-parseable, token-optimized encoding. Exactly one form
	// is sent. Sending both would in fact work — Formatter checks AXI first, so
	// --axi wins over --json — but relying on that makes the encoding depend on
	// a precedence rule rather than on what was asked for.
	if axi {
		cmdArgs = append(cmdArgs, "--axi")
	} else {
		cmdArgs = append(cmdArgs, "--json", "--min")
	}

	// Determine timeout based on command type
	timeout := CommandTimeout
	if llmCommands[cmdName] {
		timeout = LLMTimeout
	}

	// Execute command with appropriate timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if Debug {
		fmt.Fprintln(os.Stderr, "[LLM_DATA_MCP] Command:", BinaryPath, strings.Join(cmdArgs, " "))
		fmt.Fprintf(os.Stderr, "[LLM_DATA_MCP] Timeout: %v\n", timeout)
	}

	cmd := exec.CommandContext(ctx, BinaryPath, cmdArgs...)
	output, err := cmd.CombinedOutput()

	if ctx.Err() == context.DeadlineExceeded {
		// Return captured output on timeout - it may contain partial results or error messages
		errMsg := fmt.Errorf("command timed out after %v (captured %d bytes)", timeout, len(output))
		if Debug {
			fmt.Fprintf(os.Stderr, "[LLM_DATA_MCP] Timeout error (captured output): %q\n", string(output))
		}
		return string(output), errMsg
	}

	if err != nil {
		// Return output even on error (may contain useful error message)
		if len(output) > 0 {
			if Debug {
				fmt.Fprintf(os.Stderr, "[LLM_DATA_MCP] Command failed with output: %q\n", string(output))
			}
			return string(output), nil
		}
		return "", fmt.Errorf("command failed: %w", err)
	}

	return string(output), nil
}

// buildArgs builds CLI arguments for the given tool
func buildArgs(cmdName string, args map[string]interface{}) ([]string, error) {
	// Normalize parameter aliases before processing
	args = normalizeArgs(args)

	switch cmdName {
	case "tree":
		return buildTreeArgs(args), nil
	case "grep":
		return buildGrepArgs(args), nil
	case "multiexists":
		return buildMultiexistsArgs(args), nil
	case "template":
		return buildTemplateArgs(args), nil
	case "discover_tests":
		return buildDiscoverTestsArgs(args), nil
	case "multigrep":
		return buildMultigrepArgs(args), nil
	case "detect":
		return buildDetectArgs(args), nil
	case "project_components":
		return buildProjectComponentsArgs(args), nil
	case "count":
		return buildCountArgs(args), nil
	case "git_context":
		return buildGitContextArgs(args), nil
	case "repo_root":
		return buildRepoRootArgs(args), nil
	case "extract_relevant":
		return buildExtractRelevantArgs(args), nil
	case "extract_links":
		return buildExtractLinksArgs(args), nil
	case "highest":
		return buildHighestArgs(args), nil
	case "plan_type":
		return buildPlanTypeArgs(args), nil
	case "git_changes":
		return buildGitChangesArgs(args), nil
	case "yaml_get":
		return buildYamlGetArgs(args), nil
	case "yaml_set":
		return buildYamlSetArgs(args), nil
	case "yaml_multiget":
		return buildYamlMultigetArgs(args), nil
	case "yaml_multiset":
		return buildYamlMultisetArgs(args), nil
	case "catfiles":
		return buildCatfilesArgs(args), nil
	case "math":
		return buildMathArgs(args), nil
	case "stats":
		return buildStatsArgs(args), nil
	case "runtime":
		return buildRuntimeArgs(args), nil
	case "diff_smell":
		return buildDiffSmellArgs(args), nil
	case "tier_classifier":
		return buildTierClassifierArgs(args), nil
	default:
		return nil, fmt.Errorf("unknown command: %s", cmdName)
	}
}

func buildTreeArgs(args map[string]interface{}) []string {
	cmdArgs := []string{"tree"}
	if path, ok := args["path"].(string); ok {
		cmdArgs = append(cmdArgs, "--path", path)
	}
	if depth, ok := getInt(args, "depth"); ok {
		cmdArgs = append(cmdArgs, "--depth", strconv.Itoa(depth))
	}
	if maxEntries, ok := getInt(args, "max_entries"); ok {
		cmdArgs = append(cmdArgs, "--max-entries", strconv.Itoa(maxEntries))
	}
	if getBool(args, "sizes") {
		cmdArgs = append(cmdArgs, "--sizes")
	}
	if excludes, ok := args["exclude"].([]interface{}); ok {
		for _, e := range excludes {
			if s, ok := e.(string); ok {
				cmdArgs = append(cmdArgs, "--exclude", s)
			}
		}
	}
	if getBool(args, "no_gitignore") {
		cmdArgs = append(cmdArgs, "--no-gitignore")
	}
	if getBool(args, "no_default_excludes") {
		cmdArgs = append(cmdArgs, "--no-default-excludes")
	}
	return cmdArgs
}

func buildGrepArgs(args map[string]interface{}) []string {
	cmdArgs := []string{"grep"}
	if pattern, ok := args["pattern"].(string); ok {
		cmdArgs = append(cmdArgs, pattern)
	}
	if paths, ok := args["paths"].([]interface{}); ok {
		for _, p := range paths {
			if s, ok := p.(string); ok {
				cmdArgs = append(cmdArgs, s)
			}
		}
	}
	if getBool(args, "ignore_case") {
		cmdArgs = append(cmdArgs, "-i")
	}
	if getBool(args, "line_numbers") {
		cmdArgs = append(cmdArgs, "-n")
	}
	if getBool(args, "files_only") {
		cmdArgs = append(cmdArgs, "-l")
	}
	return cmdArgs
}

func buildMultiexistsArgs(args map[string]interface{}) []string {
	cmdArgs := []string{"multiexists"}
	if paths, ok := args["paths"].([]interface{}); ok {
		for _, p := range paths {
			if s, ok := p.(string); ok {
				cmdArgs = append(cmdArgs, s)
			}
		}
	}
	if getBool(args, "verbose") {
		cmdArgs = append(cmdArgs, "--verbose")
	}
	cmdArgs = append(cmdArgs, "--no-fail")
	if getBoolDefault(args, "json", true) {
		cmdArgs = append(cmdArgs, "--json")
	}
	if getBoolDefault(args, "min", true) {
		cmdArgs = append(cmdArgs, "--min")
	}
	return cmdArgs
}

func buildTemplateArgs(args map[string]interface{}) []string {
	// Note: The template command requires a positional file path, NOT a --file flag
	// The template file is passed as the first positional argument
	templateFile := ""
	if file, ok := args["file"].(string); ok {
		templateFile = file
	}
	// Also check for template param alias
	if file, ok := args["template"].(string); ok && templateFile == "" {
		templateFile = file
	}

	if templateFile == "" {
		// No file provided - return minimal args which will cause error
		return []string{"template"}
	}

	// Build args: template command needs file as positional arg FIRST
	cmdArgs := []string{"template", templateFile}

	if vars, ok := args["vars"].(map[string]interface{}); ok {
		for k, v := range vars {
			cmdArgs = append(cmdArgs, "--var", fmt.Sprintf("%s=%v", k, v))
		}
	}
	// Default to brackets syntax ([[var]]) to avoid conflicts with LLM template syntaxes
	// (Claude uses $var, Qwen/others use {{var}})
	syntax := "brackets"
	if s, ok := args["syntax"].(string); ok {
		syntax = s
	}
	cmdArgs = append(cmdArgs, "--syntax", syntax)
	if getBoolDefault(args, "json", true) {
		cmdArgs = append(cmdArgs, "--json")
	}
	if getBoolDefault(args, "min", true) {
		cmdArgs = append(cmdArgs, "--min")
	}
	return cmdArgs
}

func buildDiscoverTestsArgs(args map[string]interface{}) []string {
	cmdArgs := []string{"discover-tests"}
	if path, ok := args["path"].(string); ok {
		cmdArgs = append(cmdArgs, "--path", path)
	}
	if getBoolDefault(args, "json", true) {
		cmdArgs = append(cmdArgs, "--json")
	}
	// NOTE: --min is intentionally NOT used here. This command documents specific
	// output fields (PATTERN, FRAMEWORK, TEST_RUNNER, etc.) that must always be
	// present in JSON output for reliable parsing. Using --min would omit empty fields.
	return cmdArgs
}

func buildMultigrepArgs(args map[string]interface{}) []string {
	cmdArgs := []string{"multigrep"}
	if keywords, ok := args["keywords"].(string); ok {
		cmdArgs = append(cmdArgs, "--keywords", keywords)
	}
	if path, ok := args["path"].(string); ok {
		cmdArgs = append(cmdArgs, "--path", path)
	}
	if ext, ok := args["extensions"].(string); ok {
		cmdArgs = append(cmdArgs, "--extensions", ext)
	}
	if max, ok := getInt(args, "max_per_keyword"); ok {
		cmdArgs = append(cmdArgs, "--max-per-keyword", strconv.Itoa(max))
	}
	if getBool(args, "ignore_case") {
		cmdArgs = append(cmdArgs, "--ignore-case")
	}
	if getBool(args, "definitions_only") {
		cmdArgs = append(cmdArgs, "--definitions-only")
	}
	if getBoolDefault(args, "json", true) {
		cmdArgs = append(cmdArgs, "--json")
	}
	if getBoolDefault(args, "min", true) {
		cmdArgs = append(cmdArgs, "--min")
	}
	if dir, ok := args["output_dir"].(string); ok {
		cmdArgs = append(cmdArgs, "--output-dir", dir)
	}
	return cmdArgs
}

func buildDetectArgs(args map[string]interface{}) []string {
	cmdArgs := []string{"detect"}
	if path, ok := args["path"].(string); ok {
		cmdArgs = append(cmdArgs, "--path", path)
	}
	if dirs, ok := args["dirs"].(string); ok && dirs != "" {
		cmdArgs = append(cmdArgs, "--dirs", dirs)
	}
	if getBoolDefault(args, "json", true) {
		cmdArgs = append(cmdArgs, "--json")
	}
	// NOTE: --min is intentionally NOT used here. This command documents specific
	// output fields (STACK, LANGUAGE, PACKAGE_MANAGER, etc.) that must always be
	// present in JSON output for reliable parsing. Using --min would omit empty fields.
	return cmdArgs
}

func buildProjectComponentsArgs(args map[string]interface{}) []string {
	cmdArgs := []string{"project-components"}
	if file, ok := args["file"].(string); ok {
		cmdArgs = append(cmdArgs, "--file", file)
	}
	if getBoolDefault(args, "json", true) {
		cmdArgs = append(cmdArgs, "--json")
	}
	if getBool(args, "min") {
		cmdArgs = append(cmdArgs, "--min")
	}
	return cmdArgs
}

func buildCountArgs(args map[string]interface{}) []string {
	cmdArgs := []string{"count"}
	if mode, ok := args["mode"].(string); ok {
		cmdArgs = append(cmdArgs, "--mode", mode)
	}
	if path, ok := args["path"].(string); ok {
		cmdArgs = append(cmdArgs, "--path", path)
	}
	if getBool(args, "recursive") {
		cmdArgs = append(cmdArgs, "--recursive")
	}
	if pattern, ok := args["pattern"].(string); ok {
		cmdArgs = append(cmdArgs, "--pattern", pattern)
	}
	if getBoolDefault(args, "json", true) {
		cmdArgs = append(cmdArgs, "--json")
	}
	if getBoolDefault(args, "min", true) {
		cmdArgs = append(cmdArgs, "--min")
	}
	return cmdArgs
}

func buildGitContextArgs(args map[string]interface{}) []string {
	cmdArgs := []string{"git-context"}
	if path, ok := args["path"].(string); ok {
		cmdArgs = append(cmdArgs, "--path", path)
	}
	if getBool(args, "include_diff") {
		cmdArgs = append(cmdArgs, "--include-diff")
	}
	if since, ok := args["since"].(string); ok {
		cmdArgs = append(cmdArgs, "--since", since)
	}
	if max, ok := getInt(args, "max_commits"); ok {
		cmdArgs = append(cmdArgs, "--max-commits", strconv.Itoa(max))
	}
	if getBoolDefault(args, "json", true) {
		cmdArgs = append(cmdArgs, "--json")
	}
	if getBoolDefault(args, "min", true) {
		cmdArgs = append(cmdArgs, "--min")
	}
	return cmdArgs
}

func buildRepoRootArgs(args map[string]interface{}) []string {
	cmdArgs := []string{"repo-root"}
	if path, ok := args["path"].(string); ok {
		cmdArgs = append(cmdArgs, "--path", path)
	}
	if getBool(args, "validate") {
		cmdArgs = append(cmdArgs, "--validate")
	}
	return cmdArgs
}

func buildExtractRelevantArgs(args map[string]interface{}) []string {
	cmdArgs := []string{"extract-relevant"}
	if path, ok := args["path"].(string); ok {
		cmdArgs = append(cmdArgs, "--path", path)
	}
	if ctx, ok := args["context"].(string); ok {
		cmdArgs = append(cmdArgs, "--context", ctx)
	}
	if conc, ok := getInt(args, "concurrency"); ok {
		cmdArgs = append(cmdArgs, "--concurrency", strconv.Itoa(conc))
	}
	if output, ok := args["output"].(string); ok {
		cmdArgs = append(cmdArgs, "--output", output)
	}
	if timeout, ok := getInt(args, "timeout"); ok {
		cmdArgs = append(cmdArgs, "--timeout", strconv.Itoa(timeout))
	}
	if getBoolDefault(args, "json", true) {
		cmdArgs = append(cmdArgs, "--json")
	}
	if getBoolDefault(args, "min", true) {
		cmdArgs = append(cmdArgs, "--min")
	}
	return cmdArgs
}

func buildExtractLinksArgs(args map[string]interface{}) []string {
	cmdArgs := []string{"extract-links"}
	if url, ok := args["url"].(string); ok {
		cmdArgs = append(cmdArgs, "--url", url)
	}
	if context, ok := args["context"].(string); ok && context != "" {
		cmdArgs = append(cmdArgs, "--context", context)
	}
	if timeout, ok := getInt(args, "timeout"); ok {
		cmdArgs = append(cmdArgs, "--timeout", strconv.Itoa(timeout))
	}
	if getBoolDefault(args, "json", true) {
		cmdArgs = append(cmdArgs, "--json")
	}
	if getBoolDefault(args, "min", true) {
		cmdArgs = append(cmdArgs, "--min")
	}
	return cmdArgs
}

func buildHighestArgs(args map[string]interface{}) []string {
	cmdArgs := []string{"highest"}
	if path, ok := args["path"].(string); ok {
		cmdArgs = append(cmdArgs, "--path", path)
	}
	// Handle paths array - takes precedence over path if both specified
	if paths, ok := args["paths"].([]interface{}); ok && len(paths) > 0 {
		var pathStrs []string
		for _, p := range paths {
			if ps, ok := p.(string); ok {
				pathStrs = append(pathStrs, ps)
			}
		}
		if len(pathStrs) > 0 {
			cmdArgs = append(cmdArgs, "--paths", strings.Join(pathStrs, ","))
		}
	}
	if pattern, ok := args["pattern"].(string); ok {
		cmdArgs = append(cmdArgs, "--pattern", pattern)
	}
	if t, ok := args["type"].(string); ok {
		cmdArgs = append(cmdArgs, "--type", t)
	}
	if prefix, ok := args["prefix"].(string); ok {
		cmdArgs = append(cmdArgs, "--prefix", prefix)
	}
	if getBoolDefault(args, "json", true) {
		cmdArgs = append(cmdArgs, "--json")
	}
	if getBoolDefault(args, "min", true) {
		cmdArgs = append(cmdArgs, "--min")
	}
	return cmdArgs
}

func buildPlanTypeArgs(args map[string]interface{}) []string {
	cmdArgs := []string{"plan-type"}
	if path, ok := args["path"].(string); ok {
		cmdArgs = append(cmdArgs, "--path", path)
	}
	if getBoolDefault(args, "json", true) {
		cmdArgs = append(cmdArgs, "--json")
	}
	if getBoolDefault(args, "min", true) {
		cmdArgs = append(cmdArgs, "--min")
	}
	return cmdArgs
}

func buildGitChangesArgs(args map[string]interface{}) []string {
	cmdArgs := []string{"git-changes"}
	if path, ok := args["path"].(string); ok {
		cmdArgs = append(cmdArgs, "--path", path)
	}
	// Handle include_untracked - default is true, only add flag if explicitly false
	if v, ok := args["include_untracked"].(bool); ok && !v {
		cmdArgs = append(cmdArgs, "--include-untracked=false")
	}
	if getBool(args, "staged_only") {
		cmdArgs = append(cmdArgs, "--staged-only")
	}
	if getBoolDefault(args, "json", true) {
		cmdArgs = append(cmdArgs, "--json")
	}
	if getBoolDefault(args, "min", true) {
		cmdArgs = append(cmdArgs, "--min")
	}
	return cmdArgs
}

// Helper functions

func getBool(args map[string]interface{}, key string) bool {
	if v, ok := args[key].(bool); ok {
		return v
	}
	return false
}

// getBoolDefault returns the bool value for key, or defaultVal if not set.
// Used for json/min flags that should default to true in MCP context.
func getBoolDefault(args map[string]interface{}, key string, defaultVal bool) bool {
	if v, ok := args[key].(bool); ok {
		return v
	}
	return defaultVal
}

func getInt(args map[string]interface{}, key string) (int, bool) {
	switch v := args[key].(type) {
	case int:
		return v, true
	case float64:
		return int(v), true
	case int64:
		return int(v), true
	}
	return 0, false
}

func getInt64(args map[string]interface{}, key string) (int64, bool) {
	switch v := args[key].(type) {
	case int:
		return int64(v), true
	case float64:
		return int64(v), true
	case int64:
		return v, true
	}
	return 0, false
}

func getFloat(args map[string]interface{}, key string) (float64, bool) {
	switch v := args[key].(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	}
	return 0, false
}

func buildYamlGetArgs(args map[string]interface{}) []string {
	cmdArgs := []string{"yaml", "get"}

	// Add --file flag (required)
	if file, ok := args["file"].(string); ok {
		cmdArgs = append(cmdArgs, "--file", file)
	}

	// Add key as positional argument
	if key, ok := args["key"].(string); ok {
		cmdArgs = append(cmdArgs, key)
	}

	// Default value
	if def, ok := args["default"].(string); ok {
		cmdArgs = append(cmdArgs, "--default", def)
	}

	// Output format flags
	if getBoolDefault(args, "json", true) {
		cmdArgs = append(cmdArgs, "--json")
	}
	if getBoolDefault(args, "min", true) {
		cmdArgs = append(cmdArgs, "--min")
	}

	return cmdArgs
}

func buildYamlSetArgs(args map[string]interface{}) []string {
	cmdArgs := []string{"yaml", "set"}

	// Add --file flag (required)
	if file, ok := args["file"].(string); ok {
		cmdArgs = append(cmdArgs, "--file", file)
	}

	// Add key and value as positional arguments
	if key, ok := args["key"].(string); ok {
		cmdArgs = append(cmdArgs, key)
	}
	if value, ok := args["value"].(string); ok {
		cmdArgs = append(cmdArgs, value)
	}

	// Create flag
	if getBool(args, "create") {
		cmdArgs = append(cmdArgs, "--create")
	}

	// Dry-run flag
	if getBool(args, "dry_run") {
		cmdArgs = append(cmdArgs, "--dry-run")
	}

	// Quiet flag
	if getBool(args, "quiet") {
		cmdArgs = append(cmdArgs, "--quiet")
	}

	// Output format flags
	if getBoolDefault(args, "json", true) {
		cmdArgs = append(cmdArgs, "--json")
	}
	if getBoolDefault(args, "min", true) {
		cmdArgs = append(cmdArgs, "--min")
	}

	return cmdArgs
}

func buildYamlMultigetArgs(args map[string]interface{}) []string {
	cmdArgs := []string{"yaml", "multiget"}

	// Add --file flag (required)
	if file, ok := args["file"].(string); ok {
		cmdArgs = append(cmdArgs, "--file", file)
	}

	// Add keys as positional arguments
	if keys, ok := args["keys"].([]interface{}); ok {
		for _, k := range keys {
			if s, ok := k.(string); ok {
				cmdArgs = append(cmdArgs, s)
			}
		}
	}

	// Add defaults as JSON string to --defaults flag
	if defaults, ok := args["defaults"].(map[string]interface{}); ok && len(defaults) > 0 {
		// Convert map to JSON string for the CLI
		jsonBytes, err := json.Marshal(defaults)
		if err == nil {
			cmdArgs = append(cmdArgs, "--defaults", string(jsonBytes))
		}
	}

	// Required file flag
	if requiredFile, ok := args["required_file"].(string); ok && requiredFile != "" {
		cmdArgs = append(cmdArgs, "--required-file", requiredFile)
	}

	// Output format flags
	if getBoolDefault(args, "json", true) {
		cmdArgs = append(cmdArgs, "--json")
	}
	if getBoolDefault(args, "min", true) {
		cmdArgs = append(cmdArgs, "--min")
	}

	return cmdArgs
}

func buildYamlMultisetArgs(args map[string]interface{}) []string {
	cmdArgs := []string{"yaml", "multiset"}

	// Add --file flag (required)
	if file, ok := args["file"].(string); ok {
		cmdArgs = append(cmdArgs, "--file", file)
	}

	// Convert pairs object to KEY VALUE arguments
	if pairs, ok := args["pairs"].(map[string]interface{}); ok {
		for key, value := range pairs {
			cmdArgs = append(cmdArgs, key)
			if v, ok := value.(string); ok {
				cmdArgs = append(cmdArgs, v)
			} else {
				cmdArgs = append(cmdArgs, fmt.Sprintf("%v", value))
			}
		}
	}

	// Create flag
	if getBool(args, "create") {
		cmdArgs = append(cmdArgs, "--create")
	}

	// Dry-run flag
	if getBool(args, "dry_run") {
		cmdArgs = append(cmdArgs, "--dry-run")
	}

	// Quiet flag
	if getBool(args, "quiet") {
		cmdArgs = append(cmdArgs, "--quiet")
	}

	// Output format flags
	if getBoolDefault(args, "json", true) {
		cmdArgs = append(cmdArgs, "--json")
	}
	if getBoolDefault(args, "min", true) {
		cmdArgs = append(cmdArgs, "--min")
	}

	return cmdArgs
}

func buildCatfilesArgs(args map[string]interface{}) []string {
	cmdArgs := []string{"catfiles"}

	if paths, ok := args["paths"].([]interface{}); ok {
		for _, p := range paths {
			if s, ok := p.(string); ok {
				cmdArgs = append(cmdArgs, s)
			}
		}
	}
	if maxSize, ok := getInt(args, "max_size"); ok {
		cmdArgs = append(cmdArgs, "--max-size", strconv.Itoa(maxSize))
	}
	if getBool(args, "no_gitignore") {
		cmdArgs = append(cmdArgs, "--no-gitignore")
	}
	if getBoolDefault(args, "json", true) {
		cmdArgs = append(cmdArgs, "--json")
	}
	if getBoolDefault(args, "min", true) {
		cmdArgs = append(cmdArgs, "--min")
	}

	return cmdArgs
}

func buildMathArgs(args map[string]interface{}) []string {
	cmdArgs := []string{"math"}

	if expr, ok := args["expression"].(string); ok {
		cmdArgs = append(cmdArgs, expr)
	}

	return cmdArgs
}

func buildStatsArgs(args map[string]interface{}) []string {
	cmdArgs := []string{"stats"}

	if path, ok := args["path"].(string); ok {
		cmdArgs = append(cmdArgs, "--path", path)
	}
	if getBool(args, "no_gitignore") {
		cmdArgs = append(cmdArgs, "--no-gitignore")
	}
	if getBoolDefault(args, "json", true) {
		cmdArgs = append(cmdArgs, "--json")
	}
	if getBoolDefault(args, "min", true) {
		cmdArgs = append(cmdArgs, "--min")
	}

	return cmdArgs
}

func buildRuntimeArgs(args map[string]interface{}) []string {
	cmdArgs := []string{"runtime"}

	if start, ok := getInt64(args, "start"); ok {
		cmdArgs = append(cmdArgs, "--start", strconv.FormatInt(start, 10))
	}
	if end, ok := getInt64(args, "end"); ok {
		cmdArgs = append(cmdArgs, "--end", strconv.FormatInt(end, 10))
	}
	if format, ok := args["format"].(string); ok {
		cmdArgs = append(cmdArgs, "--format", format)
	}
	if precision, ok := getInt(args, "precision"); ok {
		cmdArgs = append(cmdArgs, "--precision", strconv.Itoa(precision))
	}
	if getBool(args, "label") {
		cmdArgs = append(cmdArgs, "--label")
	}
	if getBool(args, "raw") {
		cmdArgs = append(cmdArgs, "--raw")
	}
	if getBoolDefault(args, "json", true) {
		cmdArgs = append(cmdArgs, "--json")
	}
	if getBoolDefault(args, "min", true) {
		cmdArgs = append(cmdArgs, "--min")
	}

	return cmdArgs
}

func buildDiffSmellArgs(args map[string]interface{}) []string {
	cmdArgs := []string{"diff-smell"}
	if v, ok := args["diff"].(string); ok && v != "" {
		cmdArgs = append(cmdArgs, "--diff", v)
	}
	if v, ok := args["repo"].(string); ok && v != "" {
		cmdArgs = append(cmdArgs, "--repo", v)
	}
	if v, ok := args["rev"].(string); ok && v != "" {
		cmdArgs = append(cmdArgs, "--rev", v)
	}
	cmdArgs = append(cmdArgs, "--json")
	return cmdArgs
}

func buildTierClassifierArgs(args map[string]interface{}) []string {
	cmdArgs := []string{"tier-classifier"}
	if v, ok := args["packages"].(string); ok && v != "" {
		cmdArgs = append(cmdArgs, "--packages", v)
	}
	if v, ok := args["config"].(string); ok && v != "" {
		cmdArgs = append(cmdArgs, "--config", v)
	}
	cmdArgs = append(cmdArgs, "--json")
	return cmdArgs
}
