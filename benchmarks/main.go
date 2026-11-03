// Command benchmarks measures what --axi (TOON) saves over --json on real
// repositories. It runs the built llm-data with each format on the same
// commands, counts bytes and approximate tokens (bytes/4), and prints a
// markdown table.
//
//	go run ./benchmarks                      # this repo and spf13/cobra from the module cache
//	go run ./benchmarks -o benchmarks/RESULTS.md
//	go run ./benchmarks -bin build/llm-data DIR DIR...
//
// Any command that fails, prints nothing, or answers --axi with the JSON
// document stops the run with a non-zero exit and no table: a missing row
// must never read as a measured one.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Command is one llm-data invocation, run from inside the repository with
// paths relative to it, so neither format pays for the checkout location.
type Command struct {
	Name string
	Args []string
}

// commands are read-only and need no git history, so they run the same on a
// clone and on a module-cache copy of a repository.
var commands = []Command{
	{Name: "tree", Args: []string{"tree", "--path", ".", "--depth", "3"}},
	{Name: "stats", Args: []string{"stats", "--path", "."}},
	{Name: "detect", Args: []string{"detect", "--path", "."}},
	{Name: "discover-tests", Args: []string{"discover-tests", "--path", "."}},
	{Name: "grep", Args: []string{"grep", "-n", "^func ", "."}},
	{Name: "multigrep", Args: []string{"multigrep", "--path", ".", "--keywords", "Command,Flags,Error"}},
}

// Repo is a directory the commands run in.
type Repo struct {
	Name string
	Dir  string
}

// Runner runs llm-data with args in dir and returns its stdout.
type Runner func(dir string, args []string) ([]byte, error)

// Row is one command on one repository, measured in both formats.
type Row struct {
	Repo, Command        string
	TOONBytes, JSONBytes int
}

// TOONTokens approximates the token count of the TOON document.
func (r Row) TOONTokens() int { return r.TOONBytes / 4 }

// JSONTokens approximates the token count of the JSON document.
func (r Row) JSONTokens() int { return r.JSONBytes / 4 }

func measure(run Runner, repos []Repo, cmds []Command) ([]Row, error) {
	var rows []Row
	for _, repo := range repos {
		for _, c := range cmds {
			toon, err := runFormat(run, repo, c, "--axi")
			if err != nil {
				return nil, err
			}
			json, err := runFormat(run, repo, c, "--json")
			if err != nil {
				return nil, err
			}
			if bytes.Equal(toon, json) {
				return nil, fmt.Errorf("%s on %s: --axi returned the same document as --json", c.Name, repo.Name)
			}
			rows = append(rows, Row{Repo: repo.Name, Command: c.Name, TOONBytes: len(toon), JSONBytes: len(json)})
		}
	}
	return rows, nil
}

func runFormat(run Runner, repo Repo, c Command, format string) ([]byte, error) {
	args := append(append([]string{}, c.Args...), format)
	out, err := run(repo.Dir, args)
	if err != nil {
		return nil, fmt.Errorf("%s %s on %s: %w", c.Name, format, repo.Name, err)
	}
	if len(bytes.TrimSpace(out)) == 0 {
		return nil, fmt.Errorf("%s %s on %s: printed nothing", c.Name, format, repo.Name)
	}
	return out, nil
}

const tableHeader = "| Repo | Command | JSON bytes | JSON tokens | TOON bytes | TOON tokens | Saved |\n" +
	"|---|---|---:|---:|---:|---:|---:|\n"

func renderTable(rows []Row) string {
	var b strings.Builder
	b.WriteString(tableHeader)
	var total Row
	for _, r := range rows {
		writeRow(&b, r.Repo, "`"+r.Command+"`", r)
		total.TOONBytes += r.TOONBytes
		total.JSONBytes += r.JSONBytes
	}
	writeRow(&b, "**total**", "", total)
	return b.String()
}

func writeRow(b *strings.Builder, repo, cmd string, r Row) {
	saved := 0.0
	if r.JSONBytes > 0 {
		saved = 100 * (1 - float64(r.TOONBytes)/float64(r.JSONBytes))
	}
	fmt.Fprintf(b, "| %s | %s | %d | %d | %d | %d | %.1f%% |\n",
		repo, cmd, r.JSONBytes, r.JSONTokens(), r.TOONBytes, r.TOONTokens(), saved)
}

