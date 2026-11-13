package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/samestrin/llm-data-axi/pkg/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Version is set at build time via ldflags
var Version = "1.0.0"

// bareVersion is Version without a leading "v". release.yml stamps the tag
// name, so a release build carries "v1.1.0" and the version flags print
// "1.1.0".
func bareVersion() string {
	return strings.TrimPrefix(Version, "v")
}

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

// NewRootCmd builds a fresh command tree. Execute builds one per invocation.
//
// The tree is rebuilt rather than shared because pflag leaves a bound variable
// UNTOUCHED when its flag is absent, and keeps a flag's Changed bit once set.
// With a package singleton, a second in-process run inherited the first one's
// modes: --axi and then --json emitted TOON for the second. A new tree
// re-registers every flag, and registering a flag writes its default into the
// bound variable, so each invocation starts from a clean state with nothing to
// reset by hand.
func NewRootCmd() *cobra.Command {
	var showVersion bool
	root := &cobra.Command{
		Use:   "llm-data",
		Short: "Generic data, file and project analysis tools for LLM workflows",
		Long: `llm-data provides deterministic commands for reading, searching and
reshaping code, configuration files and documentation in LLM-assisted
workflows.

Designed for fast startup, single binary distribution, and AXI (TOON)
output that costs fewer tokens than JSON.`,
		Version: bareVersion(),
		// No Args validator on purpose: with Args nil, cobra's legacyArgs rejects a
		// stray word as "unknown command" before it looks at --help. cobra.NoArgs
		// would let `llm-data typo --help` print help and exit 0.
		RunE:          runHome,
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
			// Published unconditionally, including when the flag is false, so the
			// package default always matches this invocation.
			output.SetDefaultAXI(GlobalAXIOutput)
		},
	}

	// -v, -V and --version print the bare version and nothing else. Both flags
	// bind showVersion, so cobra's version check, which reads "version", sees
	// -V too. They are root-local: under a subcommand -v is an unknown flag,
	// not a silently ignored one.
	root.SetVersionTemplate("{{.Version}}\n")
	root.Flags().BoolVarP(&showVersion, "version", "v", false, "Print the version and exit (also -V)")
	root.Flags().BoolVarP(&showVersion, "version-upper", "V", false, "Print the version and exit")
	_ = root.Flags().MarkHidden("version-upper")

	// Inherited by every subcommand: cobra looks up the parent chain for one.
	root.SetFlagErrorFunc(flagError)

	// Global persistent flags (available to all subcommands)
	root.PersistentFlags().Bool("no-gitignore", false, "Disable .gitignore filtering")
	root.PersistentFlags().BoolVar(&GlobalJSONOutput, "json", false, "Output as JSON")
	root.PersistentFlags().BoolVar(&GlobalMinOutput, "min", false, "Minimal/token-optimized output")
	root.PersistentFlags().BoolVar(&GlobalAXIOutput, "axi", false, "Output as TOON (AXI token-dense format)")

	root.AddCommand(
		newCatfilesCmd(),
		newCountCmd(),
		newDetectCmd(),
		newDiffSmellCmd(),
		newDiscoverTestsCmd(),
		newGitChangesCmd(),
		newGitContextCmd(),
		newGrepCmd(),
		newHighestCmd(),
		newMathCmd(),
		newMultiexistsCmd(),
		newMultigrepCmd(),
		newPlanTypeCmd(),
		newProjectComponentsCmd(),
		newRepoRootCmd(),
		newRuntimeCmd(),
		newSetupCmd(),
		newStatsCmd(),
		newTemplateCmd(),
		newTierClassifierCmd(),
		newTreeCmd(),
		newYamlCmd(),
	)
	return root
}

// homeDescription is the one sentence the home view prints.
const homeDescription = "Deterministic commands for reading, searching and reshaping code, configuration and documentation in LLM workflows."

// executablePath is os.Executable, swappable so a test can pin the path the
// home view reports.
var executablePath = os.Executable

// homeGitTimeout bounds every git call the home view makes. A bare run is
// what a session-start hook calls, so a hung git must not hang it.
var homeGitTimeout = 2 * time.Second

// homeHelp is the next step a bare run offers in --json and --axi. The plain
// view stays two lines; help there is what --help is for.
var homeHelp = []string{
	"Run `llm-data --help` to list every command",
	"Run `llm-data <command> --help` for one command's flags",
}

// HomeView is what a bare `llm-data` prints: where the binary is, what it is
// for and the repository it was run in, instead of the full cobra help page.
type HomeView struct {
	Bin         string `json:"bin"`
	Description string `json:"description"`
	RepoRoot    string `json:"repo_root,omitempty"`
	ProjectType string `json:"project_type,omitempty"`
	// A pointer so a clean tree prints 0 rather than dropping the field.
	GitChanges *int `json:"git_changes,omitempty"`
	// Repo is set only when the repository fields are missing or partial:
	// "no repository", or "git unavailable: <reason>".
	Repo string   `json:"repo,omitempty"`
	Help []string `json:"help,omitempty"`
}

