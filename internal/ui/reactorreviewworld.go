package ui

import (
	"slices"

	"komrade/internal/assemble"
	watchv1 "komrade/proto/gen/watch/v1"

	tea "charm.land/bubbletea/v2"
)

// reviewWorldReactor keeps the review on screen up with its threads, its reviews and its agent.
type reviewWorldReactor struct{}

func (reviewWorldReactor) react(m *model, changed assemble.Changed) tea.Cmd {
	if m.screen != screenReview || !doesMoveReview(changed, m.review.agentID) {
		return nil
	}

	return m.review.followWorld(m.shared())
}

// doesMoveReview reports whether a batch touched anything the review of an agent draws.
func doesMoveReview(changed assemble.Changed, agentID string) bool {
	return changed.Replaced ||
		len(changed.Keys[watchv1.Resource_RESOURCE_REVIEW_THREAD]) > 0 ||
		len(changed.Keys[watchv1.Resource_RESOURCE_REVIEW]) > 0 ||
		slices.Contains(changed.Keys[watchv1.Resource_RESOURCE_AGENT], agentID)
}
