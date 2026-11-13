package mcp_integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
)

// TestReleaseStampSetsTheServerVersion defends the release version stamp:
// serverVersion was a const, and `-X` cannot set a const, so every release
// build reported the hardcoded 1.0.0 to MCP clients. The test builds the server
// with the -X symbol the Makefile itself uses, stamped the way release.yml
// stamps it (the tag, with its "v"), and reads the version back from the
// initialize response. The "v" is stripped, as `llm-data --version` strips it.
func TestReleaseStampSetsTheServerVersion(t *testing.T) {
	root := repoRoot(t)
	makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^MCP_LDFLAGS\s*:=.*-X ([^=\s]+)=\$\(VERSION\)`).FindSubmatch(makefile)
	if m == nil {
		t.Fatal("Makefile has no MCP_LDFLAGS line stamping $(VERSION)")
	}
	symbol := string(m[1])

	cli, _ := builtBinaries(t)
	mcp := filepath.Join(t.TempDir(), "llm-data-mcp")
	build := exec.Command("go", "build", "-ldflags", "-X "+symbol+"=v9.8.7", "-o", mcp, "./cmd/llm-data-mcp")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	env := append(os.Environ(), "LLM_DATA_BINARY="+cli, "LLM_DATA_MCP_AXI=")
	resp := speakWith(t, mcp, env, []string{initRequest})[1]
	var res struct {
		ServerInfo struct {
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(resp.Result, &res); err != nil {
		t.Fatalf("parse initialize result: %v\n%s", err, resp.Result)
	}
	if res.ServerInfo.Version != "9.8.7" {
		t.Errorf("serverInfo.version = %q, want the stamped 9.8.7", res.ServerInfo.Version)
	}
}
