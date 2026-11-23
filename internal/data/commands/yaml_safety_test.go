package commands

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// yamlDir returns a temp dir holding config.yaml with the given content and
// mode. The mode is set with Chmod so the umask cannot change it.
func yamlDir(t *testing.T, content string, mode os.FileMode) (dir, file string) {
	t.Helper()
	dir = realTempDir(t)
	file = filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(file, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(file, mode); err != nil {
		t.Fatal(err)
	}
	return dir, file
}

// dirNames lists dir's entries, sorted.
func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

func readString(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Defends the file mode across a write: the temp file + rename came back 0600
// whatever the file was before, so a 0644 config shared with another user or
// a service stopped being readable by it, and the run still said "set".
func TestYamlWritesKeepTheFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no POSIX mode bits")
	}
	for _, args := range [][]string{
		{"yaml", "set", "port", "2"},
		{"yaml", "multiset", "port", "2", "host", "b"},
	} {
		t.Run(args[1], func(t *testing.T) {
			_, file := yamlDir(t, "port: 1\nhost: a\n", 0o644)
			full := append(append([]string{}, args[:2]...), append([]string{"--file", file}, args[2:]...)...)
			if code, stdout, _ := runCLI(t, full...); code != 0 {
				t.Fatalf("exit %d: %s", code, stdout)
			}
			info, err := os.Stat(file)
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got != 0o644 {
				t.Errorf("mode after %s = %o, want 644", args[1], got)
			}
			if !strings.Contains(readString(t, file), "port: 2") {
				t.Errorf("value not written: %q", readString(t, file))
			}
		})
	}
}

// Defends a symlinked config: the write replaced link.yaml with a regular
// file and left the target unchanged, then reported "status: set" with exit
// 0. Every other reader of the target still saw the old value.
func TestYamlWritesThroughASymlinkUpdateTheTarget(t *testing.T) {
	for _, args := range [][]string{
		{"yaml", "set", "port", "2"},
		{"yaml", "multiset", "port", "2", "host", "b"},
	} {
		t.Run(args[1], func(t *testing.T) {
			dir, target := yamlDir(t, "port: 1\nhost: a\n", 0o644)
			link := filepath.Join(dir, "link.yaml")
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("cannot create a symlink here: %v", err)
			}
			full := append(append([]string{}, args[:2]...), append([]string{"--file", link}, args[2:]...)...)
			if code, stdout, _ := runCLI(t, full...); code != 0 {
				t.Fatalf("exit %d: %s", code, stdout)
			}
			info, err := os.Lstat(link)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode()&os.ModeSymlink == 0 {
				t.Errorf("%s replaced the symlink with a regular file", args[1])
			}
			if got := readString(t, target); !strings.Contains(got, "port: 2") {
				t.Errorf("the symlink's target was not updated: %q", got)
			}
		})
	}
}

// Defends against reporting a write that did not happen: an empty key and
// "a..b" printed "status: set" with exit 0 and left the file unchanged. A key
// the CLI cannot address is a malformed invocation, exit 2, file untouched.
func TestYamlWritesRefuseAnEmptyOrMalformedKey(t *testing.T) {
	const content = "a:\n  b: 1\n"
	for _, key := range []string{"", "a..b", ".a", "a.", "a[0", "a[]"} {
		for _, sub := range []string{"set", "multiset"} {
			t.Run(sub+"/"+key, func(t *testing.T) {
				dir, file := yamlDir(t, content, 0o644)
				code, stdout, _ := runCLI(t, "yaml", sub, "--file", file, "--", key, "9")
				if code != 2 {
					t.Errorf("exit %d, want 2; stdout %q", code, stdout)
				}
				if !strings.Contains(stdout, "error: true") || strings.Contains(stdout, "status:") {
					t.Errorf("want an error and no status on stdout, got %q", stdout)
				}
				if got := readString(t, file); got != content {
					t.Errorf("file changed: %q", got)
				}
				if names := dirNames(t, dir); strings.Join(names, ",") != "config.yaml" {
					t.Errorf("files left behind: %v", names)
				}
			})
		}
	}
}

// Defends the keys that must keep working once malformed keys are refused:
// nested, bracketed, negative and escaped-dot paths.
func TestYamlWritesAcceptWellFormedKeys(t *testing.T) {
	for _, key := range []string{"a.b", "items[0]", "items[-1]", "items[0].name", `dotted\.key`} {
		if err := validateKeyPath(key); err != nil {
			t.Errorf("validateKeyPath(%q) = %v, want nil", key, err)
		}
	}
}

// Defends the line comment on the edited key: `port: 1 # keep` came back as
// `port: 2`, losing the note a maintainer left on exactly that line.
func TestYamlSetKeepsTheLineCommentOnTheEditedKey(t *testing.T) {
	_, file := yamlDir(t, "# head\nserver:\n  port: 1 # keep\n  host: a # h\n", 0o644)
	if code, stdout, _ := runCLI(t, "yaml", "set", "--file", file, "server.port", "2"); code != 0 {
		t.Fatalf("exit %d: %s", code, stdout)
	}
	got := readString(t, file)
	for _, want := range []string{"# head", "port: 2 # keep", "host: a # h"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q after set:\n%s", want, got)
		}
	}
}

// Defends read-only commands against writing: yaml get, multiget and the
// --dry-run writes each left a 0-byte <file>.lock beside the config, and
// `yaml get --file .` created "..lock" in the parent directory. A read must
// leave the directory exactly as it found it; so must a write, once it ends.
func TestYamlCommandsLeaveNoLockFileBehind(t *testing.T) {
	for _, args := range [][]string{
		{"yaml", "get", "port"},
		{"yaml", "multiget", "port", "host"},
		{"yaml", "set", "--dry-run", "port", "2"},
		{"yaml", "multiset", "--dry-run", "port", "2"},
		{"yaml", "set", "port", "2"},
		{"yaml", "multiset", "port", "3", "host", "b"},
	} {
		t.Run(strings.Join(args[1:3], " "), func(t *testing.T) {
			dir, file := yamlDir(t, "port: 1\nhost: a\n", 0o644)
			full := append(append([]string{}, args[:2]...), append([]string{"--file", file}, args[2:]...)...)
			if code, stdout, _ := runCLI(t, full...); code != 0 {
				t.Fatalf("exit %d: %s", code, stdout)
			}
			if names := dirNames(t, dir); strings.Join(names, ",") != "config.yaml" {
				t.Errorf("directory after %v = %v, want only config.yaml", args[1:], names)
			}
		})
	}

	// A get on a directory and on a missing file fail, and create nothing.
	parent := realTempDir(t)
	sub := filepath.Join(parent, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{sub, filepath.Join(sub, "missing.yaml")} {
		if code, _, _ := runCLI(t, "yaml", "get", "--file", target, "k"); code == 0 {
			t.Errorf("yaml get --file %s exit 0, want a failure", target)
		}
	}
	if names := dirNames(t, parent); strings.Join(names, ",") != "sub" {
		t.Errorf("parent after get on a directory = %v, want only sub", names)
	}
	if names := dirNames(t, sub); len(names) != 0 {
		t.Errorf("sub after get on a missing file = %v, want empty", names)
	}
}
