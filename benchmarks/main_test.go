package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeRunner answers by output format, so a test controls both documents
// without exec'ing anything.
func fakeRunner(toon, json string, err error) Runner {
	return func(dir string, args []string) ([]byte, error) {
		if err != nil {
			return nil, err
		}
		for _, a := range args {
			if a == "--axi" {
				return []byte(toon), nil
			}
		}
		return []byte(json), nil
	}
}

var oneRepo = []Repo{{Name: "r", Dir: "."}}
var oneCmd = []Command{{Name: "stats", Args: []string{"stats", "--path", "."}}}

// Defends the core of the task: each row carries the bytes of both formats
// and tokens as bytes/4, measured on the same command.
func TestMeasureCountsBytesAndTokensPerFormat(t *testing.T) {
	rows, err := measure(fakeRunner("abcdefgh", "abcdefghijklmnop", nil), oneRepo, oneCmd)
	if err != nil {
		t.Fatalf("measure: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	r := rows[0]
	if r.TOONBytes != 8 || r.JSONBytes != 16 {
		t.Fatalf("bytes = toon %d json %d, want 8 and 16", r.TOONBytes, r.JSONBytes)
	}
	if r.TOONTokens() != 2 || r.JSONTokens() != 4 {
		t.Fatalf("tokens = toon %d json %d, want 2 and 4", r.TOONTokens(), r.JSONTokens())
	}
}

// Defends against both formats being asked for in one run: the runner must
// see --axi on one call and --json on the other, never both.
func TestMeasurePassesOneFormatFlagPerRun(t *testing.T) {
	var calls [][]string
	run := func(dir string, args []string) ([]byte, error) {
		calls = append(calls, args)
		return []byte(strings.Join(args, " ")), nil
	}
	if _, err := measure(run, oneRepo, oneCmd); err != nil {
		t.Fatalf("measure: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("got %d calls, want 2", len(calls))
	}
	var sawAXI, sawJSON bool
	for _, c := range calls {
		j := strings.Join(c, " ")
		hasAXI, hasJSON := strings.Contains(j, "--axi"), strings.Contains(j, "--json")
		if hasAXI == hasJSON {
			t.Fatalf("call %q must carry exactly one of --axi and --json", j)
		}
		sawAXI = sawAXI || hasAXI
		sawJSON = sawJSON || hasJSON
	}
	if !sawAXI || !sawJSON {
		t.Fatalf("want one --axi and one --json call, got %v", calls)
	}
}

// Defends the "Done when": a command that fails yields an error, not a row.
func TestMeasureFailsOnCommandError(t *testing.T) {
	// Only --axi fails, so a run that drops the TOON error and goes on to
	// measure JSON cannot pass.
	failAXI := func(dir string, args []string) ([]byte, error) {
		if args[len(args)-1] == "--axi" {
			return nil, errors.New("exit status 1")
		}
		return []byte("{}"), nil
	}
	rows, err := measure(failAXI, oneRepo, oneCmd)
	if err == nil {
		t.Fatal("measure returned no error for a failing command")
	}
	if rows != nil {
		t.Fatalf("measure returned rows alongside an error: %v", rows)
	}
}

// Defends against an empty document being measured as a 0-byte win.
func TestMeasureFailsOnEmptyOutput(t *testing.T) {
	if _, err := measure(fakeRunner("", "{}", nil), oneRepo, oneCmd); err == nil {
		t.Fatal("measure accepted an empty TOON document")
	}
	if _, err := measure(fakeRunner("a: 1", "  \n", nil), oneRepo, oneCmd); err == nil {
		t.Fatal("measure accepted a blank JSON document")
	}
}

// Defends against the bug this repo already shipped once: --axi answered
// with JSON. Identical documents would read as a 0% saving, not a failure.
func TestMeasureFailsWhenAXIReturnsTheJSONDocument(t *testing.T) {
	_, err := measure(fakeRunner(`{"a":1}`, `{"a":1}`, nil), oneRepo, oneCmd)
	if err == nil || !strings.Contains(err.Error(), "--axi") {
		t.Fatalf("want an error naming --axi, got %v", err)
	}
}

// Defends the table shape RESULTS.md promises: a bytes and a tokens column
// for each format, one row per repo and command, and a total.
func TestRenderTable(t *testing.T) {
	rows := []Row{
		{Repo: "a", Command: "stats", TOONBytes: 40, JSONBytes: 100},
		{Repo: "b", Command: "tree", TOONBytes: 60, JSONBytes: 100},
	}
	got := renderTable(rows)
	want := tableHeader +
		"| a | `stats` | 100 | 25 | 40 | 10 | 60.0% |\n" +
		"| b | `tree` | 100 | 25 | 60 | 15 | 40.0% |\n" +
		"| **total** |  | 200 | 50 | 100 | 25 | 50.0% |\n"
	if got != want {
		t.Fatalf("table mismatch\n got:\n%s\nwant:\n%s", got, want)
	}
}

// Defends the "Done when" that a failing command exits non-zero and prints
// no row, through the real exec path with a binary that always fails.
func TestRunExitsNonZeroWhenTheBinaryFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as the binary")
	}
	bin := filepath.Join(t.TempDir(), "llm-data")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho boom >&2\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := run([]string{"-bin", bin, t.TempDir(), t.TempDir()}, &out, &errOut)
	if code == 0 {
		t.Fatal("run exited 0 for a failing command")
	}
	if strings.Contains(out.String(), "|") {
		t.Fatalf("run printed a table row for a failing command:\n%s", out.String())
	}
	if !strings.Contains(errOut.String(), "boom") {
		t.Fatalf("stderr does not carry the command's own error: %q", errOut.String())
	}
}

// Defends the happy path through exec: a binary answering both formats
// produces the table on stdout and writes it to -o.
func TestRunPrintsAndWritesTheTable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as the binary")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "llm-data")
	script := "#!/bin/sh\nfor a in \"$@\"; do [ \"$a\" = --axi ] && { echo 'a: 1'; exit 0; }; done\necho '{\"a\": 1}'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	outFile := filepath.Join(dir, "RESULTS.md")
	var out, errOut bytes.Buffer
	if code := run([]string{"-bin", bin, "-o", outFile, dir, dir}, &out, &errOut); code != 0 {
		t.Fatalf("run exited %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), tableHeader) {
		t.Fatalf("stdout has no table:\n%s", out.String())
	}
	if n := strings.Count(out.String(), "| `"); n != 2*len(commands) {
		t.Fatalf("got %d command rows, want %d", n, 2*len(commands))
	}
	written, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("-o file not written: %v", err)
	}
	if !strings.Contains(string(written), out.String()) {
		t.Fatal("-o file does not hold the printed table")
	}
}

