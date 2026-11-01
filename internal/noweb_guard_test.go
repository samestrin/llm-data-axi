package internal_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// forbiddenImports are the web-facing imports that moved to llm-extract-axi.
// Their return would mean a network or HTML-scraping command crept back in.
var forbiddenImports = map[string]bool{
	"net":      true,
	"net/http": true,
}

func isForbidden(path string) bool {
	return forbiddenImports[path] || strings.Contains(path, "goquery")
}

// scanForbiddenImports returns "file: import" for every non-test .go file
// under root that imports a forbidden package. A parse error is returned as an
// error rather than skipped, so an unparseable file cannot hide an import.
func scanForbiddenImports(root string) ([]string, error) {
	var hits []string
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
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
			if isForbidden(path) {
				hits = append(hits, p+": "+path)
			}
		}
		return nil
	})
	return hits, err
}

// TestNoWebImports defends the Epic 01 split: nothing under internal/ or pkg/
// may import net, net/http or goquery.
func TestNoWebImports(t *testing.T) {
	for _, root := range []string{".", "../pkg"} {
		hits, err := scanForbiddenImports(root)
		if err != nil {
			t.Fatalf("scan %s: %v", root, err)
		}
		if len(hits) > 0 {
			t.Errorf("forbidden imports:\n%s", strings.Join(hits, "\n"))
		}
	}
}

// TestScanDetectsForbiddenImports proves the guard can fail: a scratch file
// importing net/http (or goquery) is reported, a _test.go file is ignored,
// and a broken file is an error, not a silent pass.
func TestScanDetectsForbiddenImports(t *testing.T) {
	dir := t.TempDir()
	write := func(name, src string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", "package x\nimport _ \"net/http\"\n")
	write("b.go", "package x\nimport _ \"github.com/PuerkitoBio/goquery\"\n")
	write("c.go", "package x\nimport _ \"net\"\n")
	write("d_test.go", "package x\nimport _ \"net/http\"\n")
	write("e.go", "package x\nimport _ \"strings\"\n")
	hits, err := scanForbiddenImports(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 3 {
		t.Fatalf("want 3 hits, got %v", hits)
	}
	write("f.go", "package x\nimport (\n")
	if _, err := scanForbiddenImports(dir); err == nil {
		t.Fatal("want parse error for broken file")
	}
}
