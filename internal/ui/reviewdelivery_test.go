package ui

import (
	"testing"

	"weave/internal/ui/panel"
	agentv1 "weave/proto/gen/agent/v1"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pinnedThread is a thread standing on the line the cursor is on, carrying whatever was said in it.
func pinnedThread(t *testing.T, m model, messages ...*agentv1.ReviewMessage) []*agentv1.ReviewThread {
	t.Helper()

	anchor, ok := m.review.diff.view.GetAnchor(m.review.diff.cursor)
	require.True(t, ok)

	return []*agentv1.ReviewThread{{
		Id:          "t1",
		FilePath:    anchor.FilePath,
		OldStart:    int32(anchor.OldStart),
		NewStart:    int32(anchor.NewStart),
		QuotedLines: anchor.QuotedLines,
		Messages:    messages,
	}}
}

// TestReviewDrawsDeliveryUnderEachComment covers the whole point of per-comment delivery: the
// sidebar answers what this one comment is doing rather than how the review is doing.
func TestReviewDrawsDeliveryUnderEachComment(t *testing.T) {
	tests := []struct {
		name    string
		message *agentv1.ReviewMessage
		want    string
		absent  string
	}{
		{
			name:    "written and not let go of",
			message: unsentComment(),
			want:    "◇ unsent",
			absent:  "delivered",
		},
		{
			name:    "released with the notice still waiting",
			message: sentComment(),
			want:    "→ waiting",
			absent:  "unsent",
		},
		{
			name:    "the agent picked the notice up",
			message: deliveredComment(),
			want:    "✓ delivered",
			absent:  "waiting",
		},
		{
			name: "nothing is said of what the agent wrote",
			message: &agentv1.ReviewMessage{
				Id:         "m9",
				AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT,
				Body:       "renamed it",
			},
			want:   "renamed it",
			absent: "unsent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := reviewRepo(t)
			m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
			m = openReview(t, m)

			m.review.applyThreads(m.shared(), pinnedThread(t, m, tt.message))

			shown := ansi.Strip(m.review.view(m.shared()))
			assert.Contains(t, shown, tt.want)
			assert.NotContains(t, shown, tt.absent)
		})
	}
}

// TestReviewDeliveryBadgeNamesTheMoment pins the time on the badge being the last thing that
// happened to the comment.
func TestReviewDeliveryBadgeNamesTheMoment(t *testing.T) {
	sent := sentComment()
	delivered := deliveredComment()

	assert.Equal(t, sent.GetSentAt(), reviewDeliveryStamp(sent))
	assert.Equal(t, delivered.GetDeliveredAt(), reviewDeliveryStamp(delivered),
		"once it has landed, when it went is no longer the news")
	assert.Empty(t, reviewDeliveryStamp(unsentComment()), "nothing has happened to it yet")
}

