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
	"weave/internal/harnesstest"
	"weave/internal/logging"

	"github.com/stretchr/testify/require"
)

// target is the agent every test in this package speaks to.
var target = harness.Target{
	AgentID:   "agent-a",
	Pane:      "%1",
	HarnessID: harnesstest.FakeID,
}

// fakeTerminal stands in for the tmux server, recording what was done to the pane in the order
// it was done.
type fakeTerminal struct {
	inMode   bool
	clientAt bool
	frame    string
	shell    bool

	// afterCut is the frame the pane shows once its line has been cut, an empty box unless a
	// test says otherwise.
	afterCut string

	// staleReads is how many reads after the cut still show the frame from before it, which is
	// a CLI that has yet to repaint.
	staleReads int

	// pasteErr and yankErr are what tmux refuses with.
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

func (f *fakeTerminal) IsPaneRunningCLI(string) bool { return !f.shell }

func (f *fakeTerminal) CapturePane(string) string {
	f.captures++

	if f.cut && f.captures > f.cutAt+f.staleReads {
		if f.afterCut != "" {
			return f.afterCut
		}

		return harnesstest.FrameIdle
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

	return NewPane(NewPaneParams{
		Terminal:  terminal,
		Harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}},
		Now:       func() time.Time { return *now },
	})
}

// deliver is the two calls the delivery service makes: ask whether the line would be taken,
// then say it.
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
