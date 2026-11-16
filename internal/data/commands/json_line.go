package commands

import (
	"encoding/json"
	"fmt"
	"io"
)

// printJSONLine writes v to w as one line of JSON. A value JSON cannot
// encode, such as YAML's .inf, is an error naming what was being printed:
// the dropped json.Marshal error this replaces printed an empty line and
// exited 0.
func printJSONLine(w io.Writer, what string, v interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	_, err = fmt.Fprintln(w, string(b))
	return err
}