// Defends against a single-repo run passing as the benchmark the task asks for.
// The binary is one that would succeed, so only the repo count can fail it.
func TestRunRefusesFewerThanTwoRepos(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as the binary")
	}
	bin := filepath.Join(t.TempDir(), "llm-data")
	script := "#!/bin/sh\nfor a in \"$@\"; do [ \"$a\" = --axi ] && { echo 'a: 1'; exit 0; }; done\necho '{\"a\": 1}'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"-bin", bin, t.TempDir()}, &out, &errOut); code == 0 {
		t.Fatal("run accepted one repository")
	}
	if !strings.Contains(errOut.String(), "at least two") {
		t.Fatalf("stderr does not say why: %q", errOut.String())
	}
}

// Defends the committed result against drifting from the program: RESULTS.md
// must carry the table the program renders, header included.
func TestCommittedResultsCarryTheTable(t *testing.T) {
	b, err := os.ReadFile("RESULTS.md")
	if err != nil {
		t.Fatalf("RESULTS.md: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, tableHeader) {
		t.Fatal("RESULTS.md does not carry the table header")
	}
	// Since epic 11.2 the path-printing commands are relative to the working
	// directory by default, so the totals no longer shift with the checkout
	// location; the report must say that, not the old absolute-path caveat.
	// (This assertion deliberately replaces the one that pinned the caveat:
	// the behaviour it defended was removed, not the test weakened.)
	if !strings.Contains(s, "print paths relative to the working directory by default") {
		t.Fatal("RESULTS.md does not say paths are relative by default")
	}
	if strings.Contains(s, "print absolute paths") {
		t.Fatal("RESULTS.md still carries the absolute-path caveat the relative default removed")
	}
	for _, c := range commands {
		if !strings.Contains(s, "| `"+c.Name+"` |") {
			t.Fatalf("RESULTS.md has no row for %s", c.Name)
		}
	}
}

// Defends the whole path against the real binary: with no -bin the program
// builds ./cmd/llm-data from the module root (the test runs in benchmarks/,
// so a cwd-relative build would fail here) and every command answers both
// formats with different, non-empty documents.
func TestRunBuildsAndMeasuresTheRealBinary(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{t.TempDir(), t.TempDir()}, &out, &errOut); code != 0 {
		t.Fatalf("run exited %d: %s", code, errOut.String())
	}
	if n := strings.Count(out.String(), "| `"); n != 2*len(commands) {
		t.Fatalf("got %d command rows, want %d:\n%s", n, 2*len(commands), out.String())
	}
}

