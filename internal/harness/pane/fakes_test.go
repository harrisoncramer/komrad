package pane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"weave/internal/harness"
	"weave/internal/logging"

	"github.com/stretchr/testify/require"
)

const (
	idleFrame      = "frame: idle"
	composingFrame = "frame: composing"
	busyFrame      = "frame: working"
)

const testHarnessID = "claude"

// target is the agent every test in this package speaks to.
var target = harness.Target{AgentID: "agent-a", Pane: "%1", HarnessID: testHarnessID}

// fakeTerminal stands in for the tmux server, recording what was done to the pane in the order
// it was done.
type fakeTerminal struct {
	inMode   bool
	clientAt bool
	frame    string

	// afterCut is the frame the pane shows once its line has been cut, an empty box unless a
	// test says otherwise.
	afterCut string

	// staleReads is how many reads after the cut still show the frame from before it, which is
	// a CLI that has yet to repaint.
	staleReads int

	// pasteErr and yankErr are what tmux refuses with, which is a query timing out under load
	// or a pane that has just gone.
	pasteErr error
	yankErr  error

	// actions is what was done to the pane, in the order it was done.
	actions  []string
	pasted   []string
	captures int
	cutAt    int
	cut      bool
}

func (f *fakeTerminal) SendKeys(_ string, keys ...string) error {
	f.actions = append(f.actions, "keys:"+strings.Join(keys, ","))

	return nil
}

func (f *fakeTerminal) IsPaneInMode(string) bool { return f.inMode }

func (f *fakeTerminal) CapturePane(string) string {
	f.captures++

	if f.cut && f.captures > f.cutAt+f.staleReads {
		if f.afterCut != "" {
			return f.afterCut
		}

		return idleFrame
	}

	return f.frame
}

func (f *fakeTerminal) IsClientAtPane(string, time.Duration) bool { return f.clientAt }

func (f *fakeTerminal) PasteAndSubmit(_, text string) error {
	if f.pasteErr != nil {
		f.actions = append(f.actions, "paste-failed")

		return f.pasteErr
	}

	f.actions = append(f.actions, "paste")
	f.pasted = append(f.pasted, text)

	return nil
}

func (f *fakeTerminal) CutLine(string) error {
	f.actions = append(f.actions, "cut")
	f.cut = true
	f.cutAt = f.captures

	return nil
}

func (f *fakeTerminal) YankLine(string) error {
	if f.yankErr != nil {
		f.actions = append(f.actions, "yank-failed")

		return f.yankErr
	}

	f.actions = append(f.actions, "yank")

	return nil
}

// fakeHarness reads the frames the tests in this package write.
type fakeHarness struct {
	interruptKeys []string
}

func (f *fakeHarness) ID() string     { return testHarnessID }
func (f *fakeHarness) Binary() string { return testHarnessID }

func (f *fakeHarness) BuildCommand(harness.Launch) (string, error) { return "agent-cli", nil }

func (f *fakeHarness) MintSessionID() string { return "" }

func (f *fakeHarness) InterruptKeys() []string {
	if f.interruptKeys == nil {
		return []string{"Escape"}
	}

	return f.interruptKeys
}

func (f *fakeHarness) ReadReport(harness.Event, []byte) (harness.Report, error) {
	return harness.Report{}, nil
}

func (f *fakeHarness) ReadInput(frame string) harness.Input {
	switch {
	case frame == busyFrame:
		return harness.Input{Activity: harness.ActivityWorking}
	case strings.HasPrefix(frame, composingFrame):
		return harness.Input{Activity: harness.ActivityComposing, Held: composingLines(strings.TrimPrefix(frame, composingFrame))}
	case frame == "":
		return harness.Input{Activity: harness.ActivityUnknown}
	default:
		return harness.Input{Activity: harness.ActivityIdle, Held: []string{""}}
	}
}

// composingLines is the box's lines out of the frame a test wrote, which is one line of its own
// unless the test spelled some.
func composingLines(held string) []string {
	if held == "" {
		return []string{"half a question about"}
	}

	return strings.Split(strings.TrimPrefix(held, ": "), "|")
}

func (f *fakeHarness) LogPath(string) string { return "" }

func (f *fakeHarness) FindSession(string) (string, error) { return "", nil }

func (f *fakeHarness) ReadLog(string, int64) (harness.LogRead, error) {
	return harness.LogRead{}, nil
}

// fakeHarnesses answers every lookup with the one harness it holds.
type fakeHarnesses struct {
	harness harness.Harness
}

func (f fakeHarnesses) GetByID(string) (harness.Harness, error) {
	if f.harness == nil {
		return nil, errors.New("no harness")
	}

	return f.harness, nil
}

// harnessesFailingAfter answers a given number of pane reads and refuses every one after that,
// which is a frame that goes unreadable part-way through a cut.
type harnessesFailingAfter struct {
	harness harness.Harness
	reads   int
	answer  int
}

func (f *harnessesFailingAfter) GetByID(string) (harness.Harness, error) {
	f.reads++
	if f.reads > f.answer {
		return nil, errors.New("the frame went unreadable")
	}

	return f.harness, nil
}

// composingPane is a channel over one pane, whose clock a test moves by writing to the time it
// returns.
func composingPane(t *testing.T, terminal *fakeTerminal, now *time.Time) *Pane {
	t.Helper()

	return New(Params{
		Terminal:  terminal,
		Harnesses: fakeHarnesses{harness: &fakeHarness{}},
		Now:       func() time.Time { return *now },
	})
}

// deliver is one whole attempt at saying something, which is what the delivery service does with
// a channel: ask whether it would be taken, then say it.
func deliver(ctx context.Context, t *testing.T, p *Pane) (bool, error) {
	t.Helper()

	ready, err := p.Ready(ctx, target)
	if err != nil || !ready {
		return false, err
	}

	return p.Send(ctx, target, "weave: 1 piece(s) of unread mail, call read_mail")
}

func loggedContext(t *testing.T) (context.Context, func() []map[string]any) {
	t.Helper()

	var written bytes.Buffer

	ctx := logging.WithLogger(t.Context(), logging.New(&written, slog.LevelDebug))

	return ctx, func() []map[string]any {
		records := make([]map[string]any, 0, 2)
		for _, line := range strings.Split(strings.TrimSpace(written.String()), "\n") {
			if line == "" {
				continue
			}

			var record map[string]any
			require.NoError(t, json.Unmarshal([]byte(line), &record))
			records = append(records, record)
		}

		return records
	}
}
