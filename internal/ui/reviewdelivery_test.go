package ui

import (
	"testing"

	agentstore "weave/internal/store/agent"
	agentv1 "weave/proto/gen/agent/v1"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pinnedThread is a thread standing on the line the cursor is on, carrying whatever was said in
// it, which is what the sidebar draws under the cursor.
func pinnedThread(t *testing.T, m model, messages ...*agentv1.ReviewMessage) []*agentv1.ReviewThread {
	t.Helper()

	anchor, ok := m.review.view.GetAnchor(m.review.cursor)
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
			name:    "released with the doorbell still waiting",
			message: sentComment(),
			want:    "→ waiting",
			absent:  "unsent",
		},
		{
			name:    "the agent picked the doorbell up",
			message: deliveredComment(),
			want:    "✓ delivered",
			absent:  "waiting",
		},
		{
			name:    "the doorbell ran out of time",
			message: expiredComment(),
			want:    "! expired",
			absent:  "delivered",
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

			m.applyReviewThreads(pinnedThread(t, m, tt.message))

			shown := ansi.Strip(m.reviewView())
			assert.Contains(t, shown, tt.want)
			assert.NotContains(t, shown, tt.absent)
		})
	}
}

// TestReviewDeliveryBadgeNamesTheMoment pins the time on the badge being the last thing that
// happened to the comment, since when it was written is already on the line above.
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
			name:    "a comment that ran out of time is shouted",
			thread:  &agentv1.ReviewThread{Messages: []*agentv1.ReviewMessage{expiredComment()}},
			want:    "!",
			comment: "ringing again is the only thing that reaches the agent now",
		},
		{
			name:    "a comment still only written down is marked hollow",
			thread:  &agentv1.ReviewThread{Messages: []*agentv1.ReviewMessage{unsentComment()}},
			want:    "◇",
			unwant:  "!",
			comment: "the submit key is what fixes this one",
		},
		{
			name: "expired outranks unsent, since the two are fixed by different keys",
			thread: &agentv1.ReviewThread{Messages: []*agentv1.ReviewMessage{
				unsentComment(),
				expiredComment(),
			}},
			want:   "!",
			unwant: "◇",
		},
		{
			name: "an answer waiting to be read still comes first",
			thread: &agentv1.ReviewThread{
				UnreadByHuman: true,
				Messages:      []*agentv1.ReviewMessage{expiredComment()},
			},
			want:   "◆",
			unwant: "!",
		},
		{
			name: "and a thread finished with says so",
			thread: &agentv1.ReviewThread{
				ResolvedAt: "2026-09-02T11:00:00Z",
				Messages:   []*agentv1.ReviewMessage{unsentComment()},
			},
			want:   "✓",
			unwant: "◇",
		},
		{
			name:   "a comment that landed is just a thread",
			thread: &agentv1.ReviewThread{Messages: []*agentv1.ReviewMessage{deliveredComment()}},
			want:   "●",
			unwant: "◇",
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
			m.applyReviewThreads(threads)

			row := reviewGutterFor(t, m, "t1")
			assert.Contains(t, row, tt.want, tt.comment)
			if tt.unwant != "" {
				assert.NotContains(t, row, tt.unwant)
			}
		})
	}
}

// TestReviewResendRingsTheExpiredCommentsAgain covers the key that reaches an agent which was
// not listening when the review was let go of, and what it says it did.
func TestReviewResendRingsTheExpiredCommentsAgain(t *testing.T) {
	tests := []struct {
		name string
		rung int32
		want string
	}{
		{
			name: "nothing has run out of time",
			want: "nothing has run out of time",
		},
		{
			name: "one comment rung again",
			rung: 1,
			want: "rang 1 comment again",
		},
		{
			name: "several",
			rung: 3,
			want: "rang 3 comments again",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := reviewRepo(t)
			agents := &fakeReviewAgents{rung: tt.rung}
			m, _ := reviewModel(t, agents, dir)
			m = openReview(t, m)

			next, cmd := m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
			m = next.(model)
			require.NotNil(t, cmd)

			wrote, ok := cmd().(reviewWroteMsg)
			require.True(t, ok)
			require.NoError(t, wrote.err)

			require.NotNil(t, agents.rang)
			assert.Equal(t, "a1", agents.rang.GetAgentId())
			assert.Equal(t, tt.want, wrote.notice)

			m.applyReviewWrote(wrote)
			assert.Contains(t, ansi.Strip(m.reviewView()), tt.want)
		})
	}
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
		{name: "archived", want: "this agent is archived"},
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
				m.listing.agents = []agentItem{{agent: agentstore.Agent{ID: "a1"}, alive: tt.alive}}
			}

			shown := ansi.Strip(m.reviewView())
			assert.Contains(t, shown, tt.want)
			assert.NotContains(t, shown, "reply in this thread", "and the key is off the strip")

			m.helpExpanded, m.height = true, 40
			expanded := ansi.Strip(m.reviewView())
			assert.NotContains(t, expanded, "ring expired again")
			assert.NotContains(t, expanded, "send what you wrote")
			assert.Contains(t, expanded, "settle the thread", "what is the person's own record stays")

			m.helpExpanded = false

			for _, key := range []tea.KeyMsg{
				{Type: tea.KeyRunes, Runes: []rune("c")},
				{Type: tea.KeyRunes, Runes: []rune("a")},
				{Type: tea.KeyCtrlS},
			} {
				next, cmd := m.updateReview(key)
				refused := next.(model)

				assert.Nil(t, cmd, "%v sends nothing", key)
				assert.False(t, refused.review.composing, "%v does not open the editor", key)
				assert.Contains(t, refused.review.notice, tt.want, "%v says why", key)
			}

			assert.Nil(t, agents.rang)
			assert.Nil(t, agents.released)
			assert.Nil(t, agents.created)
		})
	}
}

// TestReviewOfADeadAgentKeepsTheReadersOwnRecords pins what stays working, since settling,
// deleting and folding a file away are the person's own notes and send nothing to anyone.
func TestReviewOfADeadAgentKeepsTheReadersOwnRecords(t *testing.T) {
	dir := reviewRepo(t)
	agents := &fakeReviewAgents{}
	m, _ := reviewModel(t, agents, dir)
	m = openReview(t, m)

	m.applyReviewThreads(pinnedThread(t, m, unsentComment()))
	m.listing.agents = nil

	next, cmd := m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = next.(model)
	require.NotNil(t, cmd, "settling still works")
	_, ok := cmd().(reviewWroteMsg)
	require.True(t, ok)
	require.NotNil(t, agents.settled)

	next, cmd = m.updateReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	m = next.(model)
	require.NotNil(t, cmd, "and so does folding a file away")
	_, ok = cmd().(reviewWroteMsg)
	require.True(t, ok)
	require.NotNil(t, agents.marked)
}
