package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/samestrin/llm-data-axi/v2/internal/data/moved"
	"github.com/samestrin/llm-data-axi/v2/pkg/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// defaultVersion is what Version holds when no ldflags stamped it. It must
// never be a release number: a `go install` build runs no ldflags, and a
// hardcoded "1.0.0" made every such build claim a release it is not.
const defaultVersion = "dev"

// Version is set at build time via ldflags
var Version = defaultVersion

// readBuildInfo is indirected so tests can stand in for the module version Go
// records in the binary.
var readBuildInfo = debug.ReadBuildInfo

// DisplayVersion is the bare version --version prints. A release build's
// ldflags stamp wins. Without one — `go install ...@v1.1.0` runs no ldflags —
// it falls back to the module version Go recorded, so the binary does not
// claim a release it is not. "(devel)" is not a version and leaves the "dev"
// default in place. The leading "v" comes off because release.yml stamps the
// tag name ("v1.1.0") and the version flags print "1.1.0".
func DisplayVersion() string {
	return ResolveVersion(Version)
}

// ResolveVersion applies DisplayVersion's rules to a stamp that is not
// commands.Version: the MCP server has its own ldflags symbol
// (main.serverVersion) and shares this fallback so both binaries report one
// version.
func ResolveVersion(stamped string) string {
	info, ok := readBuildInfo()
	return resolveVersion(stamped, info, ok)
}

