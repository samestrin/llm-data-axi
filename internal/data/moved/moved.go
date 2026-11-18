// Package moved is the one table of commands that left llm-data for cadence
// (epic 11.2). The CLI and the MCP server both report a call to a moved name
// with the same error text, naming the cadence replacement: a bare "unknown
// command" tells an agent nothing about where the command went.
package moved

import (
	"fmt"
	"strings"

	"github.com/samestrin/llm-data-axi/v2/pkg/output"
)

// Table maps each removed CLI command name to the cadence command that
// replaces it. It is the only place a move is recorded; adding a row here is
// what makes both the CLI and the MCP server report the move.
var Table = map[string]string{
	"plan-type":          "cadence plan type",
	"project-components": "cadence plan components",
	"tier-classifier":    "cadence tier classify",
	"diff-smell":         "cadence diff smell",
}

// ErrorFor returns the moved-command error for name, or nil when name did not
// move. MCP tool names are the CLI names with underscores for dashes, so both
// spellings resolve to the same entry and the same message, which names the
// command by its CLI spelling.
func ErrorFor(name string) error {
	cliName := strings.ReplaceAll(name, "_", "-")
	replacement, ok := Table[cliName]
	if !ok {
		return nil
	}
	return &Error{Name: cliName, Replacement: replacement}
}

// Error is a call to a command that moved to cadence. It unwraps to a
// UsageError because the invocation can never succeed unchanged (exit 2), and
// carries moved_to so TOON and JSON readers get the replacement as data, not
// only inside the message.
type Error struct {
	Name        string
	Replacement string
}

func (e *Error) Error() string {
	return fmt.Sprintf("command %q has moved to cadence: run %q instead", e.Name, e.Replacement)
}

// ErrorFields puts the replacement next to the message in TOON and JSON.
func (e *Error) ErrorFields() map[string]any {
	return map[string]any{"moved_to": e.Replacement}
}

// Unwrap types the move as a usage error for output.ExitCodeFor.
func (e *Error) Unwrap() error { return &output.UsageError{Msg: e.Error()} }
