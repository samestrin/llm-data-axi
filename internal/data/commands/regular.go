package commands

import (
	"fmt"
	"os"
)

// notRegularReason is the reason a command gives for a path it will not read.
const notRegularReason = "not a regular file"

// isSpecialFile reports whether info, for path, is neither a regular file nor
// a directory: a FIFO, a device or a socket. Reading one can block forever (a
// FIFO with no writer) or never end (/dev/zero), and the size limits cannot
// stop it, because they read the stat size, which is 0 for these. A symlink is
// judged by its target; one that cannot be followed is not reported here, so
// the read that follows says why.
func isSpecialFile(path string, info os.FileInfo) bool {
	mode := info.Mode()
	if mode&os.ModeSymlink != 0 {
		target, err := os.Stat(path)
		if err != nil {
			return false
		}
		mode = target.Mode()
	}
	return !mode.IsRegular() && !mode.IsDir()
}

// requireRegularFile refuses a named path that is not a regular file, before
// anything opens it. A path that cannot be stat'ed passes, so the caller's
// own read reports the reason.
func requireRegularFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	if !info.Mode().IsRegular() && !info.IsDir() {
		return fmt.Errorf("%s: %s", notRegularReason, path)
	}
	return nil
}