// TestReviewGutterMarksWhatHasNotReachedTheAgent covers the marks beside the diff, which is
// where a reader scanning the code rather than the sidebar finds out a comment never landed.
func TestReviewGutterMarksWhatHasNotReachedTheAgent(t *testing.T) {
	tests := []struct {
		name    string
		thread  *agentv1.ReviewThread
		want    string
		unwant  string
		comment string
	}{
		{
			name:    "a comment still only written down is marked hollow",
			thread:  &agentv1.ReviewThread{Messages: []*agentv1.ReviewMessage{unsentComment()}},
			want:    "◇",
			unwant:  "●",
			comment: "the submit key is what fixes this one",
		},
		{
			name: "a draft outranks the answer waiting in it",
			thread: &agentv1.ReviewThread{
				UnreadByHuman: true,
				Messages:      []*agentv1.ReviewMessage{unsentComment()},
			},
			want:    "◇",
			unwant:  "●",
			comment: "what a reader does about this one is send it",
		},
		{
			name: "an answer nobody has read is filled in",
			thread: &agentv1.ReviewThread{
				UnreadByHuman: true,
				Messages:      []*agentv1.ReviewMessage{deliveredComment()},
			},
			want:   "●",
			unwant: "○",
		},
		{
			name: "and a thread finished with says so",
			thread: &agentv1.ReviewThread{
				ResolvedAt: "2026-09-02T11:00:00Z",
				Messages:   []*agentv1.ReviewMessage{deliveredComment()},
			},
			want:   "✓",
			unwant: "●",
		},
		{
			name: "until the agent says something more in it",
			thread: &agentv1.ReviewThread{
				ResolvedAt:    "2026-09-02T11:00:00Z",
				UnreadByHuman: true,
				Messages:      []*agentv1.ReviewMessage{deliveredComment()},
			},
			want:    "●",
			unwant:  "✓",
			comment: "nowhere else in the panel says a settled thread has moved",
		},
		{
			name:    "a comment that landed and was read is hollow",
			thread:  &agentv1.ReviewThread{Messages: []*agentv1.ReviewMessage{deliveredComment()}},
			want:    "○",
			unwant:  "●",
			comment: "nothing has happened in it since the reader last looked",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := reviewRepo(t)
			m, _ := reviewModel(t, &fakeReviewAgents{}, dir)
			m = openReview(t, m)

			threads := pinnedThread(t, m, tt.thread.GetMessages()...)
			threads[0].UnreadByHuman = tt.thread.GetUnreadByHuman()
			threads[0].ResolvedAt = tt.thread.GetResolvedAt()
			m.review.applyThreads(m.shared(), threads)

			row := reviewGutterFor(t, m, "t1")
			assert.Contains(t, row, tt.want, tt.comment)
			if tt.unwant != "" {
				assert.NotContains(t, row, tt.unwant)
			}
		})
	}
}

// TestReviewVerdictIsChosenInThePanel covers the verdict being a deliberate choice made in one
// place: the submit key asks rather than sends, and what it sends is whatever the selector was
// left on, so there is no second key that means something different.
func TestReviewVerdictIsChosenInThePanel(t *testing.T) {
	tests := []struct {
		name string
		tabs int
		want agentv1.ReviewVerdict
		says string
	}{
		{
			name: "the panel opens on asking for changes",
			want: agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED,
			says: "changes requested",
		},
		{
			name: "one tab approves",
			tabs: 1,
			want: agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED,
			says: "approved",
		},
		{
			name: "two tabs comment without asking for anything",
			tabs: 2,
			want: agentv1.ReviewVerdict_REVIEW_VERDICT_COMMENTED,
			says: "comments only",
		},
		{
			name: "and it wraps back around",
			tabs: 3,
			want: agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED,
			says: "changes requested",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := reviewRepo(t)
			agents := &fakeReviewAgents{}
			m, _ := reviewModel(t, agents, dir)
			m = openReview(t, m)

			next, _ := m.routeScreenMessage(tea.KeyMsg{Type: tea.KeyCtrlS})
			m = next.(model)
			require.True(t, m.review.threads.releasing, "the submit key asks rather than sends")
			require.Nil(t, agents.released, "and nothing has gone yet")

			for range tt.tabs {
				next, _ = m.routeScreenMessage(tea.KeyMsg{Type: tea.KeyTab})
				m = next.(model)
			}

			sent, cmd := m.routeScreenMessage(tea.KeyMsg{Type: tea.KeyCtrlS})
			m = sent.(model)
			require.NotNil(t, cmd)
			require.False(t, m.review.threads.releasing, "and the panel is put away")

			wrote, ok := cmd().(reviewWroteMsg)
			require.True(t, ok)
			require.NoError(t, wrote.err)

			require.NotNil(t, agents.released)
			assert.Equal(t, tt.want, agents.released.GetVerdict())
			assert.Contains(t, wrote.notice, tt.says)

			m.review.applyWrote(&m, m.shared(), wrote)
			assert.Contains(t, ansi.Strip(m.review.view(m.shared())), tt.says)
		})
	}
}

