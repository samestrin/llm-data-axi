package demo

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The demo is recorded by vhs, which is not installed on the build hosts (it
// needs ttyd and ffmpeg as well). These tests defend the tape from the outside:
// they read docs/demo/llm-data.tape, replay every command in it against
// ./build/llm-data, and check the output and the timings. vhs never runs.
const (
	demoTape = "../../docs/demo/llm-data.tape"
	demoGIF  = "docs/demo/llm-data.gif"
	// demoMaxDuration is the session length the demo promises.
	demoMaxDuration = 30 * time.Second
	// demoStartupAllowance covers what the tape spends outside its own Sleep
	// lines: start-up and the three commands themselves.
	demoStartupAllowance = 3 * time.Second
	// demoReplayLimit is a backstop so a broken tape cannot hang the suite.
	demoReplayLimit = 90 * time.Second
	// demoBinary is the binary `make build` writes; the tape puts its directory
	// first on PATH, so this is the binary every recorded command runs.
	demoBinary = "../../build/llm-data"
)

// vhs defaults, used when the tape does not set them.
const (
	vhsDefaultTyping   = 50 * time.Millisecond
	vhsDefaultPlayback = 1.0
)

var (
	tapeTypeLine  = regexp.MustCompile(`^Type "(.*)" Enter$`)
	tapeSleepLine = regexp.MustCompile(`^Sleep (\S+)$`)
	tapeSetLine   = regexp.MustCompile(`^Set (\S+) (\S+)$`)
)

// tapeCommand is one Type line and whether it sits in a Hide section.
type tapeCommand struct {
	text   string
	hidden bool
}

// tapeSession is everything the checks need to know about the tape.
type tapeSession struct {
	output   string
	commands []tapeCommand
	// typed is the number of characters typed by Type lines, each Enter included.
	typed    int
	sleeps   time.Duration
	typing   time.Duration
	playback float64
}

// parseDemoTape reads the tape. It fails on a Type line it cannot replay rather
// than skipping it: a check that silently ignored a command would pass while
// the demo showed a command that no longer works, and it also keeps the tape's
// one-line `Type "..." Enter` style from drifting away from this reader.
func parseDemoTape(t *testing.T) tapeSession {
	t.Helper()
	raw, err := os.ReadFile(demoTape)
	if err != nil {
		t.Fatalf("cannot read the demo tape: %v", err)
	}
	s := tapeSession{typing: vhsDefaultTyping, playback: vhsDefaultPlayback}
	hidden := false
	for i, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "" || strings.HasPrefix(line, "#"):
			continue
		case strings.HasPrefix(line, "Output "):
			s.output = strings.TrimSpace(strings.TrimPrefix(line, "Output "))
		case line == "Hide":
			hidden = true
		case line == "Show":
			hidden = false
		}
		if m := tapeSetLine.FindStringSubmatch(line); m != nil {
			switch m[1] {
			case "TypingSpeed":
				d, err := time.ParseDuration(m[2])
				if err != nil {
					t.Fatalf("%s:%d: bad TypingSpeed %q: %v", demoTape, i+1, m[2], err)
				}
				s.typing = d
			case "PlaybackSpeed":
				f, err := strconv.ParseFloat(m[2], 64)
				if err != nil || f <= 0 {
					t.Fatalf("%s:%d: bad PlaybackSpeed %q", demoTape, i+1, m[2])
				}
				s.playback = f
			}
			continue
		}
		if m := tapeSleepLine.FindStringSubmatch(line); m != nil {
			d, err := time.ParseDuration(m[1])
			if err != nil {
				t.Fatalf("%s:%d: bad Sleep %q: %v", demoTape, i+1, m[1], err)
			}
			s.sleeps += d
			continue
		}
		if strings.HasPrefix(line, "Type ") {
			m := tapeTypeLine.FindStringSubmatch(line)
			if m == nil {
				t.Fatalf("%s:%d: this check can only replay a one-line `Type \"...\" Enter` with no double quote in the text: %s", demoTape, i+1, line)
			}
			s.commands = append(s.commands, tapeCommand{text: m[1], hidden: hidden})
			s.typed += len(m[1]) + 1 // the Enter key is typed too
		}
	}
	if len(s.commands) == 0 {
		t.Fatalf("%s holds no command to replay", demoTape)
	}
	return s
}

// visible returns the commands the recording shows, in order.
func (s tapeSession) visible() []string {
	var out []string
	for _, c := range s.commands {
		if !c.hidden {
			out = append(out, c.text)
		}
	}
	return out
}

// estimatedDuration is the session length worked out from the tape itself: vhs
// types each character at TypingSpeed, sleeps for each Sleep, and divides the
// whole session by PlaybackSpeed. It is an estimate, not a measurement;
// demoStartupAllowance is deliberately generous so a tape that only just fits
// fails here instead of in the operator's render.
func (s tapeSession) estimatedDuration() time.Duration {
	typed := time.Duration(float64(s.typed) * float64(s.typing) / s.playback)
	slept := time.Duration(float64(s.sleeps) / s.playback)
	return typed + slept + demoStartupAllowance
}

