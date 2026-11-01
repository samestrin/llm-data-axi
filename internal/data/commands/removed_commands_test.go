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
			for _, c := range RootCmd.Commands() {
				if c.Name() == name {
					t.Fatalf("%s is still registered on the root command", name)
				}
			}

			help := new(bytes.Buffer)
			RootCmd.SetOut(help)
			RootCmd.SetErr(help)
			RootCmd.SetArgs([]string{"--help"})
			t.Cleanup(func() {
				RootCmd.SetOut(nil)
				RootCmd.SetErr(nil)
				RootCmd.SetArgs(nil)
			})
			if err := RootCmd.Execute(); err != nil {
				t.Fatalf("--help: %v", err)
			}
			if !strings.Contains(help.String(), "Available Commands") {
				t.Fatalf("--help printed no command list, cannot check it:\n%s", help.String())
			}
			if strings.Contains(help.String(), name) {
				t.Errorf("--help still lists %s:\n%s", name, help.String())
			}

			RootCmd.SetOut(new(bytes.Buffer))
			RootCmd.SetErr(new(bytes.Buffer))
			RootCmd.SetArgs([]string{name})
			err := RootCmd.Execute()
			if err == nil || !strings.Contains(err.Error(), "unknown command") {
				t.Errorf("running %s: want an unknown command error, got %v", name, err)
			}
		})
	}
}
