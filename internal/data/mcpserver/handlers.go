package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/samestrin/llm-data-axi/v2/internal/data/moved"
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
// Resolved at startup from LLM_DATA_BINARY, then PATH, then the directory of
// the running llm-data-mcp executable, falling back to the standard install
// location. llm-tools hardcoded "/usr/local/bin/llm-support" with no lookup,
// so the server refused to start for anyone who installed the CLI anywhere
// else — and the repo's own end-to-end tests could not run without installing
// first. The sibling step covers install.sh's default ~/.local/bin when that
// directory is not on PATH. LLM_DATA_BINARY overrides every step, which is
// what lets CI point at ./build.
var BinaryPath = resolveBinaryPath()

const (
	// binaryName is the CLI executable the server runs.
	binaryName = "llm-data"
	// FallbackBinaryPath is the standard install location, tried last. It is
	// exported so the startup error in cmd/llm-data-mcp can name it.
	FallbackBinaryPath = "/usr/local/bin/llm-data"
)

// osExecutable is indirected so tests can point the sibling lookup at a
// fixture directory; the running test binary is not llm-data-mcp.
var osExecutable = os.Executable

func resolveBinaryPath() string {
	if override := os.Getenv("LLM_DATA_BINARY"); override != "" {
		return override
	}
	if resolved, err := exec.LookPath(binaryName); err == nil {
		return resolved
	}
	if sibling := SiblingBinaryPath(); sibling != "" {
		if info, err := os.Stat(sibling); err == nil && usableBinaryFile(info) {
			return sibling
		}
	}
	return FallbackBinaryPath
}

// SiblingBinaryPath returns the llm-data candidate beside the running
// llm-data-mcp executable, or "" when the executable's location cannot be
// determined. It does not check that the file exists: the startup error names
// the location either way, so an operator can see where a sibling install
// would be found.
func SiblingBinaryPath() string {
	exe, err := osExecutable()
	if err != nil {
		return ""
	}
	name := binaryName
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(filepath.Dir(exe), name)
}

// CheckBinary reports why path is not an llm-data binary the server could
// exec, or nil. Startup used to check only that the path existed, so a
// directory or a file without the exec bit started the server and every tool
// call then failed.
func CheckBinary(path string) error {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return fmt.Errorf("llm-data binary not found at %s", path)
	}
	if err != nil {
		return fmt.Errorf("llm-data binary at %s: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("llm-data binary at %s is a directory", path)
	}
	if !usableBinaryFile(info) {
		return fmt.Errorf("llm-data binary at %s is not executable", path)
	}
	return nil
}

// usableBinaryFile reports whether info describes a file the server could
// exec. A sibling without the exec bit must not be reported as found: the
// server would start and every tool call would fail, which is a success
// report for a broken install. Windows has no exec bit; LookPath checks file
// extensions there instead.
func usableBinaryFile(info os.FileInfo) bool {
	if info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode()&0111 != 0
}

// CommandTimeout is the default timeout for command execution
var CommandTimeout = 60 * time.Second

// Debug enables verbose logging for debugging MCP tool execution
var Debug = os.Getenv("LLM_DATA_MCP_DEBUG") == "true"

// AXIFormat is the server-wide encoding: true asks the CLI for TOON, false for
// --json --min.
//
// llm-tools appended "--json --min" unconditionally and no handler ever passed
// --axi, so the CLI's AXI output was unreachable through MCP no matter what the
// binary supported. TOON is now the default. LLM_DATA_MCP_AXI=false restores
// --json --min for the whole server, and a per-call "format": "json" does the
// same for one call.
var AXIFormat = loadAXIDefault()

func loadAXIDefault() bool {
	axi, err := axiDefaultFromEnv(os.Getenv("LLM_DATA_MCP_AXI"))
	if err != nil {
		// stdout carries the protocol, so the warning goes to stderr.
		fmt.Fprintln(os.Stderr, "[LLM_DATA_MCP]", err)
	}
	return axi
}

// axiDefaultFromEnv reads LLM_DATA_MCP_AXI. Unset means TOON. A value that is
// not a boolean keeps TOON and returns an error, so it is reported rather than
// silently ignored.
func axiDefaultFromEnv(value string) (bool, error) {
	if value == "" {
		return true, nil
	}
	axi, err := strconv.ParseBool(value)
	if err != nil {
		return true, fmt.Errorf("LLM_DATA_MCP_AXI=%q is not true or false; using the default, TOON", value)
	}
	return axi, nil
}

