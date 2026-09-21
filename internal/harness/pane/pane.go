package pane

import (
	"time"

	"weave/internal/harness"
)

// draftStaleAfter is how long an input box has to hold the same text before what it holds is
// taken for abandoned.
const draftStaleAfter = 30 * time.Second

// cutSettleReads is how many times the frame is read for a cut tmux has already written to the
// pane.
const cutSettleReads = 15

// cutSettleInterval is how long is left between those reads.
const cutSettleInterval = 20 * time.Millisecond

// clientPresenceWindow is how recently somebody has to have pressed a key at an agent's window
// to count as being there.
const clientPresenceWindow = time.Minute

// terminal is the handful of moves a pane channel makes on the tmux server.
type terminal interface {
	CapturePane(pane string) string
	IsPaneInMode(pane string) bool
	IsPaneRunningCLI(pane string) bool
	IsClientAtPane(pane string, within time.Duration) bool
	PasteAndSubmit(target, text string) error
	CutLine(target string) error
	YankLine(target string) error
	SendKeys(target string, keys ...string) error
}

// harnessGetter hands back the harness that reads a pane's frames.
type harnessGetter interface {
	GetByID(id string) (harness.Harness, error)
}

// NewPaneParams is what a pane channel and observer are built over.
type NewPaneParams struct {
	Terminal  terminal
	Harnesses harnessGetter

	// Now is the clock a draft's stillness is measured on, defaulting to time.Now.
	Now func() time.Time
}

// Pane reaches an agent through the terminal it is running in.
type Pane struct {
	terminal  terminal
	harnesses harnessGetter
	now       func() time.Time
	drafts    drafts
}

// NewPane builds the channel and observer over the tmux server and the harness registry.
func NewPane(params NewPaneParams) *Pane {
	now := params.Now
	if now == nil {
		now = time.Now
	}

	return &Pane{
		terminal:  params.Terminal,
		harnesses: params.Harnesses,
		now:       now,
	}
}

var (
	_ harness.Channel  = (*Pane)(nil)
	_ harness.Observer = (*Pane)(nil)
)

// Forget drops what was remembered of an agent's input box.
func (p *Pane) Forget(agentID string) {
	p.drafts.forget(agentID)
}