// Defends the default repositories: this module and the cobra version go.mod
// pins, both resolved to directories that exist.
func TestDefaultRepos(t *testing.T) {
	repos, err := defaultRepos()
	if err != nil {
		t.Fatalf("defaultRepos: %v", err)
	}
	if len(repos) != 2 || repos[0].Name != "llm-data-axi" || repos[1].Name != "spf13/cobra@v1.8.0" {
		t.Fatalf("unexpected repos: %+v", repos)
	}
	for _, r := range repos {
		if fi, err := os.Stat(r.Dir); err != nil || !fi.IsDir() {
			t.Fatalf("%s: %s is not a directory", r.Name, r.Dir)
		}
	}
}

// Defends against a mistyped repo path being measured as an empty repository.
func TestRunRejectsAMissingRepoDir(t *testing.T) {
	var out, errOut bytes.Buffer
	missing := filepath.Join(t.TempDir(), "nope")
	if code := run([]string{"-bin", "/nonexistent", t.TempDir(), missing}, &out, &errOut); code == 0 {
		t.Fatal("run accepted a missing directory")
	}
	if !strings.Contains(errOut.String(), "not a directory") {
		t.Fatalf("stderr does not name the bad path: %q", errOut.String())
	}
}

// Defends against an unknown flag being ignored.
func TestRunRejectsAnUnknownFlag(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"-nope"}, &out, &errOut); code == 0 {
		t.Fatal("run accepted an unknown flag")
	}
}

// fakeClock advances one millisecond on every reading, so each timed run
// measures exactly 1ms and a test can count readings.
func fakeClock() func() time.Time {
	t := time.Unix(0, 0)
	return func() time.Time {
		t = t.Add(time.Millisecond)
		return t
	}
}

var perfCases = []PerfCase{
	{Name: "cold start", Args: []string{"--version"}, Dir: ".", Input: "none"},
	{Name: "large input", Args: []string{"stats", "--path", "."}, Dir: ".", Input: "3,000 files"},
}

func okRunner(calls *int) Runner {
	return func(dir string, args []string) ([]byte, error) {
		*calls++
		return []byte("ok\n"), nil
	}
}

// Defends the method the table states: warmup runs are made and discarded,
// and every timed run is kept as a raw timing.
func TestTimeCasesKeepsEveryTimedRunAndDiscardsWarmup(t *testing.T) {
	var calls int
	got, err := timeCases(okRunner(&calls), fakeClock(), perfCases, 2, 3)
	if err != nil {
		t.Fatalf("timeCases: %v", err)
	}
	if calls != 2*(2+3) {
		t.Fatalf("got %d runs, want %d (warmup and timed, per case)", calls, 2*(2+3))
	}
	if len(got) != 2 {
		t.Fatalf("got %d cases, want 2", len(got))
	}
	for _, c := range got {
		if len(c.TimingsNS) != 3 {
			t.Fatalf("%s: got %d timings, want 3", c.Name, len(c.TimingsNS))
		}
		for _, ns := range c.TimingsNS {
			if ns != int64(time.Millisecond) {
				t.Fatalf("%s: timing %d, want %d", c.Name, ns, time.Millisecond)
			}
		}
	}
	if got[1].Command != "llm-data stats --path ." || got[1].Input != "3,000 files" {
		t.Fatalf("case not carried through: %+v", got[1])
	}
}