// resolveVersion is the pure core: the stamp wins unless it is the "dev"
// default and Go recorded a real module version.
func resolveVersion(stamped string, info *debug.BuildInfo, ok bool) string {
	v := stamped
	if v == defaultVersion && ok && info != nil {
		if mv := info.Main.Version; mv != "" && mv != "(devel)" {
			v = mv
		}
	}
	return strings.TrimPrefix(v, "v")
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
	//
	// It is bound to --axi, and PersistentPreRun then sets it whenever neither
	// --json nor --plain was given: TOON is the default, and --axi an alias.
	GlobalAXIOutput bool

	// GlobalPlainOutput selects the plain text that was the default before
	// TOON was.
	GlobalPlainOutput bool
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
output that costs fewer tokens than JSON.

Output is TOON by default. --json prints JSON and --plain prints plain
text; --axi is accepted and prints the same TOON as no flag. --plain cannot
be combined with --json or --axi. --min shortens --json and --plain output
and has no effect on TOON, which is already token-dense.

tree, stats, multigrep, grep, catfiles, highest and git-context print paths
relative to the working directory; a path outside it stays absolute.
--absolute prints them absolute, and any other command rejects it. repo-root
always prints an absolute root.`,
		Version: DisplayVersion(),
		// No Args validator on purpose: with Args nil, cobra's legacyArgs rejects a
		// stray word as "unknown command" before it looks at --help. cobra.NoArgs
		// would let `llm-data typo --help` print help and exit 0.
		RunE:          runHome,
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Sync local command flags to global vars for error handling
			if f := cmd.Flag("json"); f != nil && f.Changed {
				GlobalJSONOutput = true
			}
			if f := cmd.Flag("min"); f != nil && f.Changed {
				GlobalMinOutput = true
			}
			jsonOn := flagOn(cmd, "json")
			if err := formatConflict(GlobalPlainOutput, jsonOn, GlobalAXIOutput); err != nil {
				return err
			}
			if err := checkAbsolute(cmd.Name(), GlobalAbsolutePaths); err != nil {
				return err
			}
			GlobalAXIOutput = GlobalAXIOutput || (!jsonOn && !GlobalPlainOutput)
			if GlobalAXIOutput {
				// --min is a documented no-op under TOON. Several commands read
				// their own min flag before the formatter and abbreviated TOON's
				// keys, which publishes a second column contract no consumer could
				// predict, so the flag is cleared here once for all of them.
				GlobalMinOutput = false
				if f := cmd.Flags().Lookup("min"); f != nil {
					_ = f.Value.Set("false")
				}
			}
			// Published unconditionally, including when the flag is false, so the
			// package default always matches this invocation.
			output.SetDefaultAXI(GlobalAXIOutput)
			return nil
		},
	}

	// -v, -V and --version print the bare version and nothing else. -V has no
	// flag of its own: pflag would need a long name for it, and that name would
	// be one more accepted spelling. run rewrites it to -v instead (see
	// versionShorthand). The flag is root-local: under a subcommand -v is an
	// unknown flag, not a silently ignored one.
	root.SetVersionTemplate("{{.Version}}\n")
	root.Flags().BoolVarP(&showVersion, "version", "v", false, "Print the version and exit (also -V)")

	// Inherited by every subcommand: cobra looks up the parent chain for one.
	root.SetFlagErrorFunc(flagError)

	// Global persistent flags (available to all subcommands)
	root.PersistentFlags().Bool("no-gitignore", false, "Disable .gitignore filtering")
	root.PersistentFlags().BoolVar(&GlobalJSONOutput, "json", false, "Output as JSON instead of TOON")
	root.PersistentFlags().BoolVar(&GlobalPlainOutput, "plain", false, "Output as plain text instead of TOON")
	root.PersistentFlags().BoolVar(&GlobalMinOutput, "min", false, "Minimal --json or --plain output (no effect on TOON)")
	root.PersistentFlags().BoolVar(&GlobalAXIOutput, "axi", false, "Output as TOON, the default (accepted alias)")
	root.PersistentFlags().BoolVar(&GlobalAbsolutePaths, "absolute", false, absoluteHelp)

	root.AddCommand(
		newCatfilesCmd(),
		newCountCmd(),
		newDetectCmd(),
		newDiscoverTestsCmd(),
		newGitChangesCmd(),
		newGitContextCmd(),
		newGrepCmd(),
		newHighestCmd(),
		newMathCmd(),
		newMultiexistsCmd(),
		newMultigrepCmd(),
		newRepoRootCmd(),
		newRuntimeCmd(),
		newSetupCmd(),
		newStatsCmd(),
		newTemplateCmd(),
		newTreeCmd(),
		newYamlCmd(),
	)
	// Moved commands leave a hidden stub that fails naming the cadence
	// replacement (epic 11.2 T3), instead of a bare "unknown command".
	for name := range moved.Table {
		root.AddCommand(newMovedCmd(name))
	}
	argErrorsAreUsage(root)
	return root
}

// argErrorsAreUsage makes a wrong argument count a usage error (exit 2) for
// every command under c. cobra's validators ("accepts 2 arg(s), received 1")
// return untyped errors, which exited 1 as if the command had run and failed,
// while a missing required flag exited 2. Wrapping the validator types them
// all at once, instead of matching each wording in ExitCodeFor.
func argErrorsAreUsage(c *cobra.Command) {
	if validate := c.Args; validate != nil {
		c.Args = func(cmd *cobra.Command, args []string) error {
			if err := validate(cmd, args); err != nil {
				return &output.UsageError{Msg: err.Error()}
			}
			return nil
		}
	}
	for _, sub := range c.Commands() {
		argErrorsAreUsage(sub)
	}
}

// homeDescription is the one sentence the home view prints.
const homeDescription = "Deterministic commands for reading, searching and reshaping code, configuration and documentation in LLM workflows."

// executablePath is os.Executable, swappable so a test can pin the path the
// home view reports.
var executablePath = os.Executable

// homeGitTimeout bounds each git call the home view makes. A bare run is
// what a session-start hook calls, so a hung git must not hang it. Each call
// has its own budget, so the timeout message is true of the call it names.
var homeGitTimeout = 2 * time.Second

// homeHelp is the next step a bare run offers in TOON and --json. The --plain
// view leaves it out; help there is what --help is for.
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
	var root string
	err := withGitBudget(func(ctx context.Context) (err error) {
		root, err = repoRootOf(ctx, "")
		return err
	})
	if errors.Is(err, errNotARepository) {
		v.Repo = "no repository"
		return
	}
	if err != nil {
		v.Repo = gitUnavailable("git rev-parse", err)
		return
	}
	v.RepoRoot = tildeHome(root, home)
	v.ProjectType = detectInDir(root).Stack

	var changes GitChangesResult
	err = withGitBudget(func(ctx context.Context) (err error) {
		changes, err = gitChangesIn(ctx, root, "", true, false)
		return err
	})
	if err != nil {
		// Never "no repository" here: the root was just found.
		v.Repo = gitUnavailable("git status", err)
		return
	}
	v.GitChanges = &changes.Count
}

// withGitBudget runs one git call under its own homeGitTimeout.
func withGitBudget(call func(ctx context.Context) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), homeGitTimeout)
	defer cancel()
	return call(ctx)
}

func gitUnavailable(call string, err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Sprintf("git unavailable: %s gave no answer within %s", call, homeGitTimeout)
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
//
// A failure is reported on stdout in the run's own format: TOON by default,
// JSON under --json, text under --plain. A caller reads one stream whatever
// happened, and stderr is left to diagnostics.
func run(args []string, stdout, stderr io.Writer) int {
	root := NewRootCmd()
	root.SetArgs(versionShorthand(root, args))
	root.SetOut(stdout)
	root.SetErr(stderr)
	cmd, err := root.ExecuteC()
	if err == nil {
		return 0
	}
	axi, jsonOut, minOut := outputMode(cmd, args)
	output.SetDefaultAXI(axi)
	return output.New(jsonOut, minOut, stdout).PrintError(err)
}

// versionShorthand rewrites -V to -v when the run is the root command, so -V
// prints the version without a flag of its own (see NewRootCmd). Every root
// flag is a bool and the root takes no arguments, so a "-V" token before "--"
// can only be the flag. Under a subcommand args are left alone, and -V there
// stays an unknown flag, as -v does.
func versionShorthand(root *cobra.Command, args []string) []string {
	if cmd, _, err := root.Find(args); err != nil || cmd != root {
		return args
	}
	out := slices.Clone(args)
	for i, a := range out {
		if a == "--" {
			break
		}
		if a == "-V" {
			out[i] = "-v"
		}
	}
	return out
}

// flagOn reports whether cmd's flag name was given and is true. A flag the
// caller did not pass is not a format they asked for, whatever its default.
func flagOn(cmd *cobra.Command, name string) bool {
	f := cmd.Flag(name)
	return f != nil && f.Changed && f.Value.String() == "true"
}

// formatConflict rejects --plain with --json or --axi. Each asks for an
// encoding the other rules out, and printing either would hand one request
// output it did not ask for, with exit 0.
func formatConflict(plain, jsonOn, axi bool) error {
	if !plain {
		return nil
	}
	var others []string
	if jsonOn {
		others = append(others, "--json")
	}
	if axi {
		others = append(others, "--axi")
	}
	if len(others) == 0 {
		return nil
	}
	return &output.UsageError{Msg: fmt.Sprintf(
		"conflicting output flags: --plain cannot be combined with %s; pass one of --plain, --json or --axi (TOON is the default)",
		strings.Join(others, " or "))}
}

// outputMode reads --axi, --json, --plain and --min from args for the error
// report. With none of the format flags the mode is TOON, the default.
//
// The globals alone are not enough: a usage error stops flag parsing before
// PersistentPreRun publishes the mode, and at the first bad flag, so in
// `grep --nope --axi` cobra never saw --axi. The args are parsed again against
// cmd's flags with unknown flags tolerated, so a flag's value and anything
// after "--" are still not mistaken for a mode flag. Each flag is copied with
// a value that accepts anything, so a value the real flag refused (the error
// being reported) does not stop the parse before a later --json.
func outputMode(cmd *cobra.Command, args []string) (axi, jsonOut, minOut bool) {
	fs := pflag.NewFlagSet("mode", pflag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.ParseErrorsWhitelist.UnknownFlags = true
	copyFlag := func(f *pflag.Flag) {
		if fs.Lookup(f.Name) == nil {
			fs.AddFlag(&pflag.Flag{Name: f.Name, Shorthand: f.Shorthand, NoOptDefVal: f.NoOptDefVal, Value: &anyValue{typ: f.Value.Type()}})
		}
	}
	cmd.Flags().VisitAll(copyFlag)
	cmd.InheritedFlags().VisitAll(copyFlag)
	_ = fs.Parse(args)
	isSet := func(name string) bool {
		f := fs.Lookup(name)
		return f != nil && f.Value.String() == "true"
	}
	jsonOut = GlobalJSONOutput || isSet("json")
	plain := GlobalPlainOutput || isSet("plain")
	return GlobalAXIOutput || isSet("axi") || (!jsonOut && !plain),
		jsonOut,
		GlobalMinOutput || isSet("min")
}

// anyValue is a flag value that accepts any string, for outputMode's re-read.
type anyValue struct{ val, typ string }

func (v *anyValue) Set(s string) error { v.val = s; return nil }
func (v *anyValue) String() string     { return v.val }
func (v *anyValue) Type() string       { return v.typ }

// flagError rewrites the two flag errors a caller can act on.
//
// An unknown flag gains the command's valid flags, in the message and as a
// valid_flags list, so a caller can correct the call without a --help round
// trip. The cobra wording is kept at the front: ExitCodeFor classifies it as
// a usage error by it.
//
// A bad value for a typed flag says what the flag takes, instead of naming
// the Go parser that refused it (see invalidValue).
func flagError(cmd *cobra.Command, err error) error {
	msg := err.Error()
	if bad := invalidValue(cmd, msg); bad != nil {
		return bad
	}
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
	return &unknownFlagError{err: err, cmdPath: cmd.CommandPath(), valid: valid}
}

// unknownFlagError is an unknown flag with the flags the command does accept.
type unknownFlagError struct {
	err     error
	cmdPath string
	valid   []string
}

func (e *unknownFlagError) Error() string {
	return fmt.Sprintf("%v; valid flags for %s: %s", e.err, e.cmdPath, strings.Join(e.valid, ", "))
}

func (e *unknownFlagError) Unwrap() error { return e.err }

// ErrorFields puts the valid flags next to the message in TOON and JSON.
func (e *unknownFlagError) ErrorFields() map[string]any {
	return map[string]any{"valid_flags": e.valid}
}

// invalidValuePattern is pflag's error for a value its parser refused:
// invalid argument "abc" for "--start" flag: strconv.ParseInt: parsing ...
// The flag is "--name", or "-s, --name" when it has a shorthand.
var invalidValuePattern = regexp.MustCompile(`^invalid argument (".*") for "(?:-., )?--([^"]+)" flag: (.*)$`)

