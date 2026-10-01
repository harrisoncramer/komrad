package ui

import (
	"context"
	"fmt"
	"slices"

	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
	"connectrpc.com/connect"
)

// abandonedSaid is what an agent is told when it was stopped for an editor that never opened.
const abandonedSaid = "I paused you to open your worktree in my editor and have let you go now " +
	"without opening it."

// editedSaid is what an agent is told when the reader's editor on its worktree closes.
const editedSaid = "I paused you while I had your worktree open in my editor and have let you go now."

// reviewHold is the hold the reader takes on the agent while their editor is open on its
// worktree: whether it is stopped, what stopped it, and the files the editor was opened on.
type reviewHold struct {
	// paused says the agent is stopped for the editor, whoever stopped it.
	paused bool
	// pausing is set while the panel is asking for the agent to stop.
	pausing bool
	// ourPause says this hold is what stopped the agent, so ending the hold is what lets it go.
	ourPause bool
	// touched are the files the reader opened an editor on during the hold, oldest first, which
	// is what the agent is told about when it is let go.
	touched []string

	// atEditor says your own editor holds the stage, so the panel draws its sidebar alone.
	atEditor    bool
	editingPath string
	// editorPane is the pane the editor was given, empty where the split was refused, which is
	// what says whether a pane going away is the editor being closed.
	editorPane string
}

// isHeld reports whether the hold has the agent, or is asking for it.
func (p reviewHold) isHeld() bool {
	return p.paused || p.pausing || p.atEditor
}

// end puts the hold down, and says whether it was this hold that stopped the agent and which
// files the reader opened while it was.
func (p *reviewHold) end() (bool, []string) {
	ours, touched := p.ourPause, p.touched

	*p = reviewHold{}

	return ours, touched
}

// openEditor records the editor holding the stage on one file, in the pane the split gave it.
func (p *reviewHold) openEditor(path, pane string) {
	p.atEditor = true
	p.editingPath = path
	p.editorPane = pane
	if !slices.Contains(p.touched, path) {
		p.touched = append(p.touched, path)
	}
}

// reviewPausedMsg is the agent having stopped, or having refused to.
type reviewPausedMsg struct {
	agentID string
	err     error
	write   int
}

// startHold stops the agent and opens the editor once it has.
func (r *reviewScreen) startHold(h host, s panelShared) tea.Cmd {
	if _, dead := r.isReadOnly(s); dead {
		return r.editFile(h, s)
	}

	if item, ok := r.agentRow(s); ok && item.Agent.HeldAt != nil {
		h.reportBrief("could not edit "+r.agentName, stillPaused)

		return nil
	}

	r.hold.pausing = true

	return holdAgentCmd(h, s, r.agentID, r.agentName)
}

// holdAgentCmd asks for the agent to stop and reports what came back.
func holdAgentCmd(h host, s panelShared, agentID, name string) tea.Cmd {
	agents, write := s.api.agents, h.beginWrite("pausing "+name)

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
		defer cancel()

		_, err := agents.HoldAgent(ctx, connect.NewRequest(&agentv1.HoldAgentRequest{Id: agentID}))

		return reviewPausedMsg{
			agentID: agentID,
			err:     err,
			write:   write,
		}
	}
}

// applyPaused opens the editor, or gives it up when the agent would not stop.
func (r *reviewScreen) applyPaused(h host, s panelShared, msg reviewPausedMsg) tea.Cmd {
	if msg.agentID != r.agentID {
		return nil
	}

	r.hold.pausing = false

	if msg.err != nil {
		h.report("Could not pause "+r.agentName, msg.err)

		return nil
	}

	r.hold.paused = true
	r.hold.ourPause = true

	return r.editFile(h, s)
}

func (m model) isPassing(agentID string) bool {
	switch m.screen {
	case screenReview:
		return m.review.agentID == agentID && m.review.hold.pausing
	case screenAgents:
		return m.agents.isPausing(agentID)
	case screenSchedules, screenArchive, screenSearch, screenWorktrees, screenDocuments, screenLogs, screenDocumentTypes:
	}

	return false
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

// endHold lets the agent go, telling it which files the reader opened while it was stopped.
func (r *reviewScreen) endHold(h host, s panelShared, said string) tea.Cmd {
	agentID := r.agentID
	ours, touched := r.hold.end()

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
