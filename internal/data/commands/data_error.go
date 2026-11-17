package commands

// dataError is a runtime failure (exit 1) that carries data next to its
// message, such as the files a command could not read. PrintError puts the
// fields in the TOON and JSON error object, so a caller reads the list as
// data; the message says the same for --plain.
type dataError struct {
	msg    string
	fields map[string]any
}

func (e *dataError) Error() string               { return e.msg }
func (e *dataError) ErrorFields() map[string]any { return e.fields }

// pathFailure is one path a command could not read or write, and why.
type pathFailure struct {
	Path   string `json:"path,omitempty"`
	File   string `json:"file,omitempty"`
	Reason string `json:"reason"`
}