// Defends the "Done when": a case that records zero timings fails the run
// instead of rendering an empty or zero row.
func TestTimeCasesFailsWhenACaseRecordsNoTimings(t *testing.T) {
	var calls int
	_, err := timeCases(okRunner(&calls), fakeClock(), perfCases, 1, 0)
	if err == nil || !strings.Contains(err.Error(), "no timings") {
		t.Fatalf("want an error naming no timings, got %v", err)
	}
}

// Defends against a failing or silent command being timed as a fast one,
// in a warmup run as much as in a timed run.
func TestTimeCasesFailsOnCommandErrorOrEmptyOutput(t *testing.T) {
	failing := func(dir string, args []string) ([]byte, error) { return nil, errors.New("exit status 3") }
	if _, err := timeCases(failing, fakeClock(), perfCases, 0, 1); err == nil {
		t.Fatal("timeCases accepted a failing command")
	}
	if _, err := timeCases(failing, fakeClock(), perfCases, 1, 1); err == nil {
		t.Fatal("timeCases accepted a command failing in warmup")
	}
	silent := func(dir string, args []string) ([]byte, error) { return []byte(" \n"), nil }
	_, err := timeCases(silent, fakeClock(), perfCases, 0, 1)
	if err == nil || !strings.Contains(err.Error(), "printed nothing") {
		t.Fatalf("want an error naming the empty output, got %v", err)
	}
}

// Defends against a clock that did not move being recorded as a 0ns run.
func TestTimeCasesFailsOnANonPositiveTiming(t *testing.T) {
	still := func() time.Time { return time.Unix(0, 0) }
	var calls int
	if _, err := timeCases(okRunner(&calls), still, perfCases, 0, 1); err == nil {
		t.Fatal("timeCases accepted a zero timing")
	}
}

func sampleResults() PerfResults {
	return PerfResults{
		Version: "1.1.0",
		Date:    "2026-11-17",
		Machine: Machine{CPU: "Test CPU", Threads: 8, RAMBytes: 16 << 30, OS: "Test OS 1", Kernel: "6.1.0"},
		Method:  Method{Warmup: 2, Runs: 4, Timing: timingMethod},
		Cases: []CaseResult{
			{Name: "cold start", Command: "llm-data --version", Input: "none", TimingsNS: []int64{4e6, 1e6, 3e6, 2e6}},
			{Name: "large input", Command: "llm-data stats --path .", Input: "3,000 files", TimingsNS: []int64{10e6, 30e6, 20e6, 40e6}},
		},
	}
}

// Defends the stamp lines the "Done when" asks for: version, date, machine
// (CPU, threads, RAM, OS, kernel) and method (warmup, runs, wall-clock per
// process), and the table's statistics computed from the raw timings.
func TestRenderPerfCarriesTheStampLinesAndStatistics(t *testing.T) {
	md, err := renderPerf(sampleResults())
	if err != nil {
		t.Fatalf("renderPerf: %v", err)
	}
	for _, want := range []string{
		"- **Version:** llm-data 1.1.0\n",
		"- **Date:** 2026-11-17\n",
		"- **Machine:** Test CPU, 8 threads, 16.0 GiB RAM, Test OS 1, kernel 6.1.0\n",
		"- **Method:** 2 warmup runs, then 4 timed runs per case. Each timing is the wall-clock time of one `llm-data` process, from start to exit; runs are sequential.",
		perfTableHeader,
		"| cold start | `llm-data --version` | none | 4 | 1.00 | 2.50 | 2.50 | 4.00 |\n",
		"| large input | `llm-data stats --path .` | 3,000 files | 4 | 10.00 | 25.00 | 25.00 | 40.00 |\n",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("rendered table lacks %q:\n%s", want, md)
		}
	}
	if strings.Contains(strings.ToLower(md), " vs ") {
		t.Fatal("the table compares against another tool")
	}
}

