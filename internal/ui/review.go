package ui

import (
	"time"

	"weave/config"
	agentv1 "weave/proto/gen/agent/v1"

	tea "github.com/charmbracelet/bubbletea"
)

const reviewTimeout = 30 * time.Second

// reviewState is the review of one agent's diff, while that screen is up.
type reviewState struct {
	agentID   string
	agentName string
	dir       string
	root      string
	exclude   []string
	round     *agentv1.ReviewRound
	loading   bool
	notice    string

	// diff is the patch on screen and where the reader is standing in it.
	diff reviewDiff

	// threads is the conversation in the sidebar and what the reader is writing into it.
	threads reviewThreads

	// pass is the reader's own pass over the work, and the editor it may hold open.
	pass reviewPass
}

// doesTakeEveryKey reports whether the review is mid-input, in which case a key bound elsewhere
// in the panel belongs here instead.
func (r reviewState) doesTakeEveryKey() bool {
	return r.isWriting() || r.diff.filter.typing
}

// isWriting reports whether the reader is composing a message or asking what to send.
func (r reviewState) isWriting() bool {
	return r.threads.composing || r.threads.releasing
}

// isThreadTaken reports whether the reader has taken the keys onto a conversation the cursor is
// standing on.
func (r reviewState) isThreadTaken() bool {
	if !r.threads.focus {
		return false
	}

	_, ok := r.diff.threadAtCursor()

	return ok
}

// reviewScreen is the review screen behind the panel's one door.
type reviewScreen struct{}

var (
	_ detailed = reviewScreen{}
	_ columned = reviewScreen{}
)

func (reviewScreen) update(m model, msg tea.Msg) (tea.Model, tea.Cmd) {
	return onKey(m, msg, model.updateReview)
}

func (reviewScreen) view(m model) string {
	return m.reviewView()
}

func (reviewScreen) needs() screenNeeds {
	return screenNeeds{}
}

// open is the screen as it stands, nothing being drawn over the review.
func (reviewScreen) open(m model, _ screenTarget) (tea.Model, tea.Cmd) {
	return m, nil
}

func (reviewScreen) takesEveryKey(m model) bool {
	return m.review.doesTakeEveryKey()
}

func (reviewScreen) details(m model) details {
	return m.reviewDetails()
}

func (reviewScreen) detailsQuit(keys config.Keybindings) []string {
	return keys.Review.Quit
}

func (reviewScreen) detailsFit(m model) bool {
	return m.doesReviewFitDetails()
}

func (reviewScreen) detailsRoom(m model) int {
	return m.reviewDetailsRoom()
}

func (reviewScreen) column() columnSpec {
	return reviewColumn()
}
