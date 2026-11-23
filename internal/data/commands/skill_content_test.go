package commands

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Defends the skill for a skill-only install: an agent that got the skill
// through `npx skills add` and has no llm-data binary was told to run
// commands it did not have, and was never told what a failure looks like or
// that a cut result can be fetched whole. The skill names how to install or
// run the pinned release, the error shape, the exit codes and --full.
func TestSkillSaysHowToInstallAndHowFailuresLook(t *testing.T) {
	changelog, err := os.ReadFile(filepath.Join(repoRootDir, "CHANGELOG.md"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^## \[(\d+\.\d+\.\d+)\] - \d{4}-\d{2}-\d{2}$`).FindSubmatch(changelog)
	if m == nil {
		t.Fatal("CHANGELOG.md has no dated release heading")
	}
	latest := "v" + string(m[1])
	for _, want := range []string{
		"go install github.com/samestrin/llm-data-axi/v2/cmd/llm-data@" + latest,
		"go run github.com/samestrin/llm-data-axi/v2/cmd/llm-data@" + latest,
		"`error: true`", "`message`", "exit 1", "exit 2", "`truncated`", "`--full`",
	} {
		if !strings.Contains(setupSkillContent, want) {
			t.Errorf("skill does not contain %q", want)
		}
	}
	for _, pin := range regexp.MustCompile(`cmd/llm-data@(v\d+\.\d+\.\d+)`).FindAllStringSubmatch(setupSkillContent, -1) {
		if pin[1] != latest {
			t.Errorf("skill pins %s, want the latest release %s", pin[1], latest)
		}
	}
}
