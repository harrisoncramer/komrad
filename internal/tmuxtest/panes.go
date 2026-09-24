package tmuxtest

import (
	"time"

	"weave/internal/harnesstest"
	agentstore "weave/internal/store/agent"
)

// Panes stands in for tmux, showing one frame until a cut of its input box has landed and
// the frame after it once it has.
type Panes struct {
	Alive    bool
	Shell    bool
	InMode   bool
	ClientAt bool
	Frame    string

	// AfterCut is the frame the pane shows once its line has been cut, an empty box unless a
	// test says otherwise.
	AfterCut string

	// StaleReads is how many reads after the cut still show the frame from before it, which is
	// a CLI that has yet to repaint.
	StaleReads int

	// PasteErr and YankErr are what tmux refuses with.
	PasteErr error
	YankErr  error

	// Actions is what was done to the pane, in the order it was done.
	Actions  []string
	Pasted   []string
	captures int
	cutAt    int
	cut      bool
}

func (f *Panes) IsAgentRunning(agentstore.Agent) bool { return f.Alive }

func (f *Panes) SendKeys(string, ...string) error { return nil }

func (f *Panes) IsPaneInMode(string) bool { return f.InMode }

func (f *Panes) IsPaneRunningCLI(string) bool { return !f.Shell }

func (f *Panes) CapturePane(string) string {
	f.captures++

	if f.cut && f.captures > f.cutAt+f.StaleReads {
		if f.AfterCut != "" {
			return f.AfterCut
		}

		return harnesstest.FrameIdle
	}

	return f.Frame
}

func (f *Panes) IsClientAtPane(string, time.Duration) bool { return f.ClientAt }

func (f *Panes) PasteAndSubmit(_, text string) error {
	if f.PasteErr != nil {
		f.Actions = append(f.Actions, "paste-failed")

		return f.PasteErr
	}

	f.Actions = append(f.Actions, "paste")
	f.Pasted = append(f.Pasted, text)

	return nil
}

func (f *Panes) CutLine(string) error {
	f.Actions = append(f.Actions, "cut")
	f.cut = true
	f.cutAt = f.captures

	return nil
}

func (f *Panes) YankLine(string) error {
	if f.YankErr != nil {
		f.Actions = append(f.Actions, "yank-failed")

		return f.YankErr
	}

	f.Actions = append(f.Actions, "yank")

	return nil
}
