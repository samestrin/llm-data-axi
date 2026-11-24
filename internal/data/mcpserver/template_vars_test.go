package mcpserver

import (
	"strings"
	"testing"
)

// Defends MCP template values against being read as files: each vars value
// went to the CLI as --var k=v, and the CLI reads a value starting with @ as
// a file path, so {"k": "@sec.txt"} returned sec.txt. The README calls that
// read opt-in; over MCP the caller never opted in. The server passes
// --literal-vars, before the separator, and the value unchanged.
func TestTemplateVarsArePassedLiterally(t *testing.T) {
	got := buildTemplateArgs(map[string]interface{}{"file": "t.txt", "vars": map[string]interface{}{"k": "@sec.txt"}})
	sep := separatorIndex(got)
	if sep < 0 {
		t.Fatalf("no separator: %q", got)
	}
	head := strings.Join(got[:sep], "\x00")
	if !strings.Contains(head, "--literal-vars") {
		t.Errorf("buildTemplateArgs = %q: want --literal-vars before \"--\"", got)
	}
	if !strings.Contains(head, "--var\x00k=@sec.txt") {
		t.Errorf("buildTemplateArgs = %q: want the value as given", got)
	}
}

// Defends the tool description: it must say values are literal and that the
// default syntax is [[var]], which differs from the CLI's {{var}} default; a
// client that reads the CLI help and sends {{var}} got its placeholders back
// unchanged with no error.
func TestTemplateToolDescriptionStatesLiteralValuesAndDefaultSyntax(t *testing.T) {
	for _, tool := range GetToolDefinitions() {
		if tool.Name != ToolPrefix+"template" {
			continue
		}
		text := tool.Description + string(tool.InputSchema)
		for _, want := range []string{"literal", "not read as a file", "default [[var]]", "CLI defaults to {{var}}"} {
			if !strings.Contains(text, want) {
				t.Errorf("template tool description lacks %q: %s", want, text)
			}
		}
		return
	}
	t.Fatal("no template tool")
}
