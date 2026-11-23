// Command benchmarks measures what --axi (TOON) saves over --json on real
// repositories. It runs the built llm-data with each format on the same
// commands, counts bytes and approximate tokens (bytes/4), and prints a
// markdown table.
//
//	go run ./benchmarks                      # this repo and spf13/cobra from the module cache
//	go run ./benchmarks -o benchmarks/RESULTS.md
//	go run ./benchmarks -bin build/llm-data DIR DIR...
//
// With -perf it times llm-data instead: cold start, one typical command and
// one large input, writing the raw timings to results-performance.json and
// the table to results-performance.md.
//
//	go run ./benchmarks -perf -bin PATH      # BENCH_DIR names the large input; unset, one is generated
//	go run ./benchmarks -perf -from-json     # regenerate the table from the JSON only
//
// Any command that fails, prints nothing, or answers --axi with the JSON
// document stops the run with a non-zero exit and no table: a missing row
// must never read as a measured one.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
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
	// Provenance is the commit the repository was measured at, or why
	// there is none; set only for the written report.
	Provenance string
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
	perf := fs.Bool("perf", false, "time llm-data instead of counting tokens")
	fromJSON := fs.Bool("from-json", false, "with -perf: regenerate the table from the JSON without running anything")
	warmup := fs.Int("warmup", 3, "with -perf: untimed runs per case before the timed ones")
	runs := fs.Int("runs", 20, "with -perf: timed runs per case")
	perfDir := fs.String("perf-dir", "", "with -perf: directory for "+perfJSONName+" and "+perfMDName+" (default: benchmarks/ in this module)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: go run ./benchmarks [-bin PATH] [-o FILE] [REPO_DIR REPO_DIR...]")
		fmt.Fprintln(stderr, "       go run ./benchmarks -perf [-bin PATH] [-warmup N] [-runs N] [-perf-dir DIR] [-from-json]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	var err error
	if *perf {
		if fs.NArg() > 0 || set["o"] {
			fmt.Fprintln(stderr, "benchmarks: -perf takes no repository directories and no -o")
			return 2
		}
		if *fromJSON && (set["bin"] || set["warmup"] || set["runs"]) {
			fmt.Fprintln(stderr, "benchmarks: -from-json runs nothing, so it takes no -bin, -warmup or -runs")
			return 2
		}
		err = benchmarkPerf(perfConfig{bin: *bin, dir: *perfDir, fromJSON: *fromJSON, warmup: *warmup, runs: *runs}, stdout)
	} else {
		for _, name := range []string{"from-json", "warmup", "runs", "perf-dir"} {
			if set[name] {
				fmt.Fprintf(stderr, "benchmarks: -%s needs -perf\n", name)
				return 2
			}
		}
		err = benchmark(*bin, *outFile, fs.Args(), stdout)
	}
	if err != nil {
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

	bin, cleanup, err := resolveBinary(bin)
	defer cleanup()
	if err != nil {
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
	if outFile == "" {
		return nil
	}
	version, err := binaryVersion(bin)
	if err != nil {
		return err
	}
	for i := range repos {
		if repos[i].Provenance, err = provenance(repos[i].Dir); err != nil {
			return err
		}
	}
	return os.WriteFile(outFile, []byte(report(version, repos, table)), 0o644)
}

// binaryVersion is what bin --version prints, for the report.
func binaryVersion(bin string) (string, error) {
	out, err := exec.Command(bin, "--version").Output()
	version := strings.TrimSpace(string(out))
	if err == nil && version == "" {
		err = errors.New("printed nothing")
	}
	if err != nil {
		return "", fmt.Errorf("%s --version: %w", bin, err)
	}
	return version, nil
}

// provenance says where a measured repository's bytes came from: the commit
// of a git checkout, or that the directory is not one (the module cache). The
// committed RESULTS.md was once generated from an uncommitted tree and could
// not be reproduced at any commit, so a checkout with uncommitted changes is
// refused: its commit would name a tree that was not the one measured.
func provenance(dir string) (string, error) {
	git := func(args ...string) (string, error) {
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
		return strings.TrimSpace(string(out)), err
	}
	top, err := git("rev-parse", "--show-toplevel")
	if err != nil || !sameDir(top, dir) {
		return "not a git checkout", nil
	}
	status, err := git("status", "--porcelain")
	if err != nil {
		return "", fmt.Errorf("git status in %s: %w", dir, err)
	}
	if status != "" {
		return "", fmt.Errorf("%s has uncommitted changes, so no commit names what would be measured; run from a clean checkout:\n%s", dir, status)
	}
	commit, err := git("rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD in %s: %w", dir, err)
	}
	return "at commit " + commit, nil
}

// sameDir reports whether a and b are one directory once symlinks resolve.
func sameDir(a, b string) bool {
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}

// resolveBinary returns bin as an absolute path or, when bin is empty,
// builds ./cmd/llm-data into a temp dir that cleanup removes.
func resolveBinary(bin string) (string, func(), error) {
	cleanup := func() {}
	if bin == "" {
		tmp, err := os.MkdirTemp("", "llm-data-bench-")
		if err != nil {
			return "", cleanup, err
		}
		cleanup = func() { os.RemoveAll(tmp) }
		root, err := goListDir()
		if err != nil {
			return "", cleanup, err
		}
		bin = filepath.Join(tmp, "llm-data")
		build := exec.Command("go", "build", "-o", bin, "./cmd/llm-data")
		build.Dir = root
		if out, err := build.CombinedOutput(); err != nil {
			return "", cleanup, fmt.Errorf("building llm-data: %w: %s", err, out)
		}
	}
	abs, err := filepath.Abs(bin)
	return abs, cleanup, err
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

func report(version string, repos []Repo, table string) string {
	var b strings.Builder
	b.WriteString("# Token benchmark: TOON (`--axi`) against JSON (`--json`)\n\n")
	b.WriteString("Generated by `go run ./benchmarks -o benchmarks/RESULTS.md`. Each command runs once with `--json` and once with `--axi`, from inside the repository. Tokens are approximated as bytes/4; no tokenizer is involved. Saved is the share of JSON bytes that TOON does not spend. `tree`, `stats`, `grep` and `multigrep` print paths relative to the working directory by default (`--absolute` restores absolute paths), so their bytes, and the totals, do not shift with where the repository sits on disk.\n\n")
	fmt.Fprintf(&b, "Measured with `llm-data` version `%s`. To reproduce, check out each repository at the commit named below and run the same command with that binary.\n\n", version)
	b.WriteString("Repositories:\n\n")
	for _, r := range repos {
		fmt.Fprintf(&b, "- %s: %s\n", r.Name, r.Provenance)
	}
	b.WriteString("\nCommands:\n\n")
	for _, c := range commands {
		args := make([]string, len(c.Args))
		for i, a := range c.Args {
			args[i] = a
			if strings.Contains(a, " ") {
				args[i] = strconv.Quote(a)
			}
		}
		fmt.Fprintf(&b, "- `%s`: `llm-data %s`\n", c.Name, strings.Join(args, " "))
	}
	b.WriteString("\n")
	b.WriteString(table)
	return b.String()
}

// Performance mode.

const (
	perfJSONName  = "results-performance.json"
	perfMDName    = "results-performance.md"
	minLargeFiles = 3000
	timingMethod  = "wall-clock per process"
)

// PerfCase is one llm-data invocation to time, run in Dir.
type PerfCase struct {
	Name  string
	Args  []string
	Dir   string
	Input string
}

// Machine names the computer the timings were taken on.
type Machine struct {
	CPU      string `json:"cpu"`
	Threads  int    `json:"threads"`
	RAMBytes int64  `json:"ram_bytes"`
	OS       string `json:"os"`
	Kernel   string `json:"kernel"`
}

// Method is how each case was run.
type Method struct {
	Warmup int    `json:"warmup"`
	Runs   int    `json:"runs"`
	Timing string `json:"timing"`
}

// CaseResult holds every raw timing of one case, in nanoseconds.
type CaseResult struct {
	Name      string  `json:"name"`
	Command   string  `json:"command"`
	Input     string  `json:"input"`
	TimingsNS []int64 `json:"timings_ns"`
}

// PerfResults is the whole of results-performance.json.
type PerfResults struct {
	Version string       `json:"version"`
	Date    string       `json:"date"`
	Machine Machine      `json:"machine"`
	Method  Method       `json:"method"`
	Cases   []CaseResult `json:"cases"`
}

// timeCases runs each case warmup times untimed, then runs times timed by
// clock around the whole process. Any failure, empty output, non-positive
// timing, or a case left with no timing is an error and no result.
func timeCases(run Runner, clock func() time.Time, cases []PerfCase, warmup, runs int) ([]CaseResult, error) {
	var results []CaseResult
	for _, c := range cases {
		command := "llm-data " + strings.Join(c.Args, " ")
		check := func(out []byte, err error) error {
			if err != nil {
				return fmt.Errorf("%s (%s): %w", c.Name, command, err)
			}
			if len(bytes.TrimSpace(out)) == 0 {
				return fmt.Errorf("%s (%s): printed nothing", c.Name, command)
			}
			return nil
		}
		for i := 0; i < warmup; i++ {
			if err := check(run(c.Dir, c.Args)); err != nil {
				return nil, err
			}
		}
		r := CaseResult{Name: c.Name, Command: command, Input: c.Input}
		for i := 0; i < runs; i++ {
			start := clock()
			out, err := run(c.Dir, c.Args)
			d := clock().Sub(start)
			if err := check(out, err); err != nil {
				return nil, err
			}
			if d <= 0 {
				return nil, fmt.Errorf("%s: timed run %d measured %v", c.Name, i+1, d)
			}
			r.TimingsNS = append(r.TimingsNS, d.Nanoseconds())
		}
		if len(r.TimingsNS) == 0 {
			return nil, fmt.Errorf("%s: recorded no timings", c.Name)
		}
		results = append(results, r)
	}
	return results, nil
}

// validate refuses results that would render a stamp with a blank field or
// a row with no timing behind it.
func (r PerfResults) validate() error {
	m := r.Machine
	switch {
	case r.Version == "":
		return errors.New("no version")
	case r.Date == "":
		return errors.New("no date")
	case m.CPU == "" || m.Threads < 1 || m.RAMBytes < 1 || m.OS == "" || m.Kernel == "":
		return fmt.Errorf("incomplete machine: %+v", m)
	case r.Method.Runs < 1 || r.Method.Warmup < 0 || r.Method.Timing != timingMethod:
		return fmt.Errorf("bad method: %d warmup, %d runs, timing %q (want %q)", r.Method.Warmup, r.Method.Runs, r.Method.Timing, timingMethod)
	case len(r.Cases) == 0:
		return errors.New("no cases")
	}
	for i, c := range r.Cases {
		if c.Name == "" || c.Command == "" || c.Input == "" {
			return fmt.Errorf("case %d is missing its name, command or input", i+1)
		}
		if len(c.TimingsNS) == 0 {
			return fmt.Errorf("%s: recorded no timings", c.Name)
		}
		if len(c.TimingsNS) != r.Method.Runs {
			return fmt.Errorf("%s: %d timings, but the method states %d runs", c.Name, len(c.TimingsNS), r.Method.Runs)
		}
		for _, ns := range c.TimingsNS {
			if ns <= 0 {
				return fmt.Errorf("%s: non-positive timing %d", c.Name, ns)
			}
		}
	}
	return nil
}

const perfTableHeader = "| Case | Command | Input | Runs | Min (ms) | Median (ms) | Mean (ms) | Max (ms) |\n" +
	"|---|---|---|---:|---:|---:|---:|---:|\n"

// renderPerf turns results into results-performance.md. It reads nothing
// but r, so the JSON alone regenerates the file.
func renderPerf(r PerfResults) (string, error) {
	if err := r.validate(); err != nil {
		return "", err
	}
	m := r.Machine
	var b strings.Builder
	b.WriteString("# Performance: llm-data\n\n")
	fmt.Fprintf(&b, "Generated by `go run ./benchmarks -perf` from the raw timings in `%s`; `go run ./benchmarks -perf -from-json` regenerates this file from that JSON alone. No comparison to another tool is made.\n\n", perfJSONName)
	fmt.Fprintf(&b, "- **Version:** llm-data %s\n", r.Version)
	fmt.Fprintf(&b, "- **Date:** %s\n", r.Date)
	fmt.Fprintf(&b, "- **Machine:** %s, %d threads, %.1f GiB RAM, %s, kernel %s\n",
		m.CPU, m.Threads, float64(m.RAMBytes)/(1<<30), m.OS, m.Kernel)
	fmt.Fprintf(&b, "- **Method:** %d warmup runs, then %d timed runs per case. Each timing is the wall-clock time of one `llm-data` process, from start to exit; runs are sequential.\n\n",
		r.Method.Warmup, r.Method.Runs)
	b.WriteString(perfTableHeader)
	for _, c := range r.Cases {
		ms := make([]float64, len(c.TimingsNS))
		sum := 0.0
		for i, ns := range c.TimingsNS {
			ms[i] = float64(ns) / 1e6
			sum += ms[i]
		}
		sort.Float64s(ms)
		n := len(ms)
		median := (ms[(n-1)/2] + ms[n/2]) / 2
		fmt.Fprintf(&b, "| %s | `%s` | %s | %d | %.2f | %.2f | %.2f | %.2f |\n",
			c.Name, c.Command, c.Input, n, ms[0], median, sum/float64(n), ms[n-1])
	}
	return b.String(), nil
}

type perfConfig struct {
	bin, dir     string
	fromJSON     bool
	warmup, runs int
}

func benchmarkPerf(cfg perfConfig, stdout io.Writer) error {
	if cfg.dir == "" {
		root, err := goListDir()
		if err != nil {
			return err
		}
		cfg.dir = filepath.Join(root, "benchmarks")
	}
	jsonPath := filepath.Join(cfg.dir, perfJSONName)

	var r PerfResults
	var raw []byte
	if cfg.fromJSON {
		b, err := os.ReadFile(jsonPath)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(b, &r); err != nil {
			return fmt.Errorf("%s: %w", jsonPath, err)
		}
	} else {
		var err error
		if r, err = timePerf(cfg); err != nil {
			return err
		}
		if raw, err = json.MarshalIndent(r, "", "  "); err != nil {
			return err
		}
		raw = append(raw, '\n')
	}
	md, err := renderPerf(r)
	if err != nil {
		return err
	}
	if raw != nil {
		if err := os.WriteFile(jsonPath, raw, 0o644); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(cfg.dir, perfMDName), []byte(md), 0o644); err != nil {
		return err
	}
	_, err = io.WriteString(stdout, md)
	return err
}

// timePerf times the three cases: cold start, a typical command in the
// module root, and stats over a directory of at least 3,000 files.
func timePerf(cfg perfConfig) (PerfResults, error) {
	machine, err := detectMachine(runtime.GOOS, runtime.NumCPU(), os.ReadFile, commandOutput)
	if err != nil {
		return PerfResults{}, err
	}
	bin, cleanup, err := resolveBinary(cfg.bin)
	defer cleanup()
	if err != nil {
		return PerfResults{}, err
	}
	root, err := goListDir()
	if err != nil {
		return PerfResults{}, err
	}
	tmp, err := os.MkdirTemp("", "llm-data-perf-")
	if err != nil {
		return PerfResults{}, err
	}
	defer os.RemoveAll(tmp)
	large, label, err := largeInputDir(os.Getenv("BENCH_DIR"), tmp)
	if err != nil {
		return PerfResults{}, err
	}

	run := execRunner(bin)
	version, err := run(root, []string{"--version"})
	if err != nil {
		return PerfResults{}, fmt.Errorf("--version: %w", err)
	}
	cases := []PerfCase{
		{Name: "cold start", Args: []string{"--version"}, Dir: root, Input: "none"},
		{Name: "typical command", Args: []string{"detect", "--path", "."}, Dir: root, Input: "this repository"},
		{Name: "large input", Args: []string{"stats", "--path", "."}, Dir: large, Input: label},
	}
	timed, err := timeCases(run, time.Now, cases, cfg.warmup, cfg.runs)
	if err != nil {
		return PerfResults{}, err
	}
	return PerfResults{
		Version: strings.TrimSpace(string(version)),
		Date:    time.Now().UTC().Format("2006-01-02"),
		Machine: machine,
		Method:  Method{Warmup: cfg.warmup, Runs: cfg.runs, Timing: timingMethod},
		Cases:   timed,
	}, nil
}

// largeInputDir returns benchDir when it holds at least 3,000 files, or,
// when benchDir is empty, generates exactly 3,000 small source files under
// tmp. A BENCH_DIR that is too small is refused, not timed as a large input.
func largeInputDir(benchDir, tmp string) (dir, label string, err error) {
	if benchDir != "" {
		if fi, err := os.Stat(benchDir); err != nil || !fi.IsDir() {
			return "", "", fmt.Errorf("BENCH_DIR %s is not a directory", benchDir)
		}
		n, err := countFiles(benchDir)
		if err != nil {
			return "", "", err
		}
		if n < minLargeFiles {
			return "", "", fmt.Errorf("BENCH_DIR %s holds %d files, need at least %d", benchDir, n, minLargeFiles)
		}
		return benchDir, fmt.Sprintf("%s files (BENCH_DIR)", thousands(n)), nil
	}
	dir = filepath.Join(tmp, "large")
	for i := 0; i < minLargeFiles; i++ {
		sub := filepath.Join(dir, fmt.Sprintf("pkg%02d", i/100))
		if i%100 == 0 {
			if err := os.MkdirAll(sub, 0o755); err != nil {
				return "", "", err
			}
		}
		src := fmt.Sprintf("package pkg%02d\n\n// F%d returns its own number.\nfunc F%d() int {\n\treturn %d\n}\n", i/100, i, i, i)
		if err := os.WriteFile(filepath.Join(sub, fmt.Sprintf("f%04d.go", i)), []byte(src), 0o644); err != nil {
			return "", "", err
		}
	}
	return dir, thousands(minLargeFiles) + " generated files", nil
}

func countFiles(dir string) (int, error) {
	n := 0
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			n++
		}
		return nil
	})
	return n, err
}

