package ui

import (
	"context"
	"fmt"
	"slices"

	agentv1 "weave/proto/gen/agent/v1"

	"connectrpc.com/connect"
	tea "github.com/charmbracelet/bubbletea"
)

// Explainer: the first thing written in a pass pauses the agent, and nobody asked for that in so
// many words, so it has to be a thing the panel does rather than a thing the reader arranges. An
// agent writing in the tree while a person edits it is the collision the pause exists to prevent,
// and a comment pinned to a snapshot the agent has already written past is the same collision
// arriving later.

// passIntent is what the reader pressed, which is what happens once the agent has stopped.
type passIntent uint8

const (
	// intentComment opens the composer on the line under the cursor.
	intentComment passIntent = iota
	// intentEdit opens the reader's editor on the file under the cursor.
	intentEdit
)

// reviewPausedMsg is the agent having stopped, or having refused to.
type reviewPausedMsg struct {
	agentID string
	intent  passIntent
	err     error
}

// startReviewPass stops the agent and opens the pass on what the reader pressed, and is a no-op
// for a pass already under way.
func (m model) startReviewPass(intent passIntent) (tea.Model, tea.Cmd) {
	// Explainer: an agent that is dormant or archived cannot write, so there is nothing for a
	// pause to stop and nothing to let go of afterwards. The editor still opens on its worktree,
	// which is what the review screen keeps that key for.
	if _, dead := m.isReviewReadOnly(); dead {
		m.review.mode = modeReviewing

		return m.openReviewPass(intent)
	}

	// An agent the person stopped themselves is already where the pass needs it, and letting it
	// go at the end of the pass is not this screen's to do.
	if item, ok := m.getReviewAgentItem(); ok && item.agent.HeldAt != nil {
		m.review.mode = modeReviewing
		m.review.paused = true
		m.review.ourPause = false

		return m.openReviewPass(intent)
	}

	m.review.mode = modeReviewing
	m.review.pausing = true

	stopping := m.holdReviewAgentCmd(intent)

	// Explainer: a comment writes nothing to the worktree, so it opens while the agent is still
	// coming to a stop. An editor does write to it, and an interrupt that never lands would put a
	// second writer in a tree the agent is still using, so that one waits for the answer.
	if intent == intentEdit {
		return m, stopping
	}

	next, opening := m.openReviewPass(intent)
	m, _ = next.(model)

	return m, tea.Batch(stopping, opening)
}

// holdReviewAgentCmd asks for the agent to stop and reports what came back.
func (m model) holdReviewAgentCmd(intent passIntent) tea.Cmd {
	agents, agentID := m.api.agents, m.review.agentID

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
		defer cancel()

		_, err := agents.HoldAgent(ctx, connect.NewRequest(&agentv1.HoldAgentRequest{Id: agentID}))

		return reviewPausedMsg{
			agentID: agentID,
			intent:  intent,
			err:     err,
		}
	}
}

// applyReviewPaused opens the pass the reader asked for, or gives it up when the agent would not
// stop, which is the one case nobody may be let into the editor in.
func (m model) applyReviewPaused(msg reviewPausedMsg) (tea.Model, tea.Cmd) {
	if msg.agentID != m.review.agentID {
		return m, nil
	}

	m.review.pausing = false

	// Explainer: the hold outlives the keypress that asked for it, so the pass may be over before
	// the agent has stopped. An agent stopped for a pass nobody is making is one nothing left in
	// the panel would ever let go, so it is let go here rather than opening an editor onto a
	// screen that has gone back to watching.
	if m.review.mode != modeReviewing {
		if msg.err != nil {
			return m, nil
		}

		m.review.paused = true
		m.review.ourPause = true

		return m.endReviewPass(abandonedSaid)
	}

	if msg.err != nil {
		// A comment written against a snapshot the agent is free to write past is the collision
		// the pass exists to prevent, so the composer goes with the pass rather than standing on
		// without one.
		m = m.cancelReviewMessage()
		m.review.mode = modeWatching
		m.report("Could not pause "+m.review.agentName, msg.err)

		return m, nil
	}

	m.review.paused = true
	m.review.ourPause = true

	// The composer was opened when the key was pressed, so there is nothing left to open here.
	if msg.intent == intentComment {
		return m, nil
	}

	return m.openReviewPass(msg.intent)
}

// openReviewPass does the thing the pass was opened for.
func (m model) openReviewPass(intent passIntent) (tea.Model, tea.Cmd) {
	switch intent {
	case intentEdit:
		return m.editReviewFile()
	case intentComment:
	}

	return m.composeReview()
}

// endReviewPass goes back to watching and lets the agent go, telling it what the reader did while
// it was stopped.
func (m model) endReviewPass(said string) (tea.Model, tea.Cmd) {
	ours, touched := m.review.ourPause, slices.Clone(m.review.touched)
	agentID := m.review.agentID

	m.review.mode = modeWatching
	m.review.paused = false
	m.review.ourPause = false
	m.review.touched = nil
	m.review.pausing = false

	// Explainer: a pause the reader took themselves is theirs to let go, from the screen they took
	// it on, so the files they opened are left where that release will find them rather than
	// reaching an agent nobody is letting go yet.
	if !ours {
		m.rememberEdited(agentID, touched)

		return m, nil
	}

	item, ok := m.getReviewAgentItem()
	if !ok {
		m.rememberEdited(agentID, touched)

		return m, nil
	}

	agent, name := item.agent, m.review.agentName

	m.rememberEdited(agentID, touched)

	return m, m.releaseCmd("Could not let "+name+" go", agent, said)
}

// rememberEdited keeps the files a reader opened over a paused agent until whoever holds that
// pause lets it go.
func (m *model) rememberEdited(agentID string, touched []string) {
	if len(touched) == 0 {
		return
	}

	if m.edited == nil {
		m.edited = map[string][]string{}
	}

	for _, path := range touched {
		if !slices.Contains(m.edited[agentID], path) {
			m.edited[agentID] = append(m.edited[agentID], path)
		}
	}
}

// forgetEdited drops the files a reader opened over an agent.
func (m *model) forgetEdited(agentID string) {
	delete(m.edited, agentID)
}

// touchedSaid names the files the reader opened an editor on, since the agent is coming back to a
// worktree that moved under it and the panel knows where to look.
func touchedSaid(touched []string) string {
	if len(touched) == 0 {
		return ""
	}

	return fmt.Sprintf(" I had these files open in an editor while you were paused, so read them "+
		"before you trust what you remember of them: %s.", joinList(touched))
}

// joinList is a list of paths as a sentence rather than as a slice.
func joinList(items []string) string {
	switch len(items) {
	case 1:
		return items[0]
	case 2:
		return items[0] + " and " + items[1]
	}

	said := ""
	for i, item := range items[:len(items)-1] {
		if i > 0 {
			said += ", "
		}

		said += item
	}

	return said + " and " + items[len(items)-1]
}
