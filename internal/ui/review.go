package ui

import (
	"time"

	"komrad/internal/ui/panel"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
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
	// notice is what the last load had to say about the round it settled on.
	notice string

	// diff is the patch on screen and where the reader is standing in it.
	diff reviewDiff

	// threads is the conversation in the sidebar and what the reader is writing into it.
	threads reviewThreads

	// hold is the reader's own hold over the work, and the editor it may hold open.
	hold reviewHold

	// offer is the forge thread J last chose, empty until it has chosen one.
	offer        string
	offers       reviewOffers
	filesFocused bool

	// filesFilter is the pattern the file tree is searched or narrowed by.
	filesFilter filterState
}

func (r reviewScreen) parent() screen {
	return screenAgents
}

// crumbs runs through the agent whose work is under review.
func (r reviewScreen) crumbs(*model) crumbs {
	return crumbs{trail: []string{r.agentName, "review"}}
}

// takesEveryKey reports whether the review is mid-input, in which case a key bound elsewhere
// in the panel belongs here instead.
func (r reviewScreen) takesEveryKey() bool {
	return r.isWriting() || r.diff.filter.typing || r.filesFilter.typing
}

// isWriting reports whether the reader is composing a message or asking what to send.
func (r reviewScreen) isWriting() bool {
	return r.threads.composing || r.threads.releasing
}

// isBusy reports whether the reader is part way through something a restart would lose.
func (r reviewScreen) isBusy() bool {
	return r.isWriting() || r.hold.isHeld()
}

// isDiffFocused is whether the patch holds the keys.
func (r reviewScreen) isDiffFocused(m *model) bool {
	return m.isListFocused() && !r.isWriting() && !r.isBlockFocused(m)
}

// update takes the review's own answers and, once nothing in the sidebar has them, the keys.
func (r *reviewScreen) update(m *model, msg tea.Msg) tea.Cmd {
	cmd := r.route(m, msg)

	return tea.Batch(cmd, r.followWorld(m))
}

func (r *reviewScreen) route(m *model, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case reviewLoadedMsg:
		return r.applyLoaded(m, msg)
	case reviewPausedMsg:
		return r.applyPaused(m, msg)
	case reviewWroteMsg:
		return r.applyWrote(m, msg)
	case reviewOffersTracedMsg:
		r.applyOffersTraced(msg)

		return nil
	case messageEditedMsg:
		return r.applyMessageEdited(m, msg)
	case tea.KeyPressMsg:
		return r.updateKey(m, msg)
	}

	return nil
}

// enter leaves the review as it stands.
func (r *reviewScreen) enter(*model, screenTarget) tea.Cmd {
	return nil
}

// enterReview is the agents screen's way onto the review of an agent's diff, with the threads
// in the sidebar and the diff in the stage.
func (m *model) enterReview(agent panel.Agent) tea.Cmd {
	if !isReviewable(agent) {
		return nil
	}

	left := m.enterScreen(screenReview)

	return tea.Batch(left, m.review.open(m, agent))
}

var (
	_ columned = (*reviewScreen)(nil)
	_ blocked  = (*reviewScreen)(nil)
)

func (r *reviewScreen) doesDrawFromListing() bool {
	return false
}

func (r *reviewScreen) column() columnSpec {
	return reviewColumn()
}

func (r reviewScreen) typedLine() (lineInput, bool) {
	if r.filesFilter.typing {
		return r.filesFilter.input, true
	}

	return r.diff.filter.input, r.diff.filter.typing
}