// fillRepo adds the repository the working directory sits in. Every git
// failure lands in Repo rather than in an error: the view is still worth
// printing without it, and the run still exits 0.
func (v *HomeView) fillRepo(home string) {
	ctx, cancel := context.WithTimeout(context.Background(), homeGitTimeout)
	defer cancel()

	root, err := repoRootOf(ctx, "")
	if errors.Is(err, errNotARepository) {
		v.Repo = "no repository"
		return
	}
	if err != nil {
		v.Repo = gitUnavailable(err)
		return
	}
	v.RepoRoot = tildeHome(root, home)
	v.ProjectType = detectInDir(root).Stack

	changes, err := gitChangesIn(ctx, root, "", true, false)
	if err != nil {
		// Never "no repository" here: the root was just found.
		v.Repo = gitUnavailable(err)
		return
	}
	v.GitChanges = &changes.Count
}

func gitUnavailable(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Sprintf("git unavailable: no answer within %s", homeGitTimeout)
	}
	return "git unavailable: " + err.Error()
}

// runHome prints the home view. A path that cannot be resolved is an error,
// not a blank line: the view exists to say where the binary is.
func runHome(cmd *cobra.Command, args []string) error {
	exe, err := executablePath()
	if err != nil {
		return fmt.Errorf("cannot resolve executable path: %w", err)
	}
	home, _ := os.UserHomeDir()
	view := HomeView{Bin: tildeHome(exe, home), Description: homeDescription, Help: homeHelp}
	view.fillRepo(home)
	return output.New(GlobalJSONOutput, GlobalMinOutput, cmd.OutOrStdout()).Print(view, printHomeText)
}

func printHomeText(w io.Writer, data interface{}) {
	v := data.(HomeView)
	fmt.Fprintf(w, "bin: %s\n", v.Bin)
	fmt.Fprintf(w, "description: %s\n", v.Description)
	if v.RepoRoot != "" {
		fmt.Fprintf(w, "repo_root: %s\n", v.RepoRoot)
		fmt.Fprintf(w, "project_type: %s\n", v.ProjectType)
	}
	if v.GitChanges != nil {
		fmt.Fprintf(w, "git_changes: %d\n", *v.GitChanges)
	}
	if v.Repo != "" {
		fmt.Fprintf(w, "repo: %s\n", v.Repo)
	}
}

// tildeHome writes path with a leading home directory as "~". Only a whole
// path element matches, so /home/samx is not shortened under /home/sam.
func tildeHome(path, home string) string {
	if home == "" {
		return path
	}
	home = filepath.Clean(home)
	if path == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
		return "~" + string(filepath.Separator) + rest
	}
	return path
}

// Execute runs the root command
func Execute() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes one invocation and returns its exit status.
func run(args []string, stdout, stderr io.Writer) int {
	root := NewRootCmd()
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	cmd, err := root.ExecuteC()
	if err == nil {
		return 0
	}
	axi, jsonOut, minOut := outputMode(cmd, args)
	output.SetDefaultAXI(axi)
	w := stderr
	if axi || jsonOut {
		w = stdout
	}
	return output.New(jsonOut, minOut, w).PrintError(err)
}

// outputMode reads --axi, --json and --min from args for the error report.
//
// The globals alone are not enough: a usage error stops flag parsing before
// PersistentPreRun publishes the mode, and at the first bad flag, so in
// `grep --nope --axi` cobra never saw --axi. The args are parsed again against
// cmd's flags with unknown flags tolerated, so a flag's value and anything
// after "--" are still not mistaken for a mode flag.
func outputMode(cmd *cobra.Command, args []string) (axi, jsonOut, minOut bool) {
	fs := pflag.NewFlagSet("mode", pflag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.ParseErrorsWhitelist.UnknownFlags = true
	fs.AddFlagSet(cmd.Flags())
	fs.AddFlagSet(cmd.InheritedFlags())
	_ = fs.Parse(args)
	isSet := func(name string) bool {
		f := fs.Lookup(name)
		return f != nil && f.Value.String() == "true"
	}
	return GlobalAXIOutput || isSet("axi"),
		GlobalJSONOutput || isSet("json"),
		GlobalMinOutput || isSet("min")
}

// flagError adds the command's valid flags to an unknown-flag error, so a
// caller can correct the call without a --help round trip. The cobra wording
// is kept at the front: ExitCodeFor classifies it as a usage error by it.
func flagError(cmd *cobra.Command, err error) error {
	msg := err.Error()
	if !strings.HasPrefix(msg, "unknown flag: ") && !strings.HasPrefix(msg, "unknown shorthand flag: ") {
		return err
	}
	var valid []string
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}
		name := "--" + f.Name
		if f.Shorthand != "" {
			name = "-" + f.Shorthand + "/" + name
		}
		valid = append(valid, name)
	})
	return fmt.Errorf("%w; valid flags for %s: %s", err, cmd.CommandPath(), strings.Join(valid, ", "))
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
