package ui

import "slices"

func (r *reviewScreen) raise(m *model, held message) {
	if slices.Contains(r.raised, held.key) {
		return
	}

	r.raised = append(r.raised, held.key)
	m.pushMessage(held)
}

func (r *reviewScreen) lower(m *model, key string) {
	m.dropMessage(key)
	r.raised = slices.DeleteFunc(r.raised, func(raised string) bool { return raised == key })
}

func (r *reviewScreen) raiseNotices(m *model) {
	if r.diff.View() == nil || r.diff.View().Len() == 0 || m.screen != screenReview {
		return
	}

	archived := "archived:" + r.agentID
	if !r.isArchived(m) {
		r.lower(m, archived)
	} else {
		r.raise(m, message{
			kind:    messageWarning,
			key:     archived,
			summary: "this agent is archived",
			detail:  "restore it before writing to it",
		})
	}

	moved := r.movedNotice(m)
	if moved.key != r.moved {
		r.lower(m, r.moved)
		r.moved = moved.key
	}

	if moved.key != "" {
		r.raise(m, moved)
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
		detail:  keyLabel(m.keys.Review.Reload) + " to catch up",
	}
}