// Defends the "Done when": rendering from JSON whose case holds no timings,
// or whose stamp is missing, is an error rather than a table.
func TestRenderPerfRejectsIncompleteResults(t *testing.T) {
	for name, mutate := range map[string]func(*PerfResults){
		"no timings":     func(r *PerfResults) { r.Cases[0].TimingsNS = nil },
		"short of runs":  func(r *PerfResults) { r.Cases[1].TimingsNS = r.Cases[1].TimingsNS[:2] },
		"zero timing":    func(r *PerfResults) { r.Cases[0].TimingsNS[0] = 0 },
		"no cases":       func(r *PerfResults) { r.Cases = nil },
		"no version":     func(r *PerfResults) { r.Version = "" },
		"no date":        func(r *PerfResults) { r.Date = "" },
		"no cpu":         func(r *PerfResults) { r.Machine.CPU = "" },
		"no threads":     func(r *PerfResults) { r.Machine.Threads = 0 },
		"no ram":         func(r *PerfResults) { r.Machine.RAMBytes = 0 },
		"no os":          func(r *PerfResults) { r.Machine.OS = "" },
		"no kernel":      func(r *PerfResults) { r.Machine.Kernel = "" },
		"zero runs":      func(r *PerfResults) { r.Method.Runs = 0 },
		"negative warm":  func(r *PerfResults) { r.Method.Warmup = -1 },
		"other method":   func(r *PerfResults) { r.Method.Timing = "user CPU" },
		"no case input":  func(r *PerfResults) { r.Cases[0].Input = "" },
		"no case name":   func(r *PerfResults) { r.Cases[0].Name = "" },
		"no case comand": func(r *PerfResults) { r.Cases[0].Command = "" },
	} {
		r := sampleResults()
		mutate(&r)
		if _, err := renderPerf(r); err == nil {
			t.Fatalf("%s: renderPerf accepted incomplete results", name)
		}
	}
}

func writeScript(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as the binary")
	}
	bin := filepath.Join(t.TempDir(), "llm-data")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

// Defends the "Done when" that one command regenerates the table from the
// JSON: -perf -from-json rewrites the markdown from the raw timings without
// running anything.
func TestRunPerfFromJSONRegeneratesTheTable(t *testing.T) {
	dir := t.TempDir()
	r := sampleResults()
	b, _ := json.Marshal(r)
	if err := os.WriteFile(filepath.Join(dir, perfJSONName), b, 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"-perf", "-from-json", "-perf-dir", dir}, &out, &errOut); code != 0 {
		t.Fatalf("run exited %d: %s", code, errOut.String())
	}
	got, err := os.ReadFile(filepath.Join(dir, perfMDName))
	if err != nil {
		t.Fatalf("markdown not written: %v", err)
	}
	want, _ := renderPerf(r)
	if string(got) != want {
		t.Fatalf("regenerated table differs\n got:\n%s\nwant:\n%s", got, want)
	}
}

// Defends AC5's failure clause: a JSON case with no timings exits non-zero,
// says so, and leaves the markdown as it was.
func TestRunPerfFromJSONFailsOnACaseWithNoTimings(t *testing.T) {
	dir := t.TempDir()
	r := sampleResults()
	r.Cases[1].TimingsNS = []int64{}
	b, _ := json.Marshal(r)
	if err := os.WriteFile(filepath.Join(dir, perfJSONName), b, 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"-perf", "-from-json", "-perf-dir", dir}, &out, &errOut); code == 0 {
		t.Fatal("run exited 0 for a case with no timings")
	}
	if !strings.Contains(errOut.String(), "no timings") {
		t.Fatalf("stderr does not say why: %q", errOut.String())
	}
	if _, err := os.Stat(filepath.Join(dir, perfMDName)); !os.IsNotExist(err) {
		t.Fatal("a markdown table was written for incomplete results")
	}
}

