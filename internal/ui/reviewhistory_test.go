package ui

import (
	"os"
	"path/filepath"
	"testing"

	agentv1 "weave/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWritingIsGivenUpWhenTheRoundMovesOn covers the mode outliving the round it was opened on:
// every write on a finished round refuses and names the key back to the whole change, and that
// key is one the writing mode withdraws, so the reader was left with nothing that worked.
func TestWritingIsGivenUpWhenTheRoundMovesOn(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}

	m := reviewWithAnEarlierReview(t, dir, agents)
	m = pressReviewBase(t, m)
	m = reviewCursorOnAnAnchor(t, m)

	_, history := m.reviewIsHistory()
	require.False(t, history, "the round the reader narrowed to is the work in hand")

	next, _ := m.updateReview(keyMsg("c"))
	m = next.(model)
	require.True(t, m.review.writing, "so the compose key opens the mode that writes")

	next, _ = m.updateReview(keyMsg("esc"))
	m = next.(model)
	require.False(t, m.review.composing)

	// The reader lets their review go, which closes the round they are standing on, and the agent
	// answers by writing again.
	second := agents.rounds[1]
	agents.reviews = append(agents.reviews, &agentv1.Review{
		Id:           "v2",
		RoundId:      second.GetId(),
		RoundOrdinal: second.GetOrdinal(),
		AuthorKind:   agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
		ReleasedAt:   "2026-09-08T11:00:00Z",
	})
	require.NoError(t, os.WriteFile(filepath.Join(dir, "third.go"),
		[]byte("package greet\n\nfunc Third() {}\n"), 0o644))

	m = reloadedReview(t, m)

	said, history := m.reviewIsHistory()
	require.True(t, history, "the diff on screen was drawn to a snapshot the agent has left")
	require.Equal(t, "round 2 of 3", said)

	assert.False(t, m.review.writing, "the mode goes with the round it was opened on")
	assert.Contains(t, m.review.notice, "round 2 of 3 is finished")
	assert.True(t, m.doesReviewWalk(), "so the key the refusal names is answered again")

	m = reviewCursorOnAnAnchor(t, m)
	next, _ = m.updateReview(keyMsg("c"))
	m = next.(model)

	assert.False(t, m.review.composing, "and the compose key refuses rather than opening an editor")
	assert.Nil(t, agents.created, "so no comment names the snapshot the agent has left behind")
}

// reviewCursorOnAnAnchor puts the cursor on a line a comment could be anchored to, since what the
// write keys do is about the line under it.
func reviewCursorOnAnAnchor(t *testing.T, m model) model {
	t.Helper()

	for row := 0; row < m.review.view.Len(); row++ {
		if found, ok := m.review.view.GetAnchor(row); ok && found.NewStart > 0 {
			m.review.cursor = row

			return m
		}
	}

	t.Fatal("the diff drew no line to anchor to")

	return m
}

// reloadedReview is the reload key, which is what a reader presses once the panel says the agent
// has written since the round on screen was taken.
func reloadedReview(t *testing.T, m model) model {
	t.Helper()

	next, cmd := m.reloadReview()
	require.NotNil(t, cmd, "the reload key reads the round again")

	loaded, ok := cmd().(reviewLoadedMsg)
	require.True(t, ok)
	next.applyReviewLoaded(loaded)

	return next
}
