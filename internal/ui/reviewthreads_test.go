package ui

import (
	"testing"

	"weave/internal/assemble"
	"weave/internal/client"
	"weave/lib/xptr"
	agentv1 "weave/proto/gen/agent/v1"
	watchv1 "weave/proto/gen/watch/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReviewFollowsAnAnswerFromTheWatch covers the whole path a reply takes onto the screen
// without a keypress: a batch puts the thread into the world and the screen draws it.
func TestReviewFollowsAnAnswerFromTheWatch(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m.reactors = newWorldReactors()
	holdReviewedAgent(&m, dir)
	m = openReview(t, m)

	thread := reviewReply(t, m)[0]
	m, _ = update(t, m, client.WatchBatch{putEvent(&watchv1.Row{Row: &watchv1.Row_ReviewThread{ReviewThread: thread}})})

	assert.Contains(t, ansi.Strip(m.review.view(m.shared())), "fixed it")
}

// TestReviewFollowsNothingWhenNothingWasSaid pins the cheap probe: repinning walks every row of
// the patch, so a batch that leaves the conversation where it was must not do it again.
func TestReviewFollowsNothingWhenNothingWasSaid(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	drawn := reviewReply(t, m)
	m.review.applyThreads(m.shared(), drawn)

	streamThreads(t, &m, reviewReply(t, m))

	assert.Same(t, drawn[0], m.review.threads.list[0], "the same conversation is not pinned again")
}

// TestReviewWorksOutDeliveryFromTheReview pins that a comment's delivery is read off its review
// in the world, so the agent claiming the review's notice moves it with the thread standing still.
func TestReviewWorksOutDeliveryFromTheReview(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	threads := reviewComment(t, m)
	threads[0].Messages[0].ReviewId = xptr.Ptr("r1")
	m.listing.world.Put(reviewRowOf("r1", "2026-09-02T10:00:00Z", ""))
	streamThreads(t, &m, threads)

	require.Contains(t, ansi.Strip(m.review.view(m.shared())), "waiting")

	m.listing.world.Put(reviewRowOf("r1", "2026-09-02T10:00:00Z", "2026-09-02T10:05:00Z"))
	m.review.followWorld(m.shared())

	assert.Contains(t, ansi.Strip(m.review.view(m.shared())), "delivered")
}

// TestReviewDrawsWhatArrivedOnceInputCloses pins that a thread arriving while the reader types is
// drawn after they stop.
func TestReviewDrawsWhatArrivedOnceInputCloses(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	m.review.threads.composing = true
	streamThreads(t, &m, reviewReply(t, m))
	require.NotContains(t, ansi.Strip(m.review.view(m.shared())), "fixed it")

	m.review.threads.composing = false
	m.review.update(&m, m.shared(), struct{}{})

	assert.Contains(t, ansi.Strip(m.review.view(m.shared())), "fixed it")
}

// TestReviewFollowLeavesTheComposerAlone covers a thread arriving while the reader has started
// writing, which must not take the editor, the cursor or the window.
func TestReviewFollowLeavesTheComposerAlone(t *testing.T) {
	dir := reviewRepo(t)
	m, tmux := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	threads := reviewReply(t, m)

	m.review.compose(&m, m.shared())
	require.True(t, m.review.threads.composing)

	m = typeReviewMessage(t, m, "half a thought")
	cursor, top := m.review.diff.cursor, m.review.diff.top
	tmux.sentKeys = nil

	streamThreads(t, &m, threads)

	assert.True(t, m.review.threads.composing, "the editor is still up")
	assert.Equal(t, "half a thought", m.review.threads.editor.value(), "with what was typed into it")
	assert.Equal(t, cursor, m.review.diff.cursor)
	assert.Equal(t, top, m.review.diff.top)
	assert.Empty(t, tmux.sentKeys, "and the diff was not repainted under it")

	m.review.threads.composing = false
	m.review.threads.editor = reviewInput{}
	m.review.followWorld(m.shared())
	assert.Contains(t, ansi.Strip(m.review.view(m.shared())), "fixed it")
}

// TestReviewStopsFollowingAnArchivedAgent pins that the threads an archived agent's row takes out
// of the world stay on screen.
func TestReviewStopsFollowingAnArchivedAgent(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)
	streamThreads(t, &m, reviewReply(t, m))

	m.listing.archivedAgents = m.listing.agents
	m.listing.agents = nil
	delete(m.listing.world.ReviewThreads, "t1")
	m.review.followWorld(m.shared())

	assert.Contains(t, ansi.Strip(m.review.view(m.shared())), "fixed it")
}

// TestReviewReadsThreadsFromTheWorld pins where a load takes the conversation from: the world for
// a live agent, and the server for an archived one.
func TestReviewReadsThreadsFromTheWorld(t *testing.T) {
	tests := []struct {
		name  string
		live  bool
		wants string
	}{
		{
			name:  "a live agent's threads are the world's",
			live:  true,
			wants: "held",
		},
		{
			name:  "an archived agent's threads are read from the server",
			live:  false,
			wants: "served",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agents := &fakeReviewAgents{threads: []*agentv1.ReviewThread{{Id: "served"}}}

			threads, err := readThreads(t.Context(), readThreadsParams{
				Agents:  agents,
				AgentID: "a1",
				Live:    tt.live,
				Held:    []*agentv1.ReviewThread{{Id: "held"}},
			})
			require.NoError(t, err)
			require.Len(t, threads, 1)

			assert.Equal(t, tt.wants, threads[0].GetId())
		})
	}
}

