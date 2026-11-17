package internal_test

import (
	"errors"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// moduleBase is the GitHub repo slug. It never changes; only the Go module
// path gains the /v2 major-version suffix (epic 11.1).
const moduleBase = "github.com/samestrin/llm-data-axi"

// wantModulePath is the module path Go requires for a v2.0.0 tag: without the
// /v2 suffix, `go install .../cmd/llm-data@latest` keeps resolving v1.x.
const wantModulePath = moduleBase + "/v2"

// parseModulePath extracts the path from a go.mod's module directive. A
// missing directive is an error, not an empty pass.
func parseModulePath(goMod []byte) (string, error) {
	for _, line := range strings.Split(string(goMod), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "module" {
			return strings.Trim(fields[1], "\""), nil
		}
	}
	return "", errors.New("no module directive in go.mod")
}

// isOldModuleImport reports whether an import path still names the pre-v2
// module. Anything under the repo slug that is not under /v2 is stale; the
// slug alone (a URL in a string, say) never reaches here because only import
// paths are scanned.
func isOldModuleImport(path string) bool {
	if path == moduleBase {
		return true
	}
	if !strings.HasPrefix(path, moduleBase+"/") {
		return false
	}
	return path != wantModulePath && !strings.HasPrefix(path, wantModulePath+"/")
}

// scanOldModuleImports returns "file: import" for every .go file under root
// (tests included: they must compile too) that imports the pre-v2 module.
// Hidden directories (.git, .planning) are skipped; a parse error is returned
// rather than skipped, so an unparseable file cannot hide an import.
func scanOldModuleImports(root string) ([]string, error) {
	var hits []string
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Never skip the root itself: WalkDir("..") names the root "..",
			// which looks hidden and would make this guard pass vacuously.
			if name := d.Name(); p != root && strings.HasPrefix(name, ".") {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		f, err := parser.ParseFile(fset, p, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return err
			}
			if isOldModuleImport(path) {
				hits = append(hits, p+": "+path)
			}
		}
		return nil
	})
	return hits, err
}

// TestModulePathIsV2 defends epic 11.1: go.mod must declare the /v2 module
// path, or a v2.0.0 tag is invisible to `go install`.
func TestModulePathIsV2(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseModulePath(data)
	if err != nil {
		t.Fatal(err)
	}
	if got != wantModulePath {
		t.Fatalf("go.mod module path = %q, want %q", got, wantModulePath)
	}
}

// TestNoOldModuleImports defends epic 11.1: no .go file may still import the
// pre-v2 path — a leftover import would resolve to the published v1 module
// instead of this checkout.
func TestNoOldModuleImports(t *testing.T) {
	hits, err := scanOldModuleImports("..")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) > 0 {
		t.Errorf("pre-v2 module imports remain:\n%s", strings.Join(hits, "\n"))
	}
}

// TestMakefileStampsV2Version defends the silent failure the plan calls out:
// a wrong -X symbol path builds clean and the binary reports "dev". The
// ldflags symbol must carry the /v2 path.
func TestMakefileStampsV2Version(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	symbol := "-X " + wantModulePath + "/internal/data/commands.Version="
	if !strings.Contains(string(data), symbol) {
		t.Fatalf("Makefile does not stamp %s; the version stamp would be lost silently", symbol)
	}
}

// TestParseModulePath proves the parser reads the directive and rejects a
// go.mod without one, instead of passing vacuously.
func TestParseModulePath(t *testing.T) {
	got, err := parseModulePath([]byte("module example.com/x/v2\n\ngo 1.24.0\n"))
	if err != nil || got != "example.com/x/v2" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := parseModulePath([]byte("go 1.24.0\n")); err == nil {
		t.Fatal("want error for go.mod with no module directive")
	}
}

// TestScanDetectsOldModuleImports proves the guard can fail: pre-v2 imports
// (in plain and _test.go files) are reported, /v2 imports and stdlib pass,
// and a broken file is an error, not a silent pass.
func TestScanDetectsOldModuleImports(t *testing.T) {
	dir := t.TempDir()
	write := func(name, src string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", "package x\nimport _ \""+moduleBase+"/pkg/output\"\n")
	write("b_test.go", "package x\nimport _ \""+moduleBase+"/internal/data/commands\"\n")
	write("c.go", "package x\nimport _ \""+wantModulePath+"/pkg/output\"\n")
	write("d.go", "package x\nimport _ \"strings\"\n")
	hits, err := scanOldModuleImports(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("want 2 hits, got %v", hits)
	}
	write("e.go", "package x\nimport (\n")
	if _, err := scanOldModuleImports(dir); err == nil {
		t.Fatal("want parse error for broken file")
	}
}