// wantsAXI reports whether this call should ask the CLI for TOON.
// A per-call "format" argument wins over the server-wide default. A format
// the server does not know is an error: falling back to a default would hand a
// caller who asked for something else an encoding it did not ask for.
//
// runtime advertises its own "format" (secs, hms, human, ...), which is its
// time format, so for runtime the argument never selects the encoding.
func wantsAXI(cmdName string, args map[string]interface{}) (bool, error) {
	f, ok := args["format"].(string)
	if !ok || f == "" || cmdName == "runtime" {
		return AXIFormat, nil
	}
	switch strings.ToLower(f) {
	case "axi", "toon":
		return true, nil
	case "json":
		return false, nil
	}
	return false, fmt.Errorf("unknown format %q: want \"axi\" (or \"toon\") or \"json\"", f)
}

// withoutBuilderEncoding returns a copy of args that tells every builder not
// to add --json or --min, so the encoding is decided in one place. It works
// through the builders' own json/min switches rather than by filtering the
// built command line, because a user value can be spelled "--json" too: a
// grep pattern or a tree exclude must reach the CLI intact.
func withoutBuilderEncoding(args map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(args)+2)
	for k, v := range args {
		out[k] = v
	}
	out["json"] = false
	out["min"] = false
	return out
}

// ExecuteHandler executes the appropriate command for a tool
func ExecuteHandler(toolName string, args map[string]interface{}) (string, error) {
	// Strip prefix
	cmdName := strings.TrimPrefix(toolName, ToolPrefix)

	axi, err := wantsAXI(cmdName, args)
	if err != nil {
		return "", err
	}

	// Build command args
	cmdArgs, err := buildArgs(cmdName, withoutBuilderEncoding(args))
	if err != nil {
		return "", err
	}

	// Ask for a machine-parseable, token-optimized encoding. Exactly one form
	// is sent: the CLI rejects --axi with --json as a usage error.
	if axi {
		cmdArgs = withEncoding(cmdArgs, []string{"--axi"})
	} else {
		cmdArgs = withEncoding(cmdArgs, []string{"--json", "--min"})
	}

	// Determine timeout based on command type
	timeout := CommandTimeout

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
		// A non-zero exit is a failure even when it printed something: the
		// output used to come back with a nil error, so the server sent the
		// CLI's own error payload marked as a success. The output is still
		// returned, because the CLI prints its error there in every format.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if Debug {
				fmt.Fprintf(os.Stderr, "[LLM_DATA_MCP] Command failed with output: %q\n", string(output))
			}
			return string(output), &CommandFailedError{ExitCode: exitErr.ExitCode(), err: err}
		}
		return "", fmt.Errorf("command failed: %w", err)
	}

	return string(output), nil
}

// withEncoding inserts the encoding flags straight after the command words,
// ahead of the builder's own flags. They used to be appended at the end of the
// line, which is behind the "--" that ends option parsing, where the CLI would
// read them as positionals. Command words never start with "-", and every
// builder puts its flags and then "--" after them, so the first token that
// starts with "-" marks the end of the command words.
func withEncoding(cmdArgs, encoding []string) []string {
	at := len(cmdArgs)
	for i, a := range cmdArgs {
		if strings.HasPrefix(a, "-") {
			at = i
			break
		}
	}
	out := make([]string, 0, len(cmdArgs)+len(encoding))
	out = append(out, cmdArgs[:at]...)
	out = append(out, encoding...)
	return append(out, cmdArgs[at:]...)
}

// appendPositionals ends option parsing with "--" and then appends values
// built from tool input. Without the separator a value spelled like an option
// is read as one: the yaml_multiset pair {"--file=h.yaml": "--create"} once
// redirected a write to h.yaml, and a grep pattern of "-v" could not be
// searched for at all. Builders call it last, after every flag.
func appendPositionals(cmdArgs []string, values ...string) []string {
	if len(values) == 0 {
		return cmdArgs
	}
	return append(append(cmdArgs, "--"), values...)
}

