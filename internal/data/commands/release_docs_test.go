package commands

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Defends the README's install instructions against naming an older release as
// latest. Historical migration notes may name old versions; every installation
// pin, source checkout and configuration example must use the newest release.
func TestReadmeReleaseExamplesUseLatestRelease(t *testing.T) {
	changelog, err := os.ReadFile(filepath.Join(repoRootDir, "CHANGELOG.md"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^## \[(\d+\.\d+\.\d+)\] - \d{4}-\d{2}-\d{2}$`).FindSubmatch(changelog)
	if m == nil {
		t.Fatal("CHANGELOG.md has no dated release heading")
	}
	latest := "v" + string(m[1])

	readme, err := os.ReadFile(filepath.Join(repoRootDir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(readme)
	if want := "Pin the latest release, `" + latest + "`"; !strings.Contains(doc, want) {
		t.Errorf("README.md does not contain %q", want)
	}
	for _, pin := range regexp.MustCompile(`LLM_DATA_VERSION=(v\d+\.\d+\.\d+)`).FindAllStringSubmatch(doc, -1) {
		if pin[1] != latest {
			t.Errorf("README.md installer pin is %s, want latest release %s", pin[1], latest)
		}
	}

	install := readmeFileSection(t, "Install")
	if want := "git checkout " + latest; !strings.Contains(install, want) {
		t.Errorf("README.md source install does not contain %q", want)
	}
	for _, command := range []string{
		"go install github.com/samestrin/llm-data-axi/v2/cmd/llm-data@latest",
		"go install github.com/samestrin/llm-data-axi/v2/cmd/llm-data-mcp@latest",
	} {
		if !strings.Contains(install, command) {
			t.Errorf("README.md Go install section does not contain %q", command)
		}
	}
	configuration := readmeFileSection(t, "Configuration")
	if want := "such as `" + latest + "`"; !strings.Contains(configuration, want) {
		t.Errorf("README.md configuration example does not contain %q", want)
	}
}

// Defends Epic 10 AC4: every release refreshes the committed benchmark results
// and the README table, not just the demo recording.
func TestReleaseChecklistRegeneratesPerformanceResults(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRootDir, "docs", "release-checklist.md"))
	if err != nil {
		t.Fatal(err)
	}
	checklist := string(b)
	if !strings.Contains(checklist, "go run ./benchmarks -perf") {
		t.Error("release checklist does not re-run the performance benchmark")
	}
	for _, line := range strings.Split(checklist, "\n") {
		if strings.HasPrefix(line, "- [ ]") &&
			strings.Contains(line, "benchmarks/results-performance.*") &&
			strings.Contains(line, "README Performance table") &&
			strings.Contains(line, "committed") {
			return
		}
	}
	t.Error("release checklist has no checkbox requiring regenerated results and the README Performance table to be committed")
}

// Defends the release procedure against benchmarking an unstamped local build:
// without -bin the script builds a binary that reports dev, so its results fail
// the latest-release guard. The published release is installed before measuring.
func TestReleaseChecklistBenchmarksPublishedReleaseBinary(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRootDir, "docs", "release-checklist.md"))
	if err != nil {
		t.Fatal(err)
	}
	checklist := string(b)
	publish := strings.Index(checklist, "Tag and publish\n")
	install := strings.Index(checklist, "Check the install path\n")
	benchmark := strings.Index(checklist, "Re-run the performance benchmark\n")
	if publish < 0 || install < 0 || benchmark < 0 || !(publish < install && install < benchmark) {
		t.Error("release checklist must publish and install the release before benchmarking it")
	}
	if want := `go run ./benchmarks -perf -bin "$(command -v llm-data)"`; !strings.Contains(checklist, want) {
		t.Errorf("release checklist does not measure the installed release binary with %q", want)
	}
	for _, line := range strings.Split(checklist, "\n") {
		if strings.HasPrefix(line, "- [ ]") &&
			strings.Contains(line, "benchmarks/results-performance.*") &&
			strings.Contains(line, "published release version") &&
			strings.Contains(line, "not `dev`") {
			return
		}
	}
	t.Error("release checklist does not require results naming the published release instead of dev")
}
