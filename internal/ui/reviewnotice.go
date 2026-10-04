package ui

import "slices"

// reviewNotices is what the review has raised on the messages stack.
type reviewNotices struct {
	// raised is every key raised this visit and not since lowered, so a dismissed notice stays down.
	raised []string
	// moved is the key of the notice about the agent having written past the diff on screen.
	moved string
}

// raise pushes a notice once per visit.
func (n *reviewNotices) raise(m *model, held message) {
	if slices.Contains(n.raised, held.key) {
		return
	}

	n.raised = append(n.raised, held.key)
	m.pushMessage(held)
}

// lower takes a notice down once it stops being true, leaving it free to be raised again.
func (n *reviewNotices) lower(m *model, key string) {
	m.dropMessage(key)
	n.raised = slices.DeleteFunc(n.raised, func(raised string) bool { return raised == key })
}

// followNotices raises what the review has to say about the agent and lowers what is no longer true.
func (r *reviewScreen) followNotices(m *model) {
	if r.diff.View() == nil || r.diff.View().Len() == 0 || m.screen != screenReview {
		return
	}

	archived := "archived:" + r.agentID
	if r.isArchived(m) {
		r.notices.raise(m, message{
			kind:    messageWarning,
			key:     archived,
			summary: "this agent is archived",
			detail:  "restore it before writing to it",
		})
	} else {
		r.notices.lower(m, archived)
	}

	moved := r.movedNotice(m)
	if moved.key != r.notices.moved {
		r.notices.lower(m, r.notices.moved)
		r.notices.moved = moved.key
	}

	if moved.key != "" {
		r.notices.raise(m, moved)
	}
}

// movedNotice is what the reader is told about the agent having written past the diff on screen.
func (r reviewScreen) movedNotice(m *model) message {
	moved := r.findMovedRound(m)
	if moved == "" || r.diff.pinned || r.hold.atEditor || r.doesFollowLive(m) {
		return message{}
	}

	key := "moved:" + r.agentID + ":" + moved
	if r.isAgentWriting(m) {
		return message{
			kind:    messageInfo,
			key:     key + ":writing",
			summary: "the agent is writing",
			detail:  "the diff catches up when it stops",
		}
	}

	return message{
		kind:    messageWarning,
		key:     key + ":stopped",
		summary: "the worktree has moved",
		detail:  keyLabel(m.keys.Menu.Open) + " " + keyLabel(m.keys.Review.Reload) + " to catch up",
	}
}