// Defends against a missing or malformed JSON being read as no results.
func TestRunPerfFromJSONFailsOnAMissingOrBadFile(t *testing.T) {
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	if code := run([]string{"-perf", "-from-json", "-perf-dir", dir}, &out, &errOut); code == 0 {
		t.Fatal("run exited 0 with no JSON file")
	}
	if err := os.WriteFile(filepath.Join(dir, perfJSONName), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"-perf", "-from-json", "-perf-dir", dir}, &out, &errOut); code == 0 {
		t.Fatal("run exited 0 on malformed JSON")
	}
}

// Defends the whole timing path through exec: three cases, every raw timing
// in the JSON, and the table on stdout and in the markdown.
func TestRunPerfWritesJSONAndTable(t *testing.T) {
	bin := writeScript(t, "echo 9.9.9\n")
	dir := t.TempDir()
	t.Setenv("BENCH_DIR", "")
	var out, errOut bytes.Buffer
	if code := run([]string{"-perf", "-bin", bin, "-warmup", "1", "-runs", "2", "-perf-dir", dir}, &out, &errOut); code != 0 {
		t.Fatalf("run exited %d: %s", code, errOut.String())
	}
	b, err := os.ReadFile(filepath.Join(dir, perfJSONName))
	if err != nil {
		t.Fatalf("JSON not written: %v", err)
	}
	var r PerfResults
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatalf("JSON does not parse: %v", err)
	}
	if r.Version != "9.9.9" || r.Method.Warmup != 1 || r.Method.Runs != 2 {
		t.Fatalf("stamp not recorded: %+v", r)
	}
	names := []string{}
	for _, c := range r.Cases {
		names = append(names, c.Name)
		if len(c.TimingsNS) != 2 {
			t.Fatalf("%s: got %d timings, want 2", c.Name, len(c.TimingsNS))
		}
	}
	if strings.Join(names, ",") != "cold start,typical command,large input" {
		t.Fatalf("cases = %v", names)
	}
	if r.Cases[2].Input != "3,000 generated files" {
		t.Fatalf("large input = %q", r.Cases[2].Input)
	}
	md, err := os.ReadFile(filepath.Join(dir, perfMDName))
	if err != nil {
		t.Fatalf("markdown not written: %v", err)
	}
	if want, _ := renderPerf(r); string(md) != want || out.String() != want {
		t.Fatalf("markdown or stdout is not the table rendered from the JSON:\n%s", md)
	}
}

// Defends against a failing binary leaving a fresh-looking result behind.
func TestRunPerfFailsAndWritesNothingWhenTheBinaryFails(t *testing.T) {
	bin := writeScript(t, "echo boom >&2\nexit 3\n")
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	if code := run([]string{"-perf", "-bin", bin, "-perf-dir", dir}, &out, &errOut); code == 0 {
		t.Fatal("run exited 0 for a failing binary")
	}
	if !strings.Contains(errOut.String(), "boom") {
		t.Fatalf("stderr does not carry the command's own error: %q", errOut.String())
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("files written for a failed run: %v", entries)
	}
}

// Defends AC5's failure clause end to end: -runs 0 records no timing, so the
// run exits non-zero and writes nothing.
func TestRunPerfFailsWithZeroRuns(t *testing.T) {
	bin := writeScript(t, "echo 9.9.9\n")
	dir := t.TempDir()
	t.Setenv("BENCH_DIR", "")
	var out, errOut bytes.Buffer
	if code := run([]string{"-perf", "-bin", bin, "-runs", "0", "-perf-dir", dir}, &out, &errOut); code == 0 {
		t.Fatal("run exited 0 with zero runs")
	}
	if !strings.Contains(errOut.String(), "no timings") {
		t.Fatalf("stderr does not say why: %q", errOut.String())
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("files written for a failed run: %v", entries)
	}
}

