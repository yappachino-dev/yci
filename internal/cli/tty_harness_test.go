package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/spf13/cobra"

	"github.com/yappachino-dev/yci/internal/app"
)

// The forms in this package are driven by a real pty rather than a
// strings.Reader, for two reasons that both matter.
//
// Bubbletea inspects the input for a file descriptor: given a plain reader
// there is no terminal, no key decoding, and the TTY guard rejects the run
// before a form is ever built. A pty is the only way to exercise the path a
// person actually takes.
//
// It also settles a question left open for several sessions: whether typing
// into the token prompt, submitting it, and then confirming actually works
// together. Earlier attempts drove input in a way the form never saw.

// keyEnter submits the focused field. Bubbletea decodes "\r" as KeyEnter.
const keyEnter = "\r"

// keySpace toggles the focused option in a multi-select.
const keySpace = " "

// keyDown is the escape sequence for the down arrow, which is what moves the
// cursor in a multi-select.
const keyDown = "\x1b[B"

// ansiPattern matches the escape sequences a full-screen renderer emits.
var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]|\x1b\][^\x07]*(\x07|\x1b\\)|\x1b[=>()][A-Za-z0-9]?`)

func stripANSI(s string) string { return ansiPattern.ReplaceAllString(s, "") }

// ttySession is a command wired to a pty, with a way to type into it and
// read what the form rendered.
type ttySession struct {
	cmd *cobra.Command
	// pty is the master side, kept so keys can be written to it.
	pty *os.File

	mu   sync.Mutex
	seen strings.Builder
}

// newTTYCommand builds a command with a pty on stdin and a buffer on stdout.
//
// Output goes to a buffer rather than the pty: the form only needs somewhere
// to render, and asserting against captured text is far easier than driving
// a terminal emulator.
func newTTYCommand(t *testing.T, c *app.Container) *ttySession {
	t.Helper()

	ptmx, tty, err := pty.Open()
	if err != nil {
		t.Skipf("no pty available: %v", err)
	}
	t.Cleanup(func() { ptmx.Close(); tty.Close() })

	s := &ttySession{pty: ptmx}
	s.cmd = &cobra.Command{}
	// The program reads the SLAVE. Writing to the master (ptmx) is what
	// delivers the keys, but reading the master gives the control side, not
	// the input queue — the form renders but never sees a keystroke, which
	// is exactly how an earlier attempt to verify this looked like it worked
	// when nothing had been typed.
	s.cmd.SetIn(tty)
	s.cmd.SetOut(&lockedWriter{mu: &s.mu, w: &s.seen})
	s.cmd.SetErr(s.cmd.OutOrStdout())
	s.cmd.SetContext(WithContainer(context.Background(), c))

	return s
}

type lockedWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.w.Write(p)
}

// typeKeys writes keys to the pty, one at a time with a pause between them.
//
// The pause matters. A whole script delivered as a single write reaches
// Bubbletea as one input chunk and is decoded as one keystroke, so a form
// never sees the individual keys at all. This is the mistake that made an
// earlier attempt to verify the token prompt look like it worked when nothing
// had been typed.
//
// Escape sequences are the exception and must be written in one piece. An
// arrow key is ESC [ B, and Bubbletea only reads it as KeyDown if all three
// bytes arrive together; delivered a byte at a time they decode as an
// unrelated key and the cursor never moves. This is a property of the test
// harness, not of the form — a real terminal delivers an arrow key whole.
func (s *ttySession) typeKeys(t *testing.T, keys string) {
	t.Helper()

	for _, chunk := range splitEscapeSequences(keys) {
		if _, err := s.pty.Write([]byte(chunk)); err != nil {
			t.Fatalf("write %q: %v", chunk, err)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// splitEscapeSequences divides input into units that must each be written in
// one call: a printable character on its own, and a complete escape sequence
// kept intact.
func splitEscapeSequences(keys string) []string {
	var (
		out     []string
		runes   = []rune(keys)
		pending string
	)

	flush := func() {
		if pending != "" {
			out = append(out, pending)
			pending = ""
		}
	}

	for i := 0; i < len(runes); i++ {
		switch {
		case runes[i] == 0x1b:
			// Take the whole CSI / SS3 sequence.
			j := i + 1
			if j < len(runes) && (runes[j] == '[' || runes[j] == 'O') {
				j++
				for j < len(runes) && runes[j] >= 0x20 && runes[j] <= 0x3f {
					j++
				}
				if j < len(runes) {
					j++
				}
			}
			flush()
			out = append(out, string(runes[i:j]))
			i = j - 1
		case runes[i] == '\r', runes[i] == '\n':
			flush()
			out = append(out, string(runes[i]))
		default:
			pending += string(runes[i])
		}
	}
	flush()

	return out
}

// output is everything the form rendered, with escape sequences removed.
func (s *ttySession) output() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return stripANSI(s.seen.String())
}

// contains asserts the rendered output mentions want.
func (s *ttySession) contains(t *testing.T, want string) {
	t.Helper()

	if !strings.Contains(s.output(), want) {
		t.Errorf("form output does not mention %q\n--- rendered ---\n%s", want, s.output())
	}
}

// waitFor polls the rendered output until it contains want, so a test does
// not depend on how quickly the form redraws.
func (s *ttySession) waitFor(t *testing.T, want string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(s.output(), want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("timed out after %s waiting for %q\n--- rendered ---\n%s", timeout, want, s.output())
}

// waitForOption polls until the focused option's display key contains want.
//
// Timing matters here. A multi-key sequence like the arrow escape arrives as
// one input chunk that the renderer has not necessarily processed by the time
// the next key is written, and the gap that is ample normally is too short
// under -race. Waiting for the rendered state to change is what makes these
// tests reliable rather than merely usually-passing.
func (s *ttySession) waitForOption(t *testing.T, contains, want string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if optionRow(s.output(), contains) == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for option row %q to become %q\n%s", contains, want, s.output())
}

// optionRow returns the rendered state of one option in the MOST RECENT
// frame: "focused-selected", "focused-unselected", "selected" or
// "unselected".
//
// Scanning from the end matters. Bubbletea redraws by appending, so the
// buffer holds every frame the form ever drew; reading from the start
// returns the first frame's state forever and a cursor move looks like it
// never happened.
func optionRow(rendered, contains string) string {
	lines := strings.Split(rendered, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if !strings.Contains(line, contains) {
			continue
		}

		state := "unselected"
		if strings.Contains(line, "[•]") {
			state = "selected"
		}
		if strings.Contains(line, "> ") {
			return "focused-" + state
		}

		return state
	}

	return "absent"
}

// await starts fn in a goroutine and returns a function that yields its
// result, so a test can type while the form is waiting for input.
func await(fn func() error) func() error {
	done := make(chan error, 1)
	go func() { done <- fn() }()

	var result error
	return func() error {
		select {
		case result = <-done:
			return result
		case <-time.After(15 * time.Second):
			return errors.New("form did not finish")
		}
	}
}
