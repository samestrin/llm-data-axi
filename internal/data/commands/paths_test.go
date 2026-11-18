package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// realTempDir is t.TempDir with its symlinks resolved. On macOS t.TempDir
// sits behind /var -> /private/var and os.Getwd resolves it, so a test that
// compares the two unresolved would fail there and nowhere else.
func realTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// writeProject creates <dir>/proj/src/a.go holding a line multigrep finds.
func writeProject(t *testing.T, dir string) string {
	t.Helper()
	proj := filepath.Join(dir, "proj")
	if err := os.MkdirAll(filepath.Join(proj, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "src", "a.go"), []byte("func needle() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return proj
}

// pathCommands are the commands whose output paths --absolute governs, each
// with the args that run it on a --path and the JSON fields holding a path.
var pathCommands = []struct {
	name  string
	args  func(path string) []string
	paths func(t *testing.T, doc map[string]any) []string
}{
	{
		name: "tree",
		args: func(p string) []string { return []string{"tree", "--path", p} },
		paths: func(t *testing.T, doc map[string]any) []string {
			got := []string{str(t, doc, "root")}
			entries, _ := doc["entries"].([]any)
			for len(entries) > 0 {
				e := entries[0].(map[string]any)
				got = append(got, str(t, e, "path"))
				entries, _ = e["items"].([]any)
			}
			return got
		},
	},
	{
		name: "stats",
		args: func(p string) []string { return []string{"stats", "--path", p} },
		paths: func(t *testing.T, doc map[string]any) []string {
			return []string{str(t, doc, "path")}
		},
	},
	{
		name: "multigrep",
		args: func(p string) []string { return []string{"multigrep", "--path", p, "--keywords", "needle"} },
		paths: func(t *testing.T, doc map[string]any) []string {
			return []string{str(t, doc, "search_path")}
		},
	},
}

func str(t *testing.T, m map[string]any, key string) string {
	t.Helper()
	s, ok := m[key].(string)
	if !ok {
		t.Fatalf("field %q missing or not a string in %v", key, m)
	}
	return s
}

// jsonPaths runs args with --json and returns the command's path fields.
func jsonPaths(t *testing.T, cmd int, args ...string) []string {
	t.Helper()
	code, stdout, _ := runCLI(t, append(args, "--json")...)
	if code != 0 {
		t.Fatalf("%v: exit %d: %s", args, code, stdout)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("%v: not JSON: %v\n%s", args, err, stdout)
	}
	return pathCommands[cmd].paths(t, doc)
}

// formats are the three encodings the relative-path contract holds in.
var formats = []struct {
	name string
	flag []string
}{{"toon", nil}, {"plain", []string{"--plain"}}, {"json", []string{"--json"}}}

// Defends epic 11.2 T1: tree, stats and multigrep printed absolute paths, so
// their output depended on where the checkout sat on disk. Paths are now
// relative to the working directory, and --absolute brings the old paths
// back, in every format.
func TestPathsRelativeByDefaultAbsoluteOnRequest(t *testing.T) {
	dir := realTempDir(t)
	proj := writeProject(t, dir)
	t.Chdir(dir)

	for i, c := range pathCommands {
		for _, f := range formats {
			t.Run(c.name+"/"+f.name, func(t *testing.T) {
				code, out, _ := runCLI(t, append(c.args("proj"), f.flag...)...)
				if code != 0 || !strings.Contains(out, "proj") {
					t.Fatalf("exit %d, want 0 and output naming proj: %s", code, out)
				}
				if strings.Contains(out, dir) {
					t.Errorf("default output carries the absolute path %s:\n%s", dir, out)
				}

				code, out, _ = runCLI(t, append(c.args("proj"), append(f.flag, "--absolute")...)...)
				if code != 0 || !strings.Contains(out, proj) {
					t.Errorf("--absolute: exit %d, want 0 and output naming %s:\n%s", code, proj, out)
				}
			})
		}

		t.Run(c.name+"/fields", func(t *testing.T) {
			for _, p := range jsonPaths(t, i, c.args("proj")...) {
				if p != "proj" && !strings.HasPrefix(p, "proj"+string(filepath.Separator)) {
					t.Errorf("path %q, want relative under proj", p)
				}
			}
			for _, p := range jsonPaths(t, i, append(c.args("proj"), "--absolute")...) {
				if p != proj && !strings.HasPrefix(p, proj+string(filepath.Separator)) {
					t.Errorf("--absolute: path %q, want absolute under %s", p, proj)
				}
			}
		})
	}
}

// Defends the point of relative paths: the same tree at two places on disk
// prints the same bytes, so output and benchmark totals stop depending on the
// machine.
func TestPathOutputIdenticalAtTwoLocations(t *testing.T) {
	first, second := realTempDir(t), realTempDir(t)
	writeProject(t, first)
	writeProject(t, second)

	for _, c := range pathCommands {
		for _, f := range formats {
			t.Run(c.name+"/"+f.name, func(t *testing.T) {
				outAt := func(dir string) string {
					t.Chdir(dir)
					code, out, _ := runCLI(t, append(c.args("proj"), f.flag...)...)
					if code != 0 || out == "" {
						t.Fatalf("exit %d, output %q", code, out)
					}
					return out
				}
				if a, b := outAt(first), outAt(second); a != b {
					t.Errorf("output differs between locations:\n--- %s\n%s\n--- %s\n%s", first, a, second, b)
				}
			})
		}
	}
}

// Defends "a path outside the working directory stays absolute": `..` and an
// absolute path elsewhere must not become a chain of "../" a caller has to
// resolve against a directory it may not know.
func TestPathOutsideWorkingDirectoryStaysAbsolute(t *testing.T) {
	dir := realTempDir(t)
	proj := writeProject(t, dir)
	elsewhere := writeProject(t, realTempDir(t))
	t.Chdir(proj)

	for i, c := range pathCommands {
		for _, tc := range []struct{ arg, want string }{{"..", dir}, {elsewhere, elsewhere}} {
			t.Run(c.name+"/"+tc.arg, func(t *testing.T) {
				for _, p := range jsonPaths(t, i, c.args(tc.arg)...) {
					if p != tc.want && !strings.HasPrefix(p, tc.want+string(filepath.Separator)) {
						t.Errorf("path %q, want absolute under %s", p, tc.want)
					}
				}
			})
		}
	}
}

// Defends a working directory reached through a symlink: os.Getwd reports
// the link while a path the caller resolved names the real directory, and a
// lexical comparison of the two put the whole tree "outside" and printed it
// absolute.
func TestPathsRelativeUnderSymlinkedWorkingDirectory(t *testing.T) {
	real := realTempDir(t)
	proj := writeProject(t, real)
	link := filepath.Join(realTempDir(t), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	t.Chdir(link)

	for i, c := range pathCommands {
		for _, arg := range []string{"proj", proj, filepath.Join(link, "proj")} {
			t.Run(c.name+"/"+arg, func(t *testing.T) {
				for _, p := range jsonPaths(t, i, c.args(arg)...) {
					if p != "proj" && !strings.HasPrefix(p, "proj"+string(filepath.Separator)) {
						t.Errorf("path %q, want relative under proj", p)
					}
				}
			})
		}
	}
}

// Defends against a flag accepted and silently ignored: --absolute changes
// only tree, stats and multigrep, so any other command refuses it rather
// than exiting 0 with output it did not change.
func TestAbsoluteRejectedWhereItHasNoEffect(t *testing.T) {
	for _, args := range [][]string{{"count", "--path", ".", "--absolute"}, {"repo-root", "--absolute"}, {"--absolute"}} {
		code, out, _ := runCLI(t, append(args, "--json")...)
		if code != 2 {
			t.Errorf("%v: exit %d, want 2: %s", args, code, out)
		}
		if !strings.Contains(out, "--absolute") || !strings.Contains(out, "tree, stats and multigrep") {
			t.Errorf("%v: error must name the commands --absolute affects: %s", args, out)
		}
	}
}

// Defends the help naming the flag's scope and the outside-the-directory
// rule, as --min's help names its no-op under TOON.
func TestAbsoluteHelpStatesScope(t *testing.T) {
	_, out, _ := runCLI(t, "tree", "--help")
	for _, want := range []string{"--absolute", "tree, stats and multigrep", "outside the working directory"} {
		if !strings.Contains(out, want) {
			t.Errorf("tree --help must contain %q:\n%s", want, out)
		}
	}
}
