package mcpserver

import (
	"encoding/json"
)

// ToolPrefix is the prefix for all llm-data tools
const ToolPrefix = "llm_data_"

// ToolDefinition defines a tool for the MCP SDK
type ToolDefinition struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

// GetToolDefinitions returns tool definitions for the official MCP SDK
func GetToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        ToolPrefix + "tree",
			Description: "Display directory structure as a tree. Respects .gitignore. Use max_entries to limit output.",
			InputSchema: json.RawMessage(`{
						"type": "object",
						"properties": {
							"path": {
								"type": "string"
							},
							"depth": {
								"type": "integer",
								"description": "Maximum depth to display"
							},
							"max_entries": {
								"type": "integer",
								"description": "Maximum entries to display (default: 500, 0 = unlimited)"
							},
							"sizes": {
								"type": "boolean"
							},
							"exclude": {
								"type": "array",
								"items": {
									"type": "string"
								},
								"description": "Regex patterns to exclude (e.g., [\"test\", \"fixtures\"])"
							},
							"no_gitignore": {
								"type": "boolean",
								"description": "Disable .gitignore filtering"
							},
							"no_default_excludes": {
								"type": "boolean",
								"description": "Disable default excludes (node_modules, vendor, target, etc.)"
							}
						}
					}`),
		},
		{
			Name:        ToolPrefix + "grep",
			Description: "Search for regex pattern in files. Supports case-insensitive search and line numbers.",
			InputSchema: json.RawMessage(`{
						"type": "object",
						"properties": {
							"pattern": {
								"type": "string"
							},
							"paths": {
								"type": "array",
								"items": {
									"type": "string"
								}
							},
							"ignore_case": {
								"type": "boolean"
							},
							"line_numbers": {
								"type": "boolean"
							},
							"files_only": {
								"type": "boolean"
							}
						},
						"required": ["pattern", "paths"]
					}`),
		},
		{
			Name:        ToolPrefix + "multiexists",
			Description: "Check if multiple files or directories exist. Returns detailed status for each path.",
			InputSchema: json.RawMessage(`{
						"type": "object",
						"properties": {
							"paths": {
								"type": "array",
								"items": {
									"type": "string"
								}
							},
							"verbose": {
								"type": "boolean"
							}
						},
						"required": ["paths"]
					}`),
		},
		{
			Name:        ToolPrefix + "template",
			Description: "Variable substitution in template files. Supports [[var]] or {{var}} syntax.",
			InputSchema: json.RawMessage(`{
						"type": "object",
						"properties": {
							"file": {
								"type": "string"
							},
							"vars": {
								"type": "object"
							},
							"syntax": {
								"type": "string",
								"enum": ["brackets", "braces"],
								"description": "brackets or braces"
							}
						},
						"required": ["file"]
					}`),
		},
		{
			Name:        ToolPrefix + "discover_tests",
			Description: "Discover test patterns, runners, and infrastructure in a project.",
			InputSchema: json.RawMessage(`{
						"type": "object",
						"properties": {
							"path": {
								"type": "string"
							}
						}
					}`),
		},
		{
			Name:        ToolPrefix + "multigrep",
			Description: "Search multiple keywords in parallel. Prioritizes definitions over usages.",
			InputSchema: json.RawMessage(`{
						"type": "object",
						"properties": {
							"keywords": {
								"type": "string"
							},
							"path": {
								"type": "string",
								"description": "Path to search (default: current directory)"
							},
							"extensions": {
								"type": "string",
								"description": "Comma-separated file extensions (e.g., 'ts,tsx,js')"
							},
							"max_per_keyword": {
								"type": "integer",
								"description": "Max matches per keyword (default: 20)"
							},
							"ignore_case": {
								"type": "boolean"
							},
							"definitions_only": {
								"type": "boolean",
								"description": "Only show definition matches"
							},
							"output_dir": {
								"type": "string",
								"description": "Write per-keyword results to directory"
							}
						},
						"required": ["keywords"]
					}`),
		},
		{
			Name:        ToolPrefix + "detect",
			Description: "Detect project type and technology stack.",
			InputSchema: json.RawMessage(`{
						"type": "object",
						"properties": {
							"path": {
								"type": "string",
								"description": "Project path to analyze"
							},
							"dirs": {
								"type": "string",
								"description": "Subdirs for per-component detection (e.g., backend,frontend)"
							}
						}
					}`),
		},
		{
			Name:        ToolPrefix + "count",
			Description: "Count checkboxes, lines, or files. For checkboxes: returns TOTAL, CHECKED, UNCHECKED, COMPLETION%.",
			InputSchema: json.RawMessage(`{
						"type": "object",
						"properties": {
							"mode": {
								"type": "string",
								"enum": ["checkboxes", "lines", "files"],
								"description": "What to count"
							},
							"path": {
								"type": "string",
								"description": "File or directory to analyze"
							},
							"recursive": {
								"type": "boolean"
							},
							"pattern": {
								"type": "string",
								"description": "Glob pattern (for files mode)"
							}
						},
						"required": ["mode", "path"]
					}`),
		},
		{
			Name:        ToolPrefix + "git_context",
			Description: "Gather git information for LLM context.",
			InputSchema: json.RawMessage(`{
						"type": "object",
						"properties": {
							"path": {
								"type": "string"
							},
							"include_diff": {
								"type": "boolean",
								"description": "Include diff statistics"
							},
							"since": {
								"type": "string",
								"description": "Only include commits since this date"
							},
							"max_commits": {
								"type": "integer",
								"description": "Maximum commits to show (default: 10)"
							}
						}
					}`),
		},
		{
			Name:        ToolPrefix + "repo_root",
			Description: "Find git repository root for a path.",
			InputSchema: json.RawMessage(`{
						"type": "object",
						"properties": {
							"path": {
								"type": "string"
							},
							"validate": {
								"type": "boolean"
							}
						}
					}`),
		},
		{
			Name:        ToolPrefix + "extract_relevant",
			Description: "Extract relevant content from files/URLs using LLM. Filters to query-relevant parts.",
			InputSchema: json.RawMessage(`{
						"type": "object",
						"properties": {
							"path": {
								"type": "string",
								"description": "File, directory, or URL"
							},
							"context": {
								"type": "string",
								"description": "Relevance criteria"
							},
							"concurrency": {
								"type": "integer",
								"description": "Concurrent API calls (default: 2)"
							},
							"output": {
								"type": "string"
							},
							"timeout": {
								"type": "integer",
								"description": "Timeout in seconds (default: 60)"
							}
						},
						"required": ["context"]
					}`),
		},
		{
			Name:        ToolPrefix + "extract_links",
			Description: "Extract and rank links from a URL. Heuristic mode (default) or LLM-ranked (with context).",
			InputSchema: json.RawMessage(`{
						"type": "object",
						"properties": {
							"url": {
								"type": "string",
								"description": "URL"
							},
							"context": {
								"type": "string",
								"description": "Context for LLM-based ranking (enables LLM mode)"
							},
							"timeout": {
								"type": "integer",
								"description": "HTTP timeout in seconds (default: 30)"
							}
						},
						"required": ["url"]
					}`),
		},
		{
			Name:        ToolPrefix + "highest",
			Description: "Find highest numbered directory or file. Returns HIGHEST, NAME, FULL_PATH, NEXT, COUNT.",
			InputSchema: json.RawMessage(`{
						"type": "object",
						"properties": {
							"path": {
								"type": "string",
								"description": "Directory to search in (default: current directory)"
							},
							"paths": {
								"type": "array",
								"items": {
									"type": "string"
								},
								"description": "Multiple dirs to search"
							},
							"pattern": {
								"type": "string",
								"description": "Custom regex for version extraction"
							},
							"type": {
								"type": "string",
								"enum": ["dir", "file", "both"],
								"description": "Type to search: dir, file, both (default: both)"
							},
							"prefix": {
								"type": "string",
								"description": "Prefix filter (e.g., 01-)"
							}
						}
					}`),
		},
		{
			Name:        ToolPrefix + "plan_type",
			Description: "Extract plan type from planning metadata.",
			InputSchema: json.RawMessage(`{
						"type": "object",
						"properties": {
							"path": {
								"type": "string",
								"description": "Plan directory path (default: current directory)"
							}
						}
					}`),
		},
		{
			Name:        ToolPrefix + "git_changes",
			Description: "Count and list git working tree changes with optional path filtering.",
			InputSchema: json.RawMessage(`{
						"type": "object",
						"properties": {
							"path": {
								"type": "string",
								"description": "Path prefix filter"
							},
							"include_untracked": {
								"type": "boolean",
								"description": "Include untracked files (default: true)"
							},
							"staged_only": {
								"type": "boolean",
								"description": "Only show staged changes"
							}
						}
					}`),
		},
		{
			Name:        ToolPrefix + "yaml_get",
			Description: "Retrieve a value from YAML config by dot-notation key.",
			InputSchema: json.RawMessage(`{
						"type": "object",
						"properties": {
							"file": {
								"type": "string"
							},
							"key": {
								"type": "string",
								"description": "Dot-notation key"
							},
							"default": {
								"type": "string",
								"description": "Default if missing"
							}
						},
						"required": ["file", "key"]
					}`),
		},
		{
			Name:        ToolPrefix + "yaml_set",
			Description: "Store a value in YAML config at dot-notation key.",
			InputSchema: json.RawMessage(`{
						"type": "object",
						"properties": {
							"file": {
								"type": "string"
							},
							"key": {
								"type": "string",
								"description": "Dot-notation key"
							},
							"value": {
								"type": "string"
							},
							"create": {
								"type": "boolean",
								"description": "Create file if it doesn't exist"
							},
							"dry_run": {
								"type": "boolean",
								"description": "Preview only"
							},
							"quiet": {
								"type": "boolean",
								"description": "Suppress success messages"
							}
						},
						"required": ["file", "key", "value"]
					}`),
		},
		{
			Name:        ToolPrefix + "yaml_multiget",
			Description: "Retrieve multiple YAML values in a single call.",
			InputSchema: json.RawMessage(`{
						"type": "object",
						"properties": {
							"file": {
								"type": "string"
							},
							"keys": {
								"type": "array",
								"items": {
									"type": "string"
								},
								"description": "Keys (dot-notation)"
							},
							"defaults": {
								"type": "object",
								"description": "Defaults for keys"
							},
							"required_file": {
								"type": "string",
								"description": "File with required keys (one per line)"
							}
						},
						"required": ["file"]
					}`),
		},
		{
			Name:        ToolPrefix + "yaml_multiset",
			Description: "Set multiple YAML key-value pairs atomically.",
			InputSchema: json.RawMessage(`{
						"type": "object",
						"properties": {
							"file": {
								"type": "string"
							},
							"pairs": {
								"type": "object",
								"description": "Key-value pairs"
							},
							"create": {
								"type": "boolean",
								"description": "Create file if it doesn't exist"
							},
							"dry_run": {
								"type": "boolean",
								"description": "Preview only"
							},
							"quiet": {
								"type": "boolean",
								"description": "Suppress success messages"
							}
						},
						"required": ["file", "pairs"]
					}`),
		},
		{
			Name:        ToolPrefix + "catfiles",
			Description: "Concatenate multiple files with path headers. Respects .gitignore.",
			InputSchema: json.RawMessage(`{
						"type": "object",
						"properties": {
							"paths": {
								"type": "array",
								"items": {
									"type": "string"
								}
							},
							"max_size": {
								"type": "integer",
								"description": "Max size in MB (default: 10)"
							},
							"no_gitignore": {
								"type": "boolean",
								"description": "Disable .gitignore filtering"
							}
						},
						"required": ["paths"]
					}`),
		},
		{
			Name:        ToolPrefix + "math",
			Description: "Evaluate mathematical expressions safely. Supports basic arithmetic, functions (sin, cos, sqrt, etc.), and variables.",
			InputSchema: json.RawMessage(`{
						"type": "object",
						"properties": {
							"expression": {
								"type": "string"
							}
						},
						"required": ["expression"]
					}`),
		},
		{
			Name:        ToolPrefix + "stats",
			Description: "Display directory statistics (file counts, size, extension breakdown).",
			InputSchema: json.RawMessage(`{
						"type": "object",
						"properties": {
							"path": {
								"type": "string"
							},
							"no_gitignore": {
								"type": "boolean",
								"description": "Disable .gitignore filtering"
							}
						}
					}`),
		},
		{
			Name:        ToolPrefix + "runtime",
			Description: "Calculate elapsed time between epoch timestamps.",
			InputSchema: json.RawMessage(`{
						"type": "object",
						"properties": {
							"start": {
								"type": "integer",
								"description": "Start epoch timestamp (required)"
							},
							"end": {
								"type": "integer",
								"description": "End epoch timestamp (default: now)"
							},
							"format": {
								"type": "string",
								"enum": ["secs", "mins", "mins-secs", "hms", "human", "compact"],
								"description": "Output format (default: human)"
							},
							"precision": {
								"type": "integer",
								"description": "Decimal precision for output (default: 1)"
							},
							"label": {
								"type": "boolean",
								"description": "Include 'Runtime: ' prefix"
							},
							"raw": {
								"type": "boolean",
								"description": "Output raw number without unit suffix"
							}
						},
						"required": ["start"]
					}`),
		},
		{
			Name:        ToolPrefix + "tier_classifier",
			Description: "Classify packages into documentation tiers (critical/important/pattern/utility/skip) via deterministic passes 1-3: explicit config map, glob patterns, keyword categories. Returns assigned map + unassigned list (for the model's Pass 4) + per-pass counts. Deterministic replacement for in-model tier assignment in /init-documentation.",
			InputSchema: json.RawMessage(`{
						"type": "object",
						"properties": {
							"packages": {"type": "string", "description": "Comma-separated package names"},
							"config": {"type": "string", "description": "Path to package-tiers.yaml (packages/patterns/categories)"}
						},
						"required": ["packages"]
					}`),
		},
		{
			Name:        ToolPrefix + "diff_smell",
			Description: "Scan a diff for over-simplification / reward-hack fingerprints: the change touched only tests (test_only), a test removed assertions without replacing them (weakened_assertion), or an added line suppressed a linter/type-checker (suppression), swallowed an exception (empty_catch), or stubbed a body (stub_body). Deterministic, model-independent gate for /resolve-td — the inverse of its too-complex SAFE_SCOPE check. Returns smells + summary.verdict (hard=force review / soft_only=adjudicate / clean). Source is a diff file (`diff`) or a commit (`repo`+`rev`, via git show).",
			InputSchema: json.RawMessage(`{
						"type": "object",
						"properties": {
							"diff": {"type": "string", "description": "Path to a unified diff file (takes precedence over repo/rev)"},
							"repo": {"type": "string", "description": "Repository to read the commit diff from when diff is not given"},
							"rev": {"type": "string", "description": "Commit to scan via 'git show <rev>' (default HEAD)"}
						}
					}`),
		},
		{
			Name:        ToolPrefix + "project_components",
			Description: "Read project config and return normalized component list.",
			InputSchema: json.RawMessage(`{
						"type": "object",
						"properties": {
							"file": {
								"type": "string"
							}
						},
						"required": ["file"]
					}`),
		},
	}
}
