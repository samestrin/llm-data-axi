package commands

// emptyHelp is the help list a result carries: the next steps when the result
// is empty, nil when it is not, so a `json:"help,omitempty"` field drops the
// key from TOON and --json. Plain printers leave the field out, as grep,
// multigrep and the home view do.
//
// The steps are copied, so a caller may append its own hint without writing
// into a shared list. An empty result with no steps panics: omitempty would
// drop the key, and the empty result would read as having nothing to say.
func emptyHelp(empty bool, steps ...string) []string {
	if !empty {
		return nil
	}
	if len(steps) == 0 {
		panic("emptyHelp: an empty result needs at least one next step")
	}
	return append([]string(nil), steps...)
}
