package ui

import (
	"strings"
	"testing"

	"weave/lib/xptr"

	agentstore "weave/internal/store/agent"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNoticesSpellOutTheMarksOnTheRow covers the top of the record: every glyph the hovered row
// is carrying is said again in words, since the row has no room to say what a glyph means.
func TestNoticesSpellOutTheMarksOnTheRow(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.width = 80
	m.listing.agents[0].agent.WorktreeRemoved = true

	view := m.detailsView()

	assert.Contains(t, view, worktreeRemovedIcon)
	assert.Contains(t, view, "worktree gone")
	assert.Contains(t, view, "directory", "the fields are still drawn under the notices")

	m.listing.agents[0].agent.WorktreeRemoved = false

	assert.NotContains(t, m.detailsView(), "worktree gone", "a mark the row is not carrying says nothing")
}

// TestNoticesCoverEveryOptionalMark walks the marks one at a time, since each is a different
// fact about the agent and the row draws them from different places.
func TestNoticesCoverEveryOptionalMark(t *testing.T) {
	tests := []struct {
		name string
		item agentItem
		want string
	}{
		{"a failed create", agentItem{agent: agentstore.Agent{State: agentstore.StateFailedToCreate}}, failedToCreateNotice},
		{"a failed cleanup", agentItem{agent: agentstore.Agent{State: agentstore.StateFailedToCleanup}}, failedToCleanupNotice},
		{"a removed worktree", agentItem{agent: agentstore.Agent{WorktreeRemoved: true}}, worktreeRemovedNotice},
		{"a hidden row", agentItem{agent: agentstore.Agent{Hidden: true}}, hiddenNotice},
		{"mail waiting", agentItem{unreadMail: true}, unreadMailNotice},
		{"output nobody read", agentItem{unviewed: true}, unviewedNotice},
		{"a failed script", agentItem{failedRun: true}, failedRunNotice},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			notices := agentNotices(tt.item)

			require.Len(t, notices, 1)
			assert.Equal(t, tt.want, notices[0].text)
		})
	}

	assert.Empty(t, agentNotices(agentItem{alive: true}), "an agent carrying nothing has nothing to say")
}

// TestNoticesAreOrderedByUrgency covers what a short pane gives up: the marks a person would
// act on are drawn first, so the ones dropped from the tail are the ones nobody needed spelled out.
func TestNoticesAreOrderedByUrgency(t *testing.T) {
	item := agentItem{
		agent:      agentstore.Agent{State: agentstore.StateFailedToCreate, WorktreeRemoved: true, Hidden: true},
		unreadMail: true,
		unviewed:   true,
		failedRun:  true,
	}

	texts := make([]string, 0, len(agentNotices(item)))
	for _, n := range agentNotices(item) {
		texts = append(texts, n.text)
	}

	assert.Equal(t, []string{
		failedToCreateNotice,
		failedRunNotice,
		unreadMailNotice,
		worktreeRemovedNotice,
		unviewedNotice,
		hiddenNotice,
	}, texts)
}

// TestArchivedNoticesAreOnlyTheFailures covers the archive, whose rows draw one mark of their
// own, since the rest stand for facts about an agent that is still running.
func TestArchivedNoticesAreOnlyTheFailures(t *testing.T) {
	agent := agentstore.Agent{State: agentstore.StateFailedToCleanup, ScheduleID: xptr.Ptr("s1"), Hidden: true}

	require.Len(t, failureNotices(agent.State), 1)
	assert.Equal(t, failedToCleanupNotice, failureNotices(agent.State)[0].text)
	assert.Empty(t, failureNotices(agentstore.StateIdle))
}

// TestNoticesKeepTheRecordsHeight covers the share the record is allowed: the notices take their
// lines from the fields rather than from the list above, so the block is the height it always was.
func TestNoticesKeepTheRecordsHeight(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.height, m.width = 40, 60

	plain := strings.Count(m.detailsView(), "\n")

	m.listing.agents[0].agent.WorktreeRemoved = true
	m.listing.agents[0].unviewed = true
	m.listing.agents[0].failedRun = true

	flagged := m.detailsView()

	assert.Equal(t, plain, strings.Count(flagged, "\n"))
	assert.Contains(t, flagged, "worktree gone")
	assert.Contains(t, flagged, "agent_id", "a field survives however many marks the row carries")
}

// TestNoticesGiveUpBeforeTheFields covers a pane too short for everything: the notices that do
// not fit are dropped, and the fields keep a line.
func TestNoticesGiveUpBeforeTheFields(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.height, m.width = 12, 40
	m.listing.agents[0].agent.WorktreeRemoved = true
	m.listing.agents[0].unviewed = true
	m.listing.agents[0].failedRun = true

	view := m.detailsView()

	assert.Equal(t, m.detailsLines()+detailsChromeLines, strings.Count(view, "\n"))
	assert.NotContains(t, view, "a script weave fired", "the last mark is given up rather than the fields")
	assert.Contains(t, view, "name", "a field is still drawn")
}

// TestNoticesCutTheFirstOneShort covers a pane too short for even one whole sentence, where the
// notice is drawn as far as it goes and says so, since the glyph alone would say nothing.
func TestNoticesCutTheFirstOneShort(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.height, m.width = 16, 24
	m.listing.agents[0].failedRun = true

	view := m.detailsView()

	assert.Contains(t, view, "…")
	assert.Contains(t, view, "run failed")
	assert.NotContains(t, view, "opened the log it went to")
}

// TestNoticesLeaveRoomToCountTheFields covers what the record keeps back for the fields: enough
// for a field and the line saying how many more there are.
func TestNoticesLeaveRoomToCountTheFields(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.height, m.width = 20, 40
	m.listing.agents[0].unviewed = true

	view := m.detailsView()

	assert.Contains(t, view, "name")
	assert.Contains(t, view, "more", "the record still says how many fields it left out")
}
