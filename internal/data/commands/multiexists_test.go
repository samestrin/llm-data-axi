package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMultiexistsCommand(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files and directories
	existingFile := filepath.Join(tmpDir, "exists.txt")
	os.WriteFile(existingFile, []byte("content"), 0644)
	existingDir := filepath.Join(tmpDir, "existsdir")
	os.Mkdir(existingDir, 0755)
	missingFile := filepath.Join(tmpDir, "missing.txt")

	tests := []struct {
		name     string
		args     []string
		expected []string
		wantErr  bool
	}{
		{
			name:     "all files exist",
			args:     []string{existingFile, existingDir, "--no-fail"},
			expected: []string{"✓", "EXISTS", "ALL_EXIST: TRUE"},
		},
		{
			name:     "some files missing",
			args:     []string{existingFile, missingFile, "--no-fail"},
			expected: []string{"✓", "✗", "MISSING", "ALL_EXIST: FALSE"},
		},
		{
			name:     "verbose mode shows types",
			args:     []string{existingFile, existingDir, "--verbose", "--no-fail"},
			expected: []string{"file", "directory"},
		},
		{
			// The failure is the whole report: the error names the missing
			// path and no success listing precedes it (EST-6876 finding 1).
			name:    "missing file fails without no-fail",
			args:    []string{missingFile},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newMultiexistsCmd()
			cmd.SilenceUsage = true // as under the root
			buf := new(bytes.Buffer)
			errBuf := new(bytes.Buffer)
			cmd.SetOut(buf)
			cmd.SetErr(errBuf)
			cmd.SetArgs(tt.args)

			err := cmd.Execute()

			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), missingFile) {
					t.Errorf("want an error naming %s, got %v", missingFile, err)
				}
				if buf.Len() != 0 {
					t.Errorf("a failed run printed a result before its error: %q", buf.String())
				}
			}

			output := buf.String()
			for _, exp := range tt.expected {
				if !strings.Contains(output, exp) {
					t.Errorf("output %q should contain %q", output, exp)
				}
			}
		})
	}
}