func thousands(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// detectMachine reads the machine stamp from /proc and os-release on Linux
// and from sysctl, sw_vers and uname on macOS. A field it cannot read is an
// error: the table must not name a machine it did not identify.
func detectMachine(goos string, threads int, readFile func(string) ([]byte, error), command func(string, ...string) (string, error)) (Machine, error) {
	m := Machine{Threads: threads}
	switch goos {
	case "linux":
		field := func(path, key, sep string) (string, error) {
			b, err := readFile(path)
			if err != nil {
				return "", err
			}
			for _, line := range strings.Split(string(b), "\n") {
				k, v, ok := strings.Cut(line, sep)
				if ok && strings.TrimSpace(k) == key {
					return strings.Trim(strings.TrimSpace(v), `"`), nil
				}
			}
			return "", fmt.Errorf("%s has no %s", path, key)
		}
		var err error
		if m.CPU, err = field("/proc/cpuinfo", "model name", ":"); err != nil {
			return Machine{}, err
		}
		mem, err := field("/proc/meminfo", "MemTotal", ":")
		if err != nil {
			return Machine{}, err
		}
		kb, err := strconv.ParseInt(strings.TrimSuffix(mem, " kB"), 10, 64)
		if err != nil {
			return Machine{}, fmt.Errorf("MemTotal %q: %w", mem, err)
		}
		m.RAMBytes = kb * 1024
		if m.OS, err = field("/etc/os-release", "PRETTY_NAME", "="); err != nil {
			return Machine{}, err
		}
		kernel, err := readFile("/proc/sys/kernel/osrelease")
		if err != nil {
			return Machine{}, err
		}
		m.Kernel = strings.TrimSpace(string(kernel))
	case "darwin":
		out := func(name string, args ...string) (string, error) {
			s, err := command(name, args...)
			if err != nil {
				return "", fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
			}
			return strings.TrimSpace(s), nil
		}
		var err error
		if m.CPU, err = out("sysctl", "-n", "machdep.cpu.brand_string"); err != nil {
			return Machine{}, err
		}
		mem, err := out("sysctl", "-n", "hw.memsize")
		if err != nil {
			return Machine{}, err
		}
		if m.RAMBytes, err = strconv.ParseInt(mem, 10, 64); err != nil {
			return Machine{}, fmt.Errorf("hw.memsize %q: %w", mem, err)
		}
		name, err := out("sw_vers", "-productName")
		if err != nil {
			return Machine{}, err
		}
		version, err := out("sw_vers", "-productVersion")
		if err != nil {
			return Machine{}, err
		}
		m.OS = name + " " + version
		if m.Kernel, err = out("uname", "-r"); err != nil {
			return Machine{}, err
		}
	default:
		return Machine{}, fmt.Errorf("cannot identify the machine on %s; -perf supports linux and darwin", goos)
	}
	return m, nil
}

func commandOutput(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).Output()
	return string(out), err
}