// execRunner runs bin and folds its stderr into the error, so a failure
// names its own cause.
func execRunner(bin string) Runner {
	return func(dir string, args []string) ([]byte, error) {
		cmd := exec.Command(bin, args...)
		cmd.Dir = dir
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return out, nil
	}
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("benchmarks", flag.ContinueOnError)
	fs.SetOutput(stderr)
	bin := fs.String("bin", "", "llm-data binary to run (default: build ./cmd/llm-data into a temp dir)")
	outFile := fs.String("o", "", "also write the result, with how it was produced, to this file")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: go run ./benchmarks [-bin PATH] [-o FILE] [REPO_DIR REPO_DIR...]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if err := benchmark(*bin, *outFile, fs.Args(), stdout); err != nil {
		fmt.Fprintln(stderr, "benchmarks:", err)
		return 1
	}
	return 0
}

func benchmark(bin, outFile string, dirs []string, stdout io.Writer) error {
	var repos []Repo
	var err error
	if len(dirs) == 0 {
		repos, err = defaultRepos()
	} else {
		repos, err = reposFromDirs(dirs)
	}
	if err != nil {
		return err
	}
	if len(repos) < 2 {
		return fmt.Errorf("need at least two repositories, got %d", len(repos))
	}

	if bin == "" {
		tmp, err := os.MkdirTemp("", "llm-data-bench-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		root, err := goListDir()
		if err != nil {
			return err
		}
		bin = filepath.Join(tmp, "llm-data")
		build := exec.Command("go", "build", "-o", bin, "./cmd/llm-data")
		build.Dir = root
		if out, err := build.CombinedOutput(); err != nil {
			return fmt.Errorf("building llm-data: %w: %s", err, out)
		}
	}
	if bin, err = filepath.Abs(bin); err != nil {
		return err
	}

	rows, err := measure(execRunner(bin), repos, commands)
	if err != nil {
		return err
	}
	table := renderTable(rows)
	if _, err := io.WriteString(stdout, table); err != nil {
		return err
	}
	if outFile != "" {
		return os.WriteFile(outFile, []byte(report(repos, table)), 0o644)
	}
	return nil
}

// defaultRepos are this repository and spf13/cobra at the version go.mod
// pins, read from the module cache: both are on disk wherever this program
// builds, and the second is identical on every machine.
func defaultRepos() ([]Repo, error) {
	self, err := goListDir()
	if err != nil {
		return nil, err
	}
	cobra, err := goListDir("github.com/spf13/cobra")
	if err != nil {
		return nil, err
	}
	return []Repo{
		{Name: "llm-data-axi", Dir: self},
		{Name: "spf13/cobra@" + strings.TrimPrefix(filepath.Base(cobra), "cobra@"), Dir: cobra},
	}, nil
}

func goListDir(module ...string) (string, error) {
	args := append([]string{"list", "-m", "-f", "{{.Dir}}"}, module...)
	out, err := exec.Command("go", args...).Output()
	dir := strings.TrimSpace(string(out))
	if err == nil && dir == "" {
		err = errors.New("not in the module cache; run go mod download")
	}
	if err != nil {
		return "", fmt.Errorf("locating %v: %w", module, err)
	}
	return dir, nil
}

func reposFromDirs(dirs []string) ([]Repo, error) {
	var repos []Repo
	for _, d := range dirs {
		abs, err := filepath.Abs(d)
		if err != nil {
			return nil, err
		}
		if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
			return nil, fmt.Errorf("%s is not a directory", d)
		}
		repos = append(repos, Repo{Name: filepath.Base(abs), Dir: abs})
	}
	return repos, nil
}

func report(repos []Repo, table string) string {
	var b strings.Builder
	b.WriteString("# Token benchmark: TOON (`--axi`) against JSON (`--json`)\n\n")
	b.WriteString("Generated by `go run ./benchmarks -o benchmarks/RESULTS.md`. Each command runs once with `--json` and once with `--axi`, from inside the repository. Tokens are approximated as bytes/4; no tokenizer is involved. Saved is the share of JSON bytes that TOON does not spend. `stats` prints the absolute path of the directory in both formats, so its bytes shift with where the repository sits on disk.\n\n")
	b.WriteString("Repositories:\n\n")
	for _, r := range repos {
		fmt.Fprintf(&b, "- %s\n", r.Name)
	}
	b.WriteString("\nCommands:\n\n")
	for _, c := range commands {
		fmt.Fprintf(&b, "- `%s`: `llm-data %s`\n", c.Name, strings.Join(c.Args, " "))
	}
	b.WriteString("\n")
	b.WriteString(table)
	return b.String()
}