// stringList returns the string elements of an array argument.
func stringList(args map[string]interface{}, key string) []string {
	var out []string
	if items, ok := args[key].([]interface{}); ok {
		for _, item := range items {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// CommandFailedError is a CLI run that exited non-zero. ExecuteHandler
// returns it together with the run's output, which is the CLI's error
// payload, so the caller can pass the payload on and still mark it an error.
type CommandFailedError struct {
	ExitCode int
	err      error
}

func (e *CommandFailedError) Error() string { return "command failed: " + e.err.Error() }
func (e *CommandFailedError) Unwrap() error { return e.err }

// commandRegistry maps each command name to its argument builder. It is the
// only place a command is wired to a builder, so a case for an unregistered
// command cannot exist, and tests check it against GetToolDefinitions in both
// directions.
var commandRegistry = map[string]func(map[string]interface{}) []string{
	"tree":           buildTreeArgs,
	"grep":           buildGrepArgs,
	"multiexists":    buildMultiexistsArgs,
	"template":       buildTemplateArgs,
	"discover_tests": buildDiscoverTestsArgs,
	"multigrep":      buildMultigrepArgs,
	"detect":         buildDetectArgs,
	"count":          buildCountArgs,
	"git_context":    buildGitContextArgs,
	"repo_root":      buildRepoRootArgs,
	"highest":        buildHighestArgs,
	"git_changes":    buildGitChangesArgs,
	"yaml_get":       buildYamlGetArgs,
	"yaml_set":       buildYamlSetArgs,
	"yaml_multiget":  buildYamlMultigetArgs,
	"yaml_multiset":  buildYamlMultisetArgs,
	"catfiles":       buildCatfilesArgs,
	"math":           buildMathArgs,
	"stats":          buildStatsArgs,
	"runtime":        buildRuntimeArgs,
}

// buildArgs builds CLI arguments for the given tool
func buildArgs(cmdName string, args map[string]interface{}) ([]string, error) {
	// A moved command fails naming its cadence replacement, with the same
	// text the CLI prints (epic 11.2 T3), before anything is exec'd.
	if err := moved.ErrorFor(cmdName); err != nil {
		return nil, err
	}
	build, ok := commandRegistry[cmdName]
	if !ok {
		return nil, fmt.Errorf("unknown command: %s", cmdName)
	}
	// Normalize parameter aliases before processing
	return build(normalizeArgs(args)), nil
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
	var positionals []string
	if pattern, ok := args["pattern"].(string); ok {
		positionals = append(positionals, pattern)
	}
	positionals = append(positionals, stringList(args, "paths")...)
	if getBool(args, "ignore_case") {
		cmdArgs = append(cmdArgs, "-i")
	}
	if getBool(args, "line_numbers") {
		cmdArgs = append(cmdArgs, "-n")
	}
	if getBool(args, "files_only") {
		cmdArgs = append(cmdArgs, "-l")
	}
	return appendPositionals(cmdArgs, positionals...)
}

func buildMultiexistsArgs(args map[string]interface{}) []string {
	cmdArgs := []string{"multiexists"}
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
	return appendPositionals(cmdArgs, stringList(args, "paths")...)
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

	// The file is the one positional, so it goes last, after "--".
	// --literal-vars: the CLI reads a --var value starting with @ as a file,
	// so {"k": "@sec.txt"} returned sec.txt to a caller that never opted in.
	cmdArgs := []string{"template", "--literal-vars"}

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
	return appendPositionals(cmdArgs, templateFile)
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

	// The key is a positional, so it goes last, after "--".
	if key, ok := args["key"].(string); ok {
		cmdArgs = appendPositionals(cmdArgs, key)
	}

	return cmdArgs
}

func buildYamlSetArgs(args map[string]interface{}) []string {
	cmdArgs := []string{"yaml", "set"}

	// Add --file flag (required)
	if file, ok := args["file"].(string); ok {
		cmdArgs = append(cmdArgs, "--file", file)
	}

	// Key and value are positionals, appended last after "--".
	var positionals []string
	if key, ok := args["key"].(string); ok {
		positionals = append(positionals, key)
	}
	if value, ok := args["value"].(string); ok {
		positionals = append(positionals, value)
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

	return appendPositionals(cmdArgs, positionals...)
}

func buildYamlMultigetArgs(args map[string]interface{}) []string {
	cmdArgs := []string{"yaml", "multiget"}

	// Add --file flag (required)
	if file, ok := args["file"].(string); ok {
		cmdArgs = append(cmdArgs, "--file", file)
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

	// The keys are positionals, appended last after "--".
	return appendPositionals(cmdArgs, stringList(args, "keys")...)
}

func buildYamlMultisetArgs(args map[string]interface{}) []string {
	cmdArgs := []string{"yaml", "multiset"}

	// Add --file flag (required)
	if file, ok := args["file"].(string); ok {
		cmdArgs = append(cmdArgs, "--file", file)
	}

	// Convert the pairs object to KEY VALUE positionals, appended last after
	// "--". Keys are sorted so the command line does not depend on map order.
	var positionals []string
	if pairs, ok := args["pairs"].(map[string]interface{}); ok {
		keys := make([]string, 0, len(pairs))
		for key := range pairs {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			positionals = append(positionals, key)
			if v, ok := pairs[key].(string); ok {
				positionals = append(positionals, v)
			} else {
				positionals = append(positionals, fmt.Sprintf("%v", pairs[key]))
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

	return appendPositionals(cmdArgs, positionals...)
}

func buildCatfilesArgs(args map[string]interface{}) []string {
	cmdArgs := []string{"catfiles"}

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

	return appendPositionals(cmdArgs, stringList(args, "paths")...)
}

func buildMathArgs(args map[string]interface{}) []string {
	cmdArgs := []string{"math"}

	// A leading "-" is common in an expression ("-1+2"), so it goes after "--".
	if expr, ok := args["expression"].(string); ok {
		cmdArgs = appendPositionals(cmdArgs, expr)
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
