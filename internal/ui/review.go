package ui

import (
	"time"

	"weave/config"
	"weave/internal/ui/panel"
	agentv1 "weave/proto/gen/agent/v1"

	tea "github.com/charmbracelet/bubbletea"
)

const reviewTimeout = 30 * time.Second

// reviewScreen is the review of one agent's diff, while that screen is up.
type reviewScreen struct {
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

// takesEveryKey reports whether the review is mid-input, in which case a key bound elsewhere
// in the panel belongs here instead.
func (r reviewScreen) takesEveryKey() bool {
	return r.isWriting() || r.diff.filter.typing
}

// isWriting reports whether the reader is composing a message or asking what to send.
func (r reviewScreen) isWriting() bool {
	return r.threads.composing || r.threads.releasing
}

// isBusy reports whether the reader is part way through something a restart would lose.
func (r reviewScreen) isBusy() bool {
	return r.isWriting() || r.pass.mode != modeWatching || r.pass.atEditor
}

// isThreadTaken reports whether the reader has taken the keys onto a conversation the cursor is
// standing on.
func (r reviewScreen) isThreadTaken() bool {
	if !r.threads.focus {
		return false
	}

	_, ok := r.diff.threadAtCursor()

	return ok
}

// isThreadFocused is whether the sidebar holds the keys, which it does mid-input and while the
// conversation has been taken.
func (r reviewScreen) isThreadFocused(s panelShared) bool {
	if r.isWriting() {
		return true
	}

	if s.draw.isDetailsFocused() {
		return false
	}

	return r.isThreadTaken()
}

// isDiffFocused is whether the patch holds the keys, which it does whenever the record and the
// conversation beside it do not.
func (r reviewScreen) isDiffFocused(s panelShared) bool {
	return s.draw.isListFocused() && !r.isThreadFocused(s)
}

// update takes the review's own answers and, once nothing in the sidebar has them, the keys.
func (r *reviewScreen) update(h host, s panelShared, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case reviewLoadedMsg:
		return r.applyLoaded(h, s, msg)
	case reviewPausedMsg:
		return r.applyPaused(h, s, msg)
	case reviewWroteMsg:
		return r.applyWrote(h, s, msg)
	case reviewRefreshedMsg:
		r.applyRefreshed(s, msg)

		return nil
	case reviewDiffPolledMsg:
		return r.applyDiffPolled(h, s, msg)
	case messageEditedMsg:
		r.applyMessageEdited(h, s, msg)

		return nil
	case tea.KeyMsg:
		return r.updateKey(h, s, msg)
	}

	return nil
}

// enter leaves the review as it stands.
func (r *reviewScreen) enter(host, panelShared, screenTarget) tea.Cmd {
	return nil
}

// enterReview is the agents screen's way onto the review of an agent's diff, with the threads
// in the sidebar and the diff in the stage.
func (m *model) enterReview(agent panel.Agent) tea.Cmd {
	if !isReviewable(agent) {
		return nil
	}

	m.enterScreen(screenReview)

	return m.review.open(m, m.shared(), agent)
}

var (
	_ detailed = (*reviewScreen)(nil)
	_ columned = (*reviewScreen)(nil)
)

func (r *reviewScreen) needs() screenNeeds {
	return screenNeeds{}
}

func (r *reviewScreen) detailsQuit(keys config.Keybindings) []string {
	return keys.Review.Quit
}

func (r *reviewScreen) column() columnSpec {
	return reviewColumn()
}
