package commands

import (
	"bytes"
	"strings"
	"testing"
)

// extract-links and extract-relevant call a model and the network, and moved
// to llm-extract-axi. This defends against either one coming back: invoking
// the name must fail as an unknown command rather than run, and --help must
// not advertise it.
func TestExtractCommandsAreNotRegistered(t *testing.T) {
	for _, name := range []string{"extract-links", "extract-relevant"} {
		t.Run(name, func(t *testing.T) {
			root := NewRootCmd()
			for _, c := range root.Commands() {
				if c.Name() == name {
					t.Fatalf("%s is still registered on the root command", name)
				}
			}

			help := new(bytes.Buffer)
			root.SetOut(help)
			root.SetErr(help)
			root.SetArgs([]string{"--help"})
			if err := root.Execute(); err != nil {
				t.Fatalf("--help: %v", err)
			}
			if !strings.Contains(help.String(), "Available Commands") {
				t.Fatalf("--help printed no command list, cannot check it:\n%s", help.String())
			}
			if strings.Contains(help.String(), name) {
				t.Errorf("--help still lists %s:\n%s", name, help.String())
			}

			root = NewRootCmd()
			root.SetOut(new(bytes.Buffer))
			root.SetErr(new(bytes.Buffer))
			root.SetArgs([]string{name})
			err := root.Execute()
			if err == nil || !strings.Contains(err.Error(), "unknown command") {
				t.Errorf("running %s: want an unknown command error, got %v", name, err)
			}
		})
	}
}