// Defends against flags that would be silently ignored in perf mode.
func TestRunPerfRejectsMisusedFlags(t *testing.T) {
	for _, args := range [][]string{
		{"-from-json"},
		{"-perf", "somedir"},
		{"-perf", "-o", "x.md"},
		{"-perf", "-from-json", "-bin", "x"},
		{"-perf", "-from-json", "-runs", "5"},
		{"-perf", "-from-json", "-warmup", "1"},
	} {
		var out, errOut bytes.Buffer
		if code := run(args, &out, &errOut); code != 2 {
			t.Fatalf("%v: exit %d, want 2", args, code)
		}
	}
}

// Defends the large-input case: a generated directory has at least 3,000
// files, and a BENCH_DIR with fewer, or that is not a directory, is refused
// rather than timed as a large input.
func TestLargeInputDir(t *testing.T) {
	dir, label, err := largeInputDir("", t.TempDir())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if n, err := countFiles(dir); err != nil || n < minLargeFiles {
		t.Fatalf("generated %d files (%v), want at least %d", n, err, minLargeFiles)
	}
	if label != "3,000 generated files" {
		t.Fatalf("label = %q", label)
	}
	if _, _, err := largeInputDir(t.TempDir(), t.TempDir()); err == nil || !strings.Contains(err.Error(), "BENCH_DIR") {
		t.Fatalf("want a BENCH_DIR error for an empty directory, got %v", err)
	}
	if _, _, err := largeInputDir(filepath.Join(t.TempDir(), "nope"), t.TempDir()); err == nil {
		t.Fatal("accepted a missing BENCH_DIR")
	}
	got, label, err := largeInputDir(dir, t.TempDir())
	if err != nil || got != dir || label != "3,000 files (BENCH_DIR)" {
		t.Fatalf("BENCH_DIR = %q %q %v", got, label, err)
	}
}

// Defends the machine stamp on Linux: every field read from /proc and
// os-release, and a missing one is an error, not an empty cell.
func TestDetectMachineLinux(t *testing.T) {
	files := map[string]string{
		"/proc/cpuinfo":              "processor\t: 0\nmodel name\t: Test CPU @ 3.00GHz\n",
		"/proc/meminfo":              "MemTotal:       16384000 kB\nMemFree: 1 kB\n",
		"/etc/os-release":            "NAME=X\nPRETTY_NAME=\"Test OS 1\"\n",
		"/proc/sys/kernel/osrelease": "6.1.0-test\n",
	}
	read := func(p string) ([]byte, error) {
		if s, ok := files[p]; ok {
			return []byte(s), nil
		}
		return nil, os.ErrNotExist
	}
	m, err := detectMachine("linux", 8, read, nil)
	if err != nil {
		t.Fatalf("detectMachine: %v", err)
	}
	want := Machine{CPU: "Test CPU @ 3.00GHz", Threads: 8, RAMBytes: 16384000 * 1024, OS: "Test OS 1", Kernel: "6.1.0-test"}
	if m != want {
		t.Fatalf("got %+v, want %+v", m, want)
	}
	for p := range files {
		saved := files[p]
		delete(files, p)
		if _, err := detectMachine("linux", 8, read, nil); err == nil {
			t.Fatalf("detectMachine accepted a missing %s", p)
		}
		files[p] = saved
	}
}