// valueWants says what each built-in flag type takes. A type not listed
// (a custom flag.Value) keeps its own reason, which it wrote for a person.
var valueWants = map[string]string{
	"bool":     "true or false",
	"int":      "an integer",
	"int8":     "an integer",
	"int16":    "an integer",
	"int32":    "an integer",
	"int64":    "an integer",
	"uint":     "a non-negative integer",
	"uint8":    "a non-negative integer",
	"uint16":   "a non-negative integer",
	"uint32":   "a non-negative integer",
	"uint64":   "a non-negative integer",
	"float32":  "a number",
	"float64":  "a number",
	"duration": "a duration such as 30s or 5m",
}

// invalidValue rewrites pflag's invalid-value error to name the flag, the
// value and what the flag takes, as a usage error. It returns nil for any
// other message, or one it cannot read, which then passes through as cobra
// wrote it (still exit 2: ExitCodeFor matches the original wording).
func invalidValue(cmd *cobra.Command, msg string) error {
	m := invalidValuePattern.FindStringSubmatch(msg)
	if m == nil {
		return nil
	}
	value, err := strconv.Unquote(m[1])
	if err != nil {
		return nil
	}
	name, reason := m[2], m[3]
	if f := cmd.Flags().Lookup(name); f != nil {
		if want, ok := valueWants[f.Value.Type()]; ok {
			reason = "want " + want
			if strings.HasSuffix(m[3], "value out of range") {
				reason = strings.TrimPrefix(strings.TrimPrefix(want, "a "), "an ") + " out of range"
			}
		}
	}
	return &output.UsageError{Msg: fmt.Sprintf("invalid value %q for --%s: %s", value, name, reason)}
}
