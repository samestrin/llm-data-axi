package commands

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestMathCommand(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected string
		hasError bool
	}{
		{
			name:     "simple addition",
			args:     []string{"2 + 3"},
			expected: "5",
		},
		{
			name:     "subtraction",
			args:     []string{"10 - 4"},
			expected: "6",
		},
		{
			name:     "multiplication",
			args:     []string{"6 * 7"},
			expected: "42",
		},
		{
			name:     "division",
			args:     []string{"20 / 4"},
			expected: "5",
		},
		{
			name:     "complex expression",
			args:     []string{"(2 + 3) * 4"},
			expected: "20",
		},
		{
			name:     "floating point",
			args:     []string{"10 / 3"},
			expected: "3.333",
		},
		{
			name:     "power",
			args:     []string{"2 ** 8"},
			expected: "256",
		},
		{
			name:     "modulo",
			args:     []string{"17 % 5"},
			expected: "2",
		},
		{
			name:     "negative numbers with parens",
			args:     []string{"(-5) + 10"},
			expected: "5",
		},
		{
			name:     "negative numbers with separator",
			args:     []string{"--", "-5 + 10"},
			expected: "5",
		},
		{
			name:     "abs function",
			args:     []string{"abs(-42)"},
			expected: "42",
		},
		{
			name:     "max function",
			args:     []string{"max(1, 5, 3)"},
			expected: "5",
		},
		{
			name:     "min function",
			args:     []string{"min(1, 5, 3)"},
			expected: "1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newMathCmd()
			buf := new(bytes.Buffer)
			errBuf := new(bytes.Buffer)
			cmd.SetOut(buf)
			cmd.SetErr(errBuf)
			cmd.SetArgs(tt.args)

			err := cmd.Execute()

			if tt.hasError {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			output := buf.String()
			if !strings.Contains(output, tt.expected) {
				t.Errorf("output %q should contain %q", output, tt.expected)
			}
		})
	}
}

func TestMathCommandJSONOutput(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		expression string
		result     float64
	}{
		{
			name:       "simple addition json",
			args:       []string{"--json", "2 + 3"},
			expression: "2 + 3",
			result:     5,
		},
		{
			name:       "complex expression json",
			args:       []string{"--json", "(10 * 5) / 2"},
			expression: "(10 * 5) / 2",
			result:     25,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newMathCmd()
			buf := new(bytes.Buffer)
			cmd.SetOut(buf)
			cmd.SetErr(new(bytes.Buffer))
			cmd.SetArgs(tt.args)

			err := cmd.Execute()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			var result map[string]interface{}
			if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
				t.Fatalf("failed to parse JSON output: %v, output: %s", err, buf.String())
			}

			if result["expression"] != tt.expression {
				t.Errorf("expected expression %q, got %q", tt.expression, result["expression"])
			}

			if result["result"] != tt.result {
				t.Errorf("expected result %v, got %v", tt.result, result["result"])
			}
		})
	}
}

func TestMathCommandMinOutput(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected string
	}{
		{
			name:     "simple addition min",
			args:     []string{"--min", "2 + 3"},
			expected: "5\n",
		},
		{
			name:     "floating point min",
			args:     []string{"--min", "10 / 4"},
			expected: "2.5\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newMathCmd()
			buf := new(bytes.Buffer)
			cmd.SetOut(buf)
			cmd.SetErr(new(bytes.Buffer))
			cmd.SetArgs(tt.args)

			err := cmd.Execute()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			output := buf.String()
			if output != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, output)
			}
		})
	}
}

// TestMathNonFiniteResultIsAnErrorInEveryFormat defends against a non-finite
// result reading as success. `math "1/0" --json` exited 0 with an EMPTY stdout
// (the json.Marshal error was dropped), --axi leaked the internal message
// "json: unsupported value: +Inf", and plain printed `RESULT: +Inf` with exit
// 0. Every format now reports a structured error naming the expression and
// the value, exit 1, and never an empty stdout under --json or --axi.
func TestMathNonFiniteResultIsAnErrorInEveryFormat(t *testing.T) {
	for _, tc := range []struct {
		expr, value, reason string
	}{
		{"1/0", "+Inf", "division by zero"},
		{"(-1)/0", "-Inf", "division by zero"},
		{"0/0", "NaN", "not a number"},
		{"sqrt(-1)", "NaN", "not a number"},
	} {
		for _, mode := range [][]string{nil, {"--plain"}, {"--plain", "--min"}, {"--json"}, {"--json", "--min"}, {"--axi"}} {
			args := append([]string{"math", tc.expr}, mode...)
			code, stdout, stderr := runCLI(t, args...)
			if code != 1 {
				t.Errorf("%v: exit %d, want 1\nstdout=%q stderr=%q", args, code, stdout, stderr)
			}
			// Plain errors go to stderr; --json and --axi errors to stdout.
			msg := stdout + stderr
			for _, want := range []string{tc.expr, tc.value, tc.reason} {
				if !strings.Contains(msg, want) {
					t.Errorf("%v: error does not name %q:\nstdout=%q stderr=%q", args, want, stdout, stderr)
				}
			}
			if strings.Contains(msg, "unsupported value") {
				t.Errorf("%v: leaked the encoder's internal message: %q", args, msg)
			}
			if strings.Contains(msg, "RESULT:") {
				t.Errorf("%v: printed a result for a non-finite value: %q", args, msg)
			}
			// TOON is the default, so no flag is structured too.
			structured := len(mode) == 0 || mode[0] == "--json" || mode[0] == "--axi"
			if structured {
				if strings.TrimSpace(stdout) == "" {
					t.Errorf("%v: empty stdout", args)
				}
				if len(mode) > 0 && mode[0] == "--json" {
					var got map[string]interface{}
					if err := json.Unmarshal([]byte(stdout), &got); err != nil {
						t.Errorf("%v: stdout is not JSON: %v\n%s", args, err, stdout)
					}
				} else if !strings.Contains(stdout, "error: true") {
					t.Errorf("%v: not a TOON error: %q", args, stdout)
				}
			}
		}
	}
}

// TestMathFiniteResultStillSucceeds is the other side of the non-finite
// check: an ordinary result, including a large finite one and a negative
// zero, must not be mistaken for a non-finite one.
func TestMathFiniteResultStillSucceeds(t *testing.T) {
	for _, expr := range []string{"1/2", "10^300", "(-0.0)", "sqrt(4)"} {
		for _, mode := range [][]string{nil, {"--plain"}, {"--json"}, {"--axi"}} {
			args := append([]string{"math", expr}, mode...)
			code, stdout, stderr := runCLI(t, args...)
			if code != 0 || strings.TrimSpace(stdout) == "" {
				t.Errorf("%v: exit %d stdout=%q stderr=%q", args, code, stdout, stderr)
			}
		}
	}
}
