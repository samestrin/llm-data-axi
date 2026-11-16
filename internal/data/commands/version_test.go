package commands

import (
	"runtime/debug"
	"testing"
)

// fakeBuildInfo stands in for the module version Go records in the binary,
// restoring the real reader afterwards. Tests are not parallel-safe here.
func fakeBuildInfo(t *testing.T, info *debug.BuildInfo, ok bool) {
	t.Helper()
	orig := readBuildInfo
	readBuildInfo = func() (*debug.BuildInfo, bool) { return info, ok }
	t.Cleanup(func() { readBuildInfo = orig })
}

// Defends the go-install version bug: Version defaulted to the literal
// "1.0.0", so `go install ...@v1.1.0` (which runs no ldflags) reported 1.0.0 —
// a wrong release, not just an unknown one. When the stamp is the "dev"
// default, the module version Go recorded must win, bare of its "v".
func TestResolveVersionFallsBackToModuleVersion(t *testing.T) {
	for _, mv := range []string{"v1.1.0", "1.1.0", "v1.1.1-0.20261116120000-abcdef123456"} {
		info := &debug.BuildInfo{Main: debug.Module{Version: mv}}
		if got := resolveVersion(defaultVersion, info, true); got != trimV(mv) {
			t.Errorf("resolveVersion(dev, Main.Version=%q) = %q, want %q", mv, got, trimV(mv))
		}
	}
}

// Defends ldflags winning: a release build stamps the tag, and the module
// version Go records (often "(devel)" or a pseudo-version) must not override
// it.
func TestResolveVersionLdflagsWin(t *testing.T) {
	info := &debug.BuildInfo{Main: debug.Module{Version: "v9.9.9"}}
	for _, stamped := range []string{"v1.1.0", "1.1.0", "v1.1.0-2-g2261c87"} {
		if got := resolveVersion(stamped, info, true); got != trimV(stamped) {
			t.Errorf("resolveVersion(%q, v9.9.9) = %q, want the stamp %q", stamped, got, trimV(stamped))
		}
	}
}

// Defends the "(devel)" skip: a local `go build` records "(devel)", which is
// not a version. It must leave the honest "dev" default in place, as must a
// missing or empty build info.
func TestResolveVersionSkipsDevelEmptyAndMissing(t *testing.T) {
	cases := []struct {
		name string
		info *debug.BuildInfo
		ok   bool
	}{
		{"devel", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, true},
		{"empty version", &debug.BuildInfo{Main: debug.Module{Version: ""}}, true},
		{"no build info", nil, false},
		{"nil info with ok", nil, true},
	}
	for _, c := range cases {
		if got := resolveVersion(defaultVersion, c.info, c.ok); got != "dev" {
			t.Errorf("%s: resolveVersion = %q, want %q", c.name, got, "dev")
		}
	}
}

// Defends the default itself: the unstamped default must be "dev", never a
// release number the binary cannot vouch for.
func TestDefaultVersionIsDev(t *testing.T) {
	if defaultVersion != "dev" {
		t.Errorf("defaultVersion = %q, want %q", defaultVersion, "dev")
	}
}

// Defends the CLI wiring: `llm-data --version` on a go-install build must
// print the module version, not the default and not a stale release.
func TestRootVersionUsesModuleVersionWhenUnstamped(t *testing.T) {
	fakeExecutable(t)
	stampVersion(t, defaultVersion)
	fakeBuildInfo(t, &debug.BuildInfo{Main: debug.Module{Version: "v1.1.0"}}, true)
	for _, flag := range []string{"-v", "-V", "--version"} {
		// Through run: -V is rewritten there, before cobra parses.
		if code, out, _ := runCLI(t, flag); code != 0 || out != "1.1.0\n" {
			t.Errorf("%s on a go-install build: want %q, got exit %d, %q", flag, "1.1.0\n", code, out)
		}
	}
}

// Defends the MCP wiring helper: the server stamps its own symbol
// (main.serverVersion), so ResolveVersion must apply the same rules to a
// stamp that is not commands.Version.
func TestResolveVersionAppliesToTheServerStamp(t *testing.T) {
	fakeBuildInfo(t, &debug.BuildInfo{Main: debug.Module{Version: "v1.1.0"}}, true)
	if got := ResolveVersion(defaultVersion); got != "1.1.0" {
		t.Errorf("ResolveVersion(dev) = %q, want %q", got, "1.1.0")
	}
	if got := ResolveVersion("v9.8.7"); got != "9.8.7" {
		t.Errorf("ResolveVersion(v9.8.7) = %q, want %q", got, "9.8.7")
	}
}

// trimV is the expectation-side "v" strip, independent of the code under
// test.
func trimV(v string) string {
	if len(v) > 0 && v[0] == 'v' {
		return v[1:]
	}
	return v
}
