package ui

import (
	"context"
	"fmt"
	"slices"

	agentv1 "weave/proto/gen/agent/v1"

	"connectrpc.com/connect"
	tea "github.com/charmbracelet/bubbletea"
)

// reviewPass is a pass over the work: whether one is under way, what it did to the agent, and
// the editor it holds open.
type reviewPass struct {
	// mode is which of the two things the screen is doing: watching the work, or making a pass
	// over it in which both writing keys are available.
	mode reviewMode
	// paused says the agent is stopped for the pass, whoever stopped it.
	paused bool
	// pausing is set while the panel is asking for the agent to stop.
	pausing bool
	// ourPause says this pass is what stopped the agent, so ending the pass is what lets it go.
	ourPause bool
	// touched are the files the reader opened an editor on during the pass, oldest first, which
	// is what the agent is told about when it is let go.
	touched []string

	// atEditor says your own editor holds the stage, so the panel draws its sidebar alone.
	atEditor    bool
	editingPath string
	// editorPane is the pane the editor was given, empty where the split was refused, which is
	// what says whether a pane going away is the editor being closed.
	editorPane string
}

// end puts the pass down, leaving an editor it holds open where it is, and says whether it was
// this pass that stopped the agent and which files the reader opened while it was.
func (p *reviewPass) end() (bool, []string) {
	ours, touched := p.ourPause, p.touched

	p.mode = modeWatching
	p.paused = false
	p.pausing = false
	p.ourPause = false
	p.touched = nil

	return ours, touched
}

// openEditor records the editor holding the stage on one file, in the pane the split gave it.
func (p *reviewPass) openEditor(path, pane string) {
	p.atEditor = true
	p.editingPath = path
	p.editorPane = pane
	if !slices.Contains(p.touched, path) {
		p.touched = append(p.touched, path)
	}
}

// closeEditor gives the stage back, staying in the pass.
func (p *reviewPass) closeEditor() {
	p.atEditor = false
	p.editingPath = ""
	p.editorPane = ""
}

// passIntent is what the reader pressed.
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

// startPass stops the agent and opens the pass on what the reader pressed, and is a no-op for a
// pass already under way.
func (r *reviewScreen) startPass(h host, s panelShared, intent passIntent) tea.Cmd {
	if _, dead := r.isReadOnly(s); dead {
		r.pass.mode = modeReviewing

		return r.openPass(h, s, intent)
	}

	// An agent the person stopped themselves is already where the pass needs it, and letting it
	// go at the end of the pass is not this screen's to do.
	if item, ok := r.agentRow(s); ok && item.Agent.HeldAt != nil {
		r.pass.mode = modeReviewing
		r.pass.paused = true
		r.pass.ourPause = false

		return r.openPass(h, s, intent)
	}

	r.pass.mode = modeReviewing
	r.pass.pausing = true

	stopping := r.holdAgentCmd(s, intent)

	if intent == intentEdit {
		return stopping
	}

	return tea.Batch(stopping, r.openPass(h, s, intent))
}

// holdAgentCmd asks for the agent to stop and reports what came back.
func (r reviewScreen) holdAgentCmd(s panelShared, intent passIntent) tea.Cmd {
	agents, agentID := s.api.agents, r.agentID

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

// applyPaused opens the pass the reader asked for, or gives it up when the agent would not stop.
func (r *reviewScreen) applyPaused(h host, s panelShared, msg reviewPausedMsg) tea.Cmd {
	if msg.agentID != r.agentID {
		return nil
	}

	r.pass.pausing = false

	if msg.err != nil {
		// A comment written against a snapshot the agent is free to write past is the collision
		// the pass exists to prevent, so the composer goes with the pass rather than standing on
		// without one.
		r.threads.cancelMessage()
		r.pass.mode = modeWatching
		h.report("Could not pause "+r.agentName, msg.err)

		return nil
	}

	r.pass.paused = true
	r.pass.ourPause = true

	// The composer was opened when the key was pressed, so there is nothing left to open here.
	if msg.intent == intentComment {
		return nil
	}

	return r.openPass(h, s, msg.intent)
}

func (m model) isPassing(agentID string) bool {
	return m.screen == screenReview && m.review.agentID == agentID && m.review.pass.mode == modeReviewing
}

func (m *model) releaseAbandonedPause(msg reviewPausedMsg) tea.Cmd {
	if msg.err != nil {
		return nil
	}

	item, ok := m.listing.liveAgent(msg.agentID)
	if !ok {
		return nil
	}

	return m.releaseCmd("Could not let "+item.Agent.Name+" go", item.Agent, abandonedSaid)
}

// openPass does the thing the pass was opened for.
func (r *reviewScreen) openPass(h host, s panelShared, intent passIntent) tea.Cmd {
	switch intent {
	case intentEdit:
		return r.editFile(h, s)
	case intentComment:
	}

	return r.compose(h, s)
}

// endPass goes back to watching and lets the agent go, telling it what the reader did while it
// was stopped.
func (r *reviewScreen) endPass(h host, s panelShared, said string) tea.Cmd {
	agentID := r.agentID
	ours, touched := r.pass.end()

	h.noteEdited(agentID, touched)

	if !ours {
		return nil
	}

	item, ok := r.agentRow(s)
	if !ok {
		return nil
	}

	agent, name := item.Agent, r.agentName

	return h.releaseCmd("Could not let "+name+" go", agent, said)
}

// touchedSaid names the files the reader opened an editor on.
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
