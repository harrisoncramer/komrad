package ui

import (
	"context"
	"slices"

	"komrad/internal/ui/api"
	"komrad/internal/ui/screen"
	agentv1 "komrad/proto/gen/agent/v1"

	"connectrpc.com/connect"
)

// abandonedSaid is what the agent hears when Komrad stopped it for an editor that never opened.
const abandonedSaid = "I paused you to open your worktree in my editor and have let you go now " +
	"without opening it."

// editedSaid is what the agent hears when the reader's editor on its worktree closes.
const editedSaid = "I paused you while I had your worktree open in my editor and have let you go now."

// reviewHold is the hold the reader takes on the agent while their editor is open on its
// worktree, recording whether the agent is stopped, what stopped it, and the files the editor
// opened.
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

	// atEditor says your own editor holds the stage, so the panel renders its sidebar alone.
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
}

// startHold stops the agent and opens the editor once it has.
func (r *reviewScreen) startHold(m *model) screen.Request {
	item, ok := r.agentRow(m)
	if !ok || !item.Alive {
		return r.editFile(m)
	}

	if item.Agent.HeldAt != nil {
		return screen.Note{
			Kind:    screen.Error,
			Summary: "could not edit " + r.agentName,
			Detail:  stillPaused,
		}
	}

	r.hold.pausing = true

	return screen.Batch{
		screen.Note{
			Kind:    screen.Info,
			Summary: "stopping the agent",
			Detail:  "the editor opens once it stops",
		},
		holdAgent(r.deps.clients, r.agentID, r.agentName),
	}
}

// holdAgent asks for the agent to stop and reports what came back.
func holdAgent(clients api.Clients, agentID, name string) screen.Request {
	agents := clients.Agents

	return screen.Write{
		Doing: "pausing " + name,
		Send: func() screen.Landed {
			ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
			defer cancel()

			_, err := agents.HoldAgent(ctx, connect.NewRequest(&agentv1.HoldAgentRequest{Id: agentID}))

			return screen.Landed{Then: reviewPausedMsg{agentID: agentID, err: err}}
		},
	}
}

// applyPaused opens the editor, or gives it up when the agent would not stop.
func (r *reviewScreen) applyPaused(m *model, msg reviewPausedMsg) screen.Request {
	if msg.agentID != r.agentID {
		return nil
	}

	r.hold.pausing = false

	if msg.err != nil {
		return screen.Report{Summary: "Could not pause " + r.agentName, Err: msg.err}
	}

	r.hold.paused = true
	r.hold.ourPause = true

	return r.editFile(m)
}

func (m *model) isPausing(agentID string) bool {
	screen, ok := m.active().(agentPauser)

	return ok && screen.isPausing(agentID)
}

func (r *reviewScreen) isPausing(agentID string) bool {
	return r.agentID == agentID && r.hold.pausing
}

func (m *model) releaseAbandonedPause(msg reviewPausedMsg) screen.Request {
	if msg.err != nil {
		return nil
	}

	item, ok := m.listing.LiveAgent(msg.agentID)
	if !ok {
		return nil
	}

	return m.api.ReleaseWrite("Could not let "+item.Agent.Name+" go", item.Agent, abandonedSaid, m.edited[item.Agent.ID])
}

// endHold lets the agent go and tells it which files the reader opened while it was stopped.
func (r *reviewScreen) endHold(m *model, said string) screen.Request {
	agentID := r.agentID
	ours, touched := r.hold.end()

	m.noteEdited(agentID, touched)

	if !ours {
		return nil
	}

	item, ok := r.agentRow(m)
	if !ok {
		return nil
	}

	agent, name := item.Agent, r.agentName

	return r.deps.clients.ReleaseWrite("Could not let "+name+" go", agent, said, m.edited[agent.ID])
}
