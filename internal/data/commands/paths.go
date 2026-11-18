package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/samestrin/llm-data-axi/v2/pkg/output"
)

// GlobalAbsolutePaths is bound to --absolute. Paths are printed relative to
// the working directory unless it is set.
var GlobalAbsolutePaths bool

// absoluteCommands are the commands whose output --absolute changes. Every
// other command refuses the flag (see checkAbsolute): accepted and ignored,
// it would exit 0 with output it never touched.
var absoluteCommands = []string{"tree", "stats", "multigrep"}

// absoluteScope names absoluteCommands for the flag's help and its error.
const absoluteScope = "tree, stats and multigrep"

// absoluteHelp is --absolute's help line. It states the commands the flag
// affects and the outside-path rule, as --min's states its no-op on TOON.
const absoluteHelp = "Print absolute paths (affects " + absoluteScope + " only; other commands reject it). " +
	"By default paths are relative to the working directory; a path outside the working directory is always absolute"

// checkAbsolute rejects --absolute on a command it does not affect.
func checkAbsolute(name string, on bool) error {
	if !on || slices.Contains(absoluteCommands, name) {
		return nil
	}
	return &output.UsageError{Msg: fmt.Sprintf(
		"--absolute has no effect on %s; it affects %s only", name, absoluteScope)}
}

// shownPaths prints the paths of one listing rooted at an absolute path:
// relative to the working directory when the root sits at or under it, and
// absolute otherwise or under --absolute. Every path in the listing follows
// its root, so a listing of `..` is absolute throughout rather than relative
// for the entries that happen to sit back under the working directory.
type shownPaths struct {
	root  string // absolute, as the command walks it
	shown string // root as printed
}

// showPaths decides how root, an absolute path, is printed.
func showPaths(root string) shownPaths {
	s := shownPaths{root: root, shown: root}
	if GlobalAbsolutePaths {
		return s
	}
	if rel, ok := relativeToCwd(root); ok {
		s.shown = rel
	}
	return s
}

// show prints p, which is the root or a path under it.
func (s shownPaths) show(p string) string {
	if s.shown == s.root {
		return p
	}
	rel, ok := output.RelativePathWithin(p, s.root)
	if !ok {
		return p
	}
	return filepath.Join(s.shown, rel)
}

// relativeToCwd returns abs relative to the working directory, when it sits
// at or under it. The working directory can be reached through a symlink
// (os.Getwd keeps $PWD), while the caller may have named the resolved path or
// the other way round, so a lexical miss is retried with both resolved.
func relativeToCwd(abs string) (string, bool) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", false
	}
	if rel, ok := output.RelativePathWithin(abs, cwd); ok {
		return rel, true
	}
	realCwd, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return "", false
	}
	realAbs, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", false
	}
	return output.RelativePathWithin(realAbs, realCwd)
}