// TestAReviewCarriesAWordAboutTheWholeThing covers the panel's other half: a comment that belongs
// to no line goes with the review.
func TestAReviewCarriesAWordAboutTheWholeThing(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	next, _ := m.routeScreenMessage(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = next.(model)

	for _, r := range "the shape is right" {
		next, _ = m.routeScreenMessage(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next.(model)
	}

	_, cmd := m.routeScreenMessage(tea.KeyMsg{Type: tea.KeyCtrlS})
	require.NotNil(t, cmd)
	cmd()

	require.NotNil(t, agents.released)
	assert.Equal(t, "the shape is right", agents.released.GetBody())
}

// TestAbandoningTheReleasePanelSendsNothing covers escape being a way out.
func TestAbandoningTheReleasePanelSendsNothing(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	next, _ := m.routeScreenMessage(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = next.(model)
	require.True(t, m.review.threads.releasing)

	next, _ = m.routeScreenMessage(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(model)

	assert.False(t, m.review.threads.releasing, "the panel is put away")
	assert.Nil(t, agents.released, "and nothing was sent")
}

// TestReviewOfADeadAgentIsReadOnly covers the narrower gate inside the screen: the diff opens
// for an agent that is not there, and every key that would put something in front of it is
// refused rather than left to fail at the server.
func TestReviewOfADeadAgentIsReadOnly(t *testing.T) {
	tests := []struct {
		name  string
		alive bool
		want  string
	}{
		{
			name:  "dormant",
			alive: false,
			want:  "this agent is dormant",
		},
		{
			name:  "archived",
			alive: false,
			want:  "this agent is archived",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := reviewRepo(t)
			agents := &fakeReviewAgents{}
			m, _ := reviewModel(t, agents, dir)
			m = openReview(t, m)

			// An archived agent is on no listing at all, where a dormant one is listed and not alive.
			m.listing.agents = nil
			if tt.want == "this agent is dormant" {
				m.listing.agents = []agentItem{{agent: panel.Agent{ID: "a1"}, alive: tt.alive}}
			}

			shown := ansi.Strip(m.review.view(m.shared()))
			assert.Contains(t, shown, tt.want)
			assert.NotContains(t, shown, "reply in this thread", "and the key is off the strip")

			m.helpExpanded, m.height = true, 40
			expanded := ansi.Strip(m.review.view(m.shared()))
			assert.NotContains(t, expanded, "send the review")
			assert.Contains(t, expanded, "settle the thread", "what is the person's own record stays")

			m.helpExpanded = false

			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyRunes, Runes: []rune("c")},
				{Type: tea.KeyCtrlS},
			} {
				next, cmd := m.routeScreenMessage(key)
				refused := next.(model)

				assert.Nil(t, cmd, "%v sends nothing", key)
				assert.False(t, refused.review.threads.composing, "%v does not open the editor", key)
				assert.False(t, refused.review.threads.releasing, "%v does not open the release panel", key)
				assert.Contains(t, refused.review.notice, tt.want, "%v says why", key)
			}

			assert.Nil(t, agents.released)
			assert.Nil(t, agents.created)
		})
	}
}

// TestReviewOfADeadAgentKeepsTheReadersOwnRecords pins settling, deleting and folding a file
// away still working.
func TestReviewOfADeadAgentKeepsTheReadersOwnRecords(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	m.review.applyThreads(m.shared(), pinnedThread(t, m, unsentComment()))
	m.listing.agents = nil

	next, cmd := m.routeScreenMessage(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = next.(model)
	require.NotNil(t, cmd, "settling still works")
	_, ok := cmd().(reviewWroteMsg)
	require.True(t, ok)
	require.NotNil(t, agents.settled)

	next, cmd = m.routeScreenMessage(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	m = next.(model)
	require.NotNil(t, cmd, "and so does folding a file away")
	_, ok = cmd().(reviewWroteMsg)
	require.True(t, ok)
	require.NotNil(t, agents.marked)
}