// TestReviewOwedIsReadFromTheWorld pins that the review still owed an answer is the person's last
// released one while a thread it opened is unsettled.
func TestReviewOwedIsReadFromTheWorld(t *testing.T) {
	tests := []struct {
		name     string
		resolved string
		wants    string
	}{
		{
			name:  "an unsettled thread is owed an answer",
			wants: "r2",
		},
		{
			name:     "a settled one is not",
			resolved: "2026-09-02T11:00:00Z",
			wants:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var w assemble.World
			w.Put(reviewRowOf("r1", "2026-09-02T09:00:00Z", ""))
			w.Put(reviewRowOf("r2", "2026-09-02T10:00:00Z", ""))
			w.Put(&watchv1.Row{Row: &watchv1.Row_ReviewThread{ReviewThread: &agentv1.ReviewThread{
				Id:         "t1",
				AgentId:    "a1",
				ReviewId:   "r2",
				ResolvedAt: tt.resolved,
			}}})

			assert.Equal(t, tt.wants, findOwedReview(w, "a1"))
		})
	}
}

// TestReviewWorldReactorFollowsWhatTheReviewDraws pins which batches the review reads the world
// again for.
func TestReviewWorldReactorFollowsWhatTheReviewDraws(t *testing.T) {
	tests := []struct {
		name    string
		changed assemble.Changed
		moves   bool
	}{
		{
			name:    "a snapshot",
			changed: assemble.Changed{Replaced: true},
			moves:   true,
		},
		{
			name:    "a thread",
			changed: changedKeys(watchv1.Resource_RESOURCE_REVIEW_THREAD, "t1"),
			moves:   true,
		},
		{
			name:    "a review",
			changed: changedKeys(watchv1.Resource_RESOURCE_REVIEW, "r1"),
			moves:   true,
		},
		{
			name:    "the agent under review",
			changed: changedKeys(watchv1.Resource_RESOURCE_AGENT, "a1"),
			moves:   true,
		},
		{
			name:    "another agent",
			changed: changedKeys(watchv1.Resource_RESOURCE_AGENT, "a2"),
			moves:   false,
		},
		{
			name:    "mail",
			changed: changedKeys(watchv1.Resource_RESOURCE_MAIL, "n1"),
			moves:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.moves, doesMoveReview(tt.changed, "a1"))
		})
	}
}

// TestReviewWorldReactorFollowsTheMove pins that the agent's row moving past the round on screen
// reads the diff again.
func TestReviewWorldReactorFollowsTheMove(t *testing.T) {
	dir := reviewRepo(t)
	m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
	m = openReview(t, m)

	markReviewWorktreeMoved(&m)

	assert.Nil(t, reviewWorldReactor{}.react(&m, changedKeys(watchv1.Resource_RESOURCE_AGENT, "a2")))
	assert.NotNil(t, reviewWorldReactor{}.react(&m, changedKeys(watchv1.Resource_RESOURCE_AGENT, "a1")))
}

// reviewReply is a thread carrying one message, pinned to wherever the cursor is standing, which
// is what a batch bringing an answer has to draw.
func reviewReply(t *testing.T, m model) []*agentv1.ReviewThread {
	t.Helper()

	anchor, ok := m.review.diff.view.GetAnchor(m.review.diff.cursor)
	require.True(t, ok)

	return []*agentv1.ReviewThread{{
		Id:          "t1",
		AgentId:     "a1",
		FilePath:    anchor.FilePath,
		OldStart:    int32(anchor.OldStart),
		NewStart:    int32(anchor.NewStart),
		QuotedLines: anchor.QuotedLines,
		UpdatedAt:   "2026-09-02T10:00:00Z",
		Messages: []*agentv1.ReviewMessage{{
			Id:         "m1",
			AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT,
			Body:       "fixed it",
			UpdatedAt:  "2026-09-02T10:00:00Z",
		}},
	}}
}

// reviewComment is a thread carrying one comment of the person's own, with every stamp held still.
func reviewComment(t *testing.T, m model) []*agentv1.ReviewThread {
	t.Helper()

	threads := reviewReply(t, m)
	threads[0].Messages = []*agentv1.ReviewMessage{{
		Id:         "m1",
		AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
		Body:       "wrong greeting",
		UpdatedAt:  "2026-09-02T10:00:00Z",
	}}

	return threads
}

// reviewRowOf is a review of a1's work the person released, delivered when deliveredAt is set.
func reviewRowOf(id, releasedAt, deliveredAt string) *watchv1.Row {
	return &watchv1.Row{Row: &watchv1.Row_Review{Review: &agentv1.WatchedReview{
		Id:          id,
		AgentId:     "a1",
		AuthorKind:  agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
		ReleasedAt:  releasedAt,
		DeliveredAt: deliveredAt,
	}}}
}

// holdReviewedAgent puts the agent under review and its workflow into the world, so a batch
// assembling the listing again keeps it.
func holdReviewedAgent(m *model, dir string) {
	m.listing.world.Put(workflowRowOf(&workflowv1.Workflow{Id: "w1", Name: "backend"}))
	m.listing.world.Put(agentRowOf(&agentv1.Agent{
		Id:         "a1",
		Name:       "auth",
		Cwd:        dir,
		WorkflowId: "w1",
	}))
}

func changedKeys(resource watchv1.Resource, key string) assemble.Changed {
	return assemble.Changed{Keys: map[watchv1.Resource][]string{resource: {key}}}
}
