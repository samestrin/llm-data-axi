package commands

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Defends the audit's line citations: v2.0.1 shipped an audit whose cites
// pointed a few lines off, so a reader following `root.go:404` landed on
// the wrong code. Every cite written as a line followed by the symbol it
// names (`root.go:462` `formatConflict`) must land on a line holding that
// symbol. Cites without a symbol cannot be checked this way; the release
// checklist re-checks them by hand.
func TestAuditLineCitesLandOnTheSymbolTheyName(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRootDir, "docs", "axi-audit.md"))
	if err != nil {
		t.Fatal(err)
	}
	byBase := map[string][]string{}
	for _, dir := range []string{"internal", "pkg", "cmd"} {
		_ = filepath.Walk(filepath.Join(repoRootDir, dir), func(p string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() && strings.HasSuffix(p, ".go") {
				byBase[filepath.Base(p)] = append(byBase[filepath.Base(p)], p)
			}
			return nil
		})
	}
	cite := regexp.MustCompile("`((?:[\\w.-]+/)*([\\w-]+\\.go)):(\\d+)` `(\\w+)`")
	checked := 0
	for _, m := range cite.FindAllStringSubmatch(string(b), -1) {
		path, base, symbol := m[1], m[2], m[4]
		line, _ := strconv.Atoi(m[3])
		file := filepath.Join(repoRootDir, path)
		if !strings.Contains(path, "/") {
			if len(byBase[base]) != 1 {
				t.Errorf("cite %s: %d files are named %s; give the path", m[0], len(byBase[base]), base)
				continue
			}
			file = byBase[base][0]
		}
		src, err := os.ReadFile(file)
		if err != nil {
			t.Errorf("cite %s: %v", m[0], err)
			continue
		}
		lines := strings.Split(string(src), "\n")
		if line < 1 || line > len(lines) || !strings.Contains(lines[line-1], symbol) {
			t.Errorf("cite %s: line %d of %s does not mention %s", m[0], line, path, symbol)
		}
		checked++
	}
	if checked < 8 {
		t.Errorf("only %d symbol cites found in the audit; the pattern no longer matches how it cites code", checked)
	}
}
