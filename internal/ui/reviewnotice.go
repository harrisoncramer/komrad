package ui

import "slices"

func (r *reviewScreen) raise(m *model, key, summary, detail string) {
	if slices.Contains(r.raised, key) {
		return
	}

	r.raised = append(r.raised, key)
	m.pushMessage(message{
		kind:    messageWarning,
		key:     key,
		summary: summary,
		detail:  detail,
	})
}

func (r *reviewScreen) raiseNotices(m *model) {
	if r.diff.View() == nil || r.diff.View().Len() == 0 || m.screen != screenReview {
		return
	}

	if r.isArchived(m) {
		r.raise(m, "archived:"+r.agentID, "this agent is archived", "restore it before writing to it")
	}

	moved := r.findMovedRound(m)
	if moved == "" || r.diff.pinned || r.hold.atEditor || r.doesFollowLive(m) {
		return
	}

	summary, detail := r.staleSaid(m)
	r.raise(m, "moved:"+r.agentID+":"+moved, summary, detail)
}

func (r reviewScreen) staleSaid(m *model) (string, string) {
	if r.isAgentWriting(m) {
		return "the agent is writing", "the diff catches up when it stops"
	}

	return "the worktree has moved", keyLabel(m.keys.Review.Reload) + " to catch up"
}