// Defends the machine stamp on macOS, where the operator may run the script.
func TestDetectMachineDarwin(t *testing.T) {
	out := map[string]string{
		"sysctl -n machdep.cpu.brand_string": "Apple M2\n",
		"sysctl -n hw.memsize":               "17179869184\n",
		"sw_vers -productName":               "macOS\n",
		"sw_vers -productVersion":            "15.1\n",
		"uname -r":                           "24.1.0\n",
	}
	command := func(name string, args ...string) (string, error) {
		if s, ok := out[strings.Join(append([]string{name}, args...), " ")]; ok {
			return s, nil
		}
		return "", errors.New("not found")
	}
	m, err := detectMachine("darwin", 8, nil, command)
	if err != nil {
		t.Fatalf("detectMachine: %v", err)
	}
	want := Machine{CPU: "Apple M2", Threads: 8, RAMBytes: 17179869184, OS: "macOS 15.1", Kernel: "24.1.0"}
	if m != want {
		t.Fatalf("got %+v, want %+v", m, want)
	}
	delete(out, "uname -r")
	if _, err := detectMachine("darwin", 8, nil, command); err == nil {
		t.Fatal("detectMachine accepted a missing kernel")
	}
	if _, err := detectMachine("plan9", 8, nil, command); err == nil {
		t.Fatal("detectMachine accepted an unsupported OS")
	}
}

// Defends the committed files against drifting apart: the committed markdown
// is exactly what -from-json renders from the committed JSON, the three cases
// the task names are there, and the large input has at least 3,000 files.
func TestCommittedPerfResultsRegenerateFromJSON(t *testing.T) {
	b, err := os.ReadFile(perfJSONName)
	if err != nil {
		t.Fatalf("%s: %v", perfJSONName, err)
	}
	var r PerfResults
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatalf("%s: %v", perfJSONName, err)
	}
	want, err := renderPerf(r)
	if err != nil {
		t.Fatalf("committed JSON does not render: %v", err)
	}
	got, err := os.ReadFile(perfMDName)
	if err != nil {
		t.Fatalf("%s: %v", perfMDName, err)
	}
	if string(got) != want {
		t.Fatalf("%s is not what %s renders; run go run ./benchmarks -perf -from-json", perfMDName, perfJSONName)
	}
	var cmds []string
	for _, c := range r.Cases {
		cmds = append(cmds, c.Command)
	}
	if strings.Join(cmds, ",") != "llm-data --version,llm-data detect --path .,llm-data stats --path ." {
		t.Fatalf("committed cases = %v", cmds)
	}
	if !strings.Contains(r.Cases[2].Input, "3,000") {
		t.Fatalf("large input is %q", r.Cases[2].Input)
	}
}

// Defends the whole path against the real binary: with no -bin the program
// builds ./cmd/llm-data and all three cases record a timing.
func TestRunPerfAgainstTheRealBinary(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("BENCH_DIR", "")
	var out, errOut bytes.Buffer
	if code := run([]string{"-perf", "-warmup", "0", "-runs", "1", "-perf-dir", dir}, &out, &errOut); code != 0 {
		t.Fatalf("run exited %d: %s", code, errOut.String())
	}
	if n := strings.Count(out.String(), "| `llm-data "); n != 3 {
		t.Fatalf("got %d case rows, want 3:\n%s", n, out.String())
	}
}

// Defends against the committed timings lagging the release they are quoted
// for: results measured on 1.1.0 sat beside a README and catalog entry that
// describe 2.0.x. The newest dated heading in CHANGELOG.md names the release;
// the committed JSON, and the README sentence that quotes it, must name the
// same version. No tag lookup, so a shallow CI checkout reads the same thing.
func TestCommittedPerfResultsAreFromTheLatestRelease(t *testing.T) {
	changelog, err := os.ReadFile(filepath.Join("..", "CHANGELOG.md"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^## \[(\d+\.\d+\.\d+)\] - \d{4}-\d{2}-\d{2}$`).FindSubmatch(changelog)
	if m == nil {
		t.Fatal("CHANGELOG.md has no dated release heading")
	}
	release := string(m[1])

	b, err := os.ReadFile(perfJSONName)
	if err != nil {
		t.Fatal(err)
	}
	var r PerfResults
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	if r.Version != release {
		t.Fatalf("%s records llm-data %s, but the latest release is %s; build the release and run go run ./benchmarks -perf", perfJSONName, r.Version, release)
	}

	readme, err := os.ReadFile(filepath.Join("..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "committed measurements for `llm-data " + release + "`"; !bytes.Contains(readme, []byte(want)) {
		t.Fatalf("README Performance section does not quote %q", want)
	}
}