// Defends the demo the README promises: one GIF at the committed path, with no
// hosted recording link, showing exactly the commands the epic names. The path
// check is what keeps `## Demo` in the README pointing at a file in this
// repository instead of at vhs.charm.sh.
func TestDemoTapeRecordsTheCommittedGIF(t *testing.T) {
	s := parseDemoTape(t)
	if s.output != demoGIF {
		t.Errorf("the tape writes %q, want %q: the GIF is a file in the repository", s.output, demoGIF)
	}
	// This task commits the tape, not the recording: the operator renders the
	// GIF in their own session (the epic's manual criterion). So a missing GIF
	// is not a failure here, but a file at that path that is not a GIF is --
	// otherwise a truncated or HTML error page would sit under the README image.
	gif, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(s.output)))
	switch {
	case err == nil && !bytes.HasPrefix(gif, []byte("GIF8")):
		t.Errorf("%s is %d bytes and not a GIF", s.output, len(gif))
	case err != nil:
		t.Logf("%s is not committed yet (the operator renders it): %v", s.output, err)
	}

	visible := s.visible()
	if len(visible) != 3 {
		t.Fatalf("the demo shows %d commands, want 3 (detect, grep --axi, yaml get):\n%s", len(visible), strings.Join(visible, "\n"))
	}
	for i, want := range []string{
		"llm-data detect",
		"llm-data grep",
		"llm-data yaml get",
	} {
		if !strings.HasPrefix(visible[i], want) {
			t.Errorf("shown command %d is %q, want it to start with %q", i+1, visible[i], want)
		}
	}
	// The epic names the AXI output format for grep explicitly, so a demo that
	// dropped the flag would show a different document and still pass above.
	if !strings.Contains(visible[1], "--axi") {
		t.Errorf("the grep command does not pass --axi: %q", visible[1])
	}
	// Every shown command must run the same binary the hidden setup puts on
	// PATH; a bare command that PATH resolves elsewhere would record another
	// install's behaviour.
	for i, cmd := range visible {
		if !strings.HasPrefix(cmd, "llm-data ") {
			t.Errorf("shown command %d does not run llm-data: %q", i+1, cmd)
		}
	}

	for _, name := range []string{"README.md", "llm-data.tape"} {
		body, err := os.ReadFile("../../docs/demo/" + name)
		if err != nil {
			t.Fatalf("cannot read docs/demo/%s: %v", name, err)
		}
		if strings.Contains(string(body), "vhs.charm.sh") {
			t.Errorf("docs/demo/%s links a hosted recording; the GIF is committed in the repository", name)
		}
	}
}

// Defends the "under 30 seconds" promise. vhs cannot run here, so the tape's
// own timings are summed: a new Sleep, a slow TypingSpeed or a fourth command
// that pushes the session over the limit fails here before the operator renders
// it.
func TestDemoTapeIsUnderThirtySeconds(t *testing.T) {
	s := parseDemoTape(t)
	got := s.estimatedDuration()
	t.Logf("estimated session: %s (typing %s, sleeps %s, playback %g, %d characters)",
		got, time.Duration(float64(s.typed)*float64(s.typing)/s.playback), s.sleeps, s.playback, s.typed)
	if got >= demoMaxDuration {
		t.Errorf("the tape is estimated at %s, want under %s: trim a Sleep, not a command", got, demoMaxDuration)
	}
}

// Defends every command in the tape: it runs the same `Type` lines, in order,
// in one bash session, the way vhs does. A command that stopped working, a
// flag it dropped or a fixture that changed under it fails here, which is what
// keeps the committed recording honest until the next render.
func TestDemoTapeReplays(t *testing.T) {
	bin, err := filepath.Abs(demoBinary)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("run `make build` first; %s is missing", demoBinary)
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}

	s := parseDemoTape(t)
	lines := make([]string, 0, len(s.commands))
	for _, c := range s.commands {
		lines = append(lines, c.text)
	}
	// The hidden setup resolves $root through git; run it from the checkout and
	// make sure a failure to do so is loud instead of an empty PATH.
	script := "set -u\n" + strings.Join(lines, "\n") + "\nexit 0\n"

	ctx, cancel := context.WithTimeout(context.Background(), demoReplayLimit)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-c", script)
	cmd.Dir = root
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err = cmd.Run()
	stdout := out.String()
	if ctx.Err() != nil {
		t.Fatalf("replaying the tape took longer than %s", demoReplayLimit)
	}
	if err != nil {
		t.Fatalf("cannot replay the tape: %v\n%s", err, out.String())
	}

	for _, bad := range []string{"error: true", "Error: "} {
		if strings.Contains(stdout, bad) {
			t.Errorf("the replay reports %q; a command in the demo fails:\n%s", bad, stdout)
		}
	}

	// Each marker is the payload the command exists to print, so this fails
	// when a command printed nothing instead of only when it exited non-zero.
	for _, want := range []struct{ command, marker string }{
		{"detect", "stack: python"},
		{"grep --axi", "matches[1]{file,line,text}:"},
		{"grep --axi", `app.py,8,"def start_server(host, port):"`},
		{"yaml get", "key: server"},
		{"yaml get", "port: 8080"},
	} {
		if !strings.Contains(stdout, want.marker) {
			t.Errorf("the %s command does not print %q:\n%s", want.command, want.marker, stdout)
		}
	}

	// The demo's own project must be the one it runs in: a replay that fell back
	// to the repository root would print a Go stack and still show three
	// commands.
	if strings.Contains(stdout, "stack: go") {
		t.Errorf("the replay ran outside docs/demo/example (a Go project was detected):\n%s", stdout)
	}
}

// Defends the render instruction the demo README owes the operator: the exact
// command that reproduces the committed GIF. Written here because docs/ prose
// drifts silently, and this one is the only route back to the recording.
func TestDemoReadmeNamesTheRenderCommand(t *testing.T) {
	body, err := os.ReadFile("../../docs/demo/README.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"vhs docs/demo/llm-data.tape", "make build"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("docs/demo/README.md does not name %q", want)
		}
	}
}
