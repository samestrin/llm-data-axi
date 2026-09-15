package commands

import (
	"os"
	"strings"

	"github.com/samestrin/llm-data-axi/pkg/output"
	"github.com/spf13/cobra"
)

// Version is set at build time via ldflags
var Version = "1.0.0"

// Global output flags accessible to all commands
var (
	GlobalJSONOutput bool
	GlobalMinOutput  bool

	// GlobalAXIOutput selects TOON output. It reaches every command through
	// output.SetDefaultAXI rather than through a per-command flag: each command
	// builds its own formatter with output.New(json, min, w), so a package
	// default is the one place that covers all of them without editing every
	// command file and inventing one chance per file to miss it.
	GlobalAXIOutput bool
)

// ResetOutputModes clears the global output flags and republishes the package
// default.
//
// It exists because RootCmd is a package-level singleton and pflag leaves a
// bound variable UNTOUCHED when its flag is absent. So a second in-process
// invocation inherits the first one's modes: running with --axi and then with
// --json emitted TOON for the second, and the same is true of --json and --min.
//
// A shipped binary cannot hit this — it runs Execute once per process, and the
// MCP server execs a fresh binary per tool call. The exposure is in-process
// reuse: a test binary today, and any future caller that drives the command
// tree twice.
//
// Execute calls this. A caller that drives RootCmd.Execute() directly must call
// it itself.
func ResetOutputModes() {
	GlobalJSONOutput = false
	GlobalMinOutput = false
	GlobalAXIOutput = false
	output.SetDefaultAXI(false)
}

// RootCmd is the base command when called without any subcommands
var RootCmd = &cobra.Command{
	Use:   "llm-data",
	Short: "Generic data, file and project analysis tools for LLM workflows",
	Long: `llm-data provides deterministic commands for reading, searching and
reshaping code, configuration files and documentation in LLM-assisted
workflows.

Designed for fast startup, single binary distribution, and AXI (TOON)
output that costs fewer tokens than JSON.`,
	Version:       Version,
	SilenceErrors: true,
	SilenceUsage:  true,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Sync local command flags to global vars for error handling
		if f := cmd.Flag("json"); f != nil && f.Changed {
			GlobalJSONOutput = true
		}
		if f := cmd.Flag("min"); f != nil && f.Changed {
			GlobalMinOutput = true
		}
		// Published UNCONDITIONALLY, including when the flag is false. An
		// earlier version only ever assigned true, with no else branch, so the
		// package default kept the previous run's setting: running with --axi
		// and then with --json in the same process emitted TOON for the second
		// one. cobra does not reset a bound flag variable between Execute
		// calls, so nothing else would have cleared it.
		output.SetDefaultAXI(GlobalAXIOutput)
	},
}

// Execute runs the root command
func Execute() {
	// Cleared before parse, because pflag only writes a bound variable when it
	// actually sees the flag. A no-op for the shipped binary, which runs this
	// once; load-bearing for anything that runs the command tree more than once
	// in a process.
	ResetOutputModes()
	if err := RootCmd.Execute(); err != nil {
		f := output.New(GlobalJSONOutput, GlobalMinOutput, os.Stdout)
		os.Exit(f.PrintError(err))
	}
}

func init() {
	// Global persistent flags (available to all subcommands)
	RootCmd.PersistentFlags().BoolP("verbose", "v", false, "Enable verbose output")
	RootCmd.PersistentFlags().String("format", "text", "Output format: text, json")
	RootCmd.PersistentFlags().Bool("no-gitignore", false, "Disable .gitignore filtering")
	RootCmd.PersistentFlags().BoolVar(&GlobalJSONOutput, "json", false, "Output as JSON")
	RootCmd.PersistentFlags().BoolVar(&GlobalMinOutput, "min", false, "Minimal/token-optimized output")
	RootCmd.PersistentFlags().BoolVar(&GlobalAXIOutput, "axi", false, "Output as TOON (AXI token-dense format)")
}

// splitCSV splits a comma-separated string, trimming blanks.
//
// Ported from td_filter.go, which does not travel to this repo; tier_classifier
// is its only caller here.
func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
