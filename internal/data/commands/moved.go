package commands

import (
	"github.com/samestrin/llm-data-axi/v2/internal/data/moved"
	"github.com/spf13/cobra"
)

// newMovedCmd is the stub a moved command leaves behind (epic 11.2 T3). It is
// hidden, so --help does not advertise a command that cannot run, and every
// invocation fails with the moved error through the root's existing error
// path: stdout, in the run's own format, exit 2. Unknown flags are tolerated
// so the flags the old command took reach the moved error too, instead of an
// unknown-flag error that hides where the command went; nothing is silently
// accepted, because the call always fails. The root's format flags still
// parse, so PersistentPreRunE publishes the mode the error prints in. Long
// carries the same message so even `llm-data help <name>` names the
// replacement.
func newMovedCmd(name string) *cobra.Command {
	err := moved.ErrorFor(name)
	return &cobra.Command{
		Use:                name,
		Short:              err.Error(),
		Long:               err.Error(),
		Hidden:             true,
		FParseErrWhitelist: cobra.FParseErrWhitelist{UnknownFlags: true},
		RunE: func(cmd *cobra.Command, args []string) error {
			return err
		},
	}
}
