package mcpserver

import (
	"encoding/json"
	"sort"
	"testing"
)

// portedTools is the exact set carried over from llm-support by the Phase 2
// port rule: a tool travels only if one of the 46 cadence skills calls it.
//
// The list is written out in full rather than derived from GetToolDefinitions,
// because a test that reads its expectation from the thing under test cannot
// catch a tool being dropped.
var portedTools = []string{
	"catfiles", "count", "detect", "diff_smell", "discover_tests",
	"extract_links", "extract_relevant", "git_changes", "git_context",
	"grep", "highest", "math", "multiexists", "multigrep", "plan_type",
	"project_components", "repo_root", "runtime", "stats", "template",
	"tier_classifier", "tree", "yaml_get", "yaml_multiget", "yaml_multiset",
	"yaml_set",
}

func TestGetToolDefinitionsRegistersExactlyThePortedSet(t *testing.T) {
	tools := GetToolDefinitions()

	if len(tools) != len(portedTools) {
		t.Errorf("expected %d tools, got %d", len(portedTools), len(tools))
	}

	got := make([]string, 0, len(tools))
	for _, tool := range tools {
		got = append(got, tool.Name)
	}
	sort.Strings(got)

	want := make([]string, 0, len(portedTools))
	for _, name := range portedTools {
		want = append(want, ToolPrefix+name)
	}
	sort.Strings(want)

	// Reported as two lists rather than a count, because "26 tools, one of them
	// the wrong one" is the failure a count alone reads as a pass.
	wantSet := make(map[string]bool, len(want))
	for _, n := range want {
		wantSet[n] = true
	}
	gotSet := make(map[string]bool, len(got))
	for _, n := range got {
		gotSet[n] = true
	}
	for _, n := range want {
		if !gotSet[n] {
			t.Errorf("missing tool: %s", n)
		}
	}
	for _, n := range got {
		if !wantSet[n] {
			t.Errorf("unexpected tool registered: %s", n)
		}
	}
}

func TestEveryToolNameCarriesThePrefix(t *testing.T) {
	for _, tool := range GetToolDefinitions() {
		if len(tool.Name) < len(ToolPrefix) || tool.Name[:len(ToolPrefix)] != ToolPrefix {
			t.Errorf("tool %q does not start with %q", tool.Name, ToolPrefix)
		}
	}
}

func TestEveryToolSchemaIsAValidObjectSchema(t *testing.T) {
	for _, tool := range GetToolDefinitions() {
		var schema map[string]interface{}
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Errorf("tool %s has an invalid JSON schema: %v", tool.Name, err)
			continue
		}
		if schemaType, ok := schema["type"].(string); !ok || schemaType != "object" {
			t.Errorf("tool %s schema should declare type: object", tool.Name)
		}
	}
}

func TestEveryToolHasAUsefulDescription(t *testing.T) {
	for _, tool := range GetToolDefinitions() {
		if len(tool.Description) < 10 {
			t.Errorf("tool %s has a too-short description: %q", tool.Name, tool.Description)
		}
	}
}

func TestToolSchemaRequiredFields(t *testing.T) {
	requiredFields := map[string][]string{
		ToolPrefix + "grep":             {"pattern", "paths"},
		ToolPrefix + "multiexists":      {"paths"},
		ToolPrefix + "template":         {"file"},
		ToolPrefix + "multigrep":        {"keywords"},
		ToolPrefix + "count":            {"mode", "path"},
		ToolPrefix + "extract_relevant": {"context"},
		ToolPrefix + "yaml_get":         {"file", "key"},
		ToolPrefix + "yaml_set":         {"file", "key", "value"},
		ToolPrefix + "yaml_multiget":    {"file"},
		ToolPrefix + "yaml_multiset":    {"file", "pairs"},
	}

	seen := make(map[string]bool)
	for _, tool := range GetToolDefinitions() {
		expected, hasRequired := requiredFields[tool.Name]
		if !hasRequired {
			continue
		}
		seen[tool.Name] = true

		var schema map[string]interface{}
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Errorf("tool %s: failed to parse schema: %v", tool.Name, err)
			continue
		}

		required, ok := schema["required"].([]interface{})
		if !ok {
			t.Errorf("tool %s: missing 'required' array in schema", tool.Name)
			continue
		}

		requiredStrs := make(map[string]bool, len(required))
		for _, r := range required {
			if s, ok := r.(string); ok {
				requiredStrs[s] = true
			}
		}

		for _, exp := range expected {
			if !requiredStrs[exp] {
				t.Errorf("tool %s: missing required field %s", tool.Name, exp)
			}
		}
	}

	// Guards against the table silently naming a tool that is not registered,
	// which would make this whole test vacuous for that row.
	for name := range requiredFields {
		if !seen[name] {
			t.Errorf("requiredFields names %s, which is not registered", name)
		}
	}
}
