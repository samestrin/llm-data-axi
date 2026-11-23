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

// Defends the release procedure against a tag whose own figures are stale.
// The checklist measured the published binary after the tag, so the tagged
// commit carried the previous release's results while its CHANGELOG named
// the new one, and TestCommittedPerfResultsAreFromTheLatestRelease failed on
// the tag itself (v2.0.1's README likewise pinned v2.0.0). The release
// candidate is built from the commit to be tagged, stamped with the release
// version so its results do not say dev, and measured before the tag; the
// token benchmark is regenerated from a clean checkout of that commit.
// (This replaces TestReleaseChecklistBenchmarksPublishedReleaseBinary: the
// order it pinned is the one that left the tag stale, not a guard weakened.)
func TestReleaseChecklistBenchmarksTheCandidateBeforeTheTag(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRootDir, "docs", "release-checklist.md"))
	if err != nil {
		t.Fatal(err)
	}
	checklist := string(b)
	benchmark := strings.Index(checklist, "Benchmark the release candidate\n")
	publish := strings.Index(checklist, "Tag and publish\n")
	if benchmark < 0 || publish < 0 || benchmark > publish {
		t.Error("release checklist must benchmark the release candidate before the tag")
	}
	for _, want := range []string{
		"make build VERSION=v<version>",
		"go run ./benchmarks -perf -bin build/llm-data",
		"go run ./benchmarks -bin build/llm-data -o benchmarks/RESULTS.md",
	} {
		if !strings.Contains(checklist, want) {
			t.Errorf("release checklist does not contain %q", want)
		}
	}
	wantLine := func(parts ...string) {
		t.Helper()
		for _, line := range strings.Split(checklist, "\n") {
			ok := strings.HasPrefix(line, "- [ ]")
			for _, p := range parts {
				ok = ok && strings.Contains(line, p)
			}
			if ok {
				return
			}
		}
		t.Errorf("release checklist has no checkbox containing %q", parts)
	}
	wantLine("benchmarks/results-performance.*", "release version", "not `dev`")
	wantLine("clean checkout", "benchmarks/RESULTS.md", "catalog")
}
