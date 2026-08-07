package ui

import (
	"errors"
	"testing"
	"time"

	"weave/config"

	agentstore "weave/internal/store/agent"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// detailsModel builds an agents model sized like a real sidebar, its cursor on the one live
// agent, which carries the metadata an agent records about its own work.
func detailsModel(tmux *recordingTmux) model {
	m := infoModel(tmux)
	m.height, m.width = 24, 40
	m.clip = &fakeClipboard{}
	m.listing.agents[0].agent.ID = "a1"
	m.listing.agents[0].agent.SessionID = "abc"
	m.listing.agents[0].agent.Metadata = []agentstore.Metadata{
		{Key: "pr", Value: "https://github.com/o/r/pull/312"},
	}

	return m
}

// send routes a key the way a real keypress arrives, through Update rather than into a
// screen's own handler, since which half of the sidebar answers a key is decided above them.
func send(t *testing.T, m model, key string) model {
	t.Helper()

	next, _ := m.Update(keyMsg(key))
	panel, ok := next.(model)
	require.True(t, ok)

	return panel
}

// TestDetailsDrawTheHoveredRow covers the section under the list, which follows the cursor:
// an agent shows what it was launched with and what it has recorded since, and the workflow
// header above it shows what its agents inherit instead.
func TestDetailsDrawTheHoveredRow(t *testing.T) {
	m := detailsModel(&recordingTmux{})

	view := m.detailsView()

	assert.Contains(t, view, "auth")
	assert.Contains(t, view, "directory")
	assert.Contains(t, view, "/repo")

	// The record is taller than the share the layout gives it, so what is past the end is
	// counted rather than drawn, and reached by moving the cursor onto it.
	assert.Contains(t, view, "more")

	m.details.row = m.hoveredDetails().id
	m.details.cursor = len(m.hoveredDetails().fields) - 1

	// The value is clipped to the pane rather than wrapped, since the whole of it is a copy
	// key away and a wrapped URL would cost the fields under it their lines.
	assert.Contains(t, m.detailsView(), "pr")
	assert.Contains(t, m.detailsView(), "https://github.com/o/r/pu")

	m.tree.cursor = 0

	header := m.detailsView()

	assert.Contains(t, header, "backend")
	assert.Contains(t, header, "agents")
	assert.NotContains(t, header, "session_id", "a workflow has no session of its own")
}

// TestDetailsFollowTheCursor covers moving between rows, which puts the field cursor back at
// the top: the field it was on belongs to the row it was left on.
func TestDetailsFollowTheCursor(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.details.row, m.details.cursor = m.hoveredDetails().id, 3

	moved := m.moveCursor(-1)

	assert.Equal(t, 0, moved.detailsCursor(moved.hoveredDetails()))
}

// TestDetailsFocusTakesTheKeys covers the focus key. The list keeps its cursor while the
// section holds the keys, so the row being read stays marked, and movement runs down the
// fields rather than the rows.
func TestDetailsFocusTakesTheKeys(t *testing.T) {
	m := detailsModel(&recordingTmux{})

	focused := send(t, m, "ctrl+t")
	require.True(t, focused.details.focused)

	moved := send(t, focused, "j")

	assert.Equal(t, 1, moved.details.cursor)
	assert.Equal(t, 1, moved.tree.cursor, "the row under the list cursor does not move")

	back := send(t, moved, "ctrl+t")
	require.False(t, back.details.focused)

	listMoved := send(t, back, "k")

	assert.Equal(t, 0, listMoved.tree.cursor, "the list has the keys again")
}

// TestDetailsCopyTheFieldUnderTheCursor covers the copy key, which takes the value rather
// than the line it is drawn on, so a clipped URL pastes whole.
func TestDetailsCopyTheFieldUnderTheCursor(t *testing.T) {
	clip := &fakeClipboard{}
	m := detailsModel(&recordingTmux{})
	m.clip = clip

	focused := send(t, m, "ctrl+t")

	// The pr the agent recorded is the last of its fields, which is what the bottom key lands on.
	bottom := send(t, focused, "G")
	copied, cmd := bottom.updateDetails(keyMsg("y"))
	panel, ok := copied.(model)
	require.True(t, ok)
	require.NotNil(t, cmd)

	msg := copyResultFrom(t, cmd)

	assert.NoError(t, msg.err)
	assert.Equal(t, []string{"https://github.com/o/r/pull/312"}, clip.written)
	assert.Positive(t, panel.details.flashed, "the copied field flashes while the write runs")
}

// TestDetailsKeysStandDownForTheRow covers the keys that act on an agent while the section
// below holds the cursor. The field under the cursor is not the agent, so a key that would
// archive or rename one does nothing rather than acting on the row above.
func TestDetailsKeysStandDownForTheRow(t *testing.T) {
	m := detailsModel(&recordingTmux{})

	focused := send(t, m, "ctrl+t")
	for _, key := range []string{"z", "r", "n"} {
		after := send(t, focused, key)

		assert.Equal(t, screenAgents, after.screen, "%q opened something", key)
		assert.Nil(t, after.form, "%q opened a form", key)
	}
}

// TestDetailsFocusNeedsFieldsToPointAt covers the key pressed with nothing listed, where
// there is no record under the cursor and so nothing to hand the keys to.
func TestDetailsFocusNeedsFieldsToPointAt(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.listing = listing{}
	m.tree = treeState{}

	focused := send(t, m, "ctrl+t")

	assert.False(t, focused.details.focused)
	assert.Empty(t, focused.detailsView())
}

// TestDetailsCursorHeldInsideTheRecord covers a record that shortens under a cursor sitting
// past its new end, which is what an agent dropping a metadata key does to the section.
func TestDetailsCursorHeldInsideTheRecord(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	held := m.hoveredDetails()
	m.details.row, m.details.cursor = held.id, 20

	assert.Equal(t, len(held.fields)-1, m.detailsCursor(held))
}

// TestDetailsCursorStartsAtTheTopOfAnotherRow covers the cursor left partway down one
// record while the row under it changes, which a refresh that reorders rows does without
// anyone pressing a key. The field it was on belonged to the row it was set against.
func TestDetailsCursorStartsAtTheTopOfAnotherRow(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.details.row, m.details.cursor = "another-agent", 4

	assert.Equal(t, 0, m.detailsCursor(m.hoveredDetails()))
}

// TestDetailsCopyReportsAFailure covers the clipboard itself refusing the write, such as no
// clipboard utility being on the machine's PATH.
func TestDetailsCopyReportsAFailure(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.clip = &fakeClipboard{err: errors.New("no clipboard utility found")}

	focused := send(t, m, "ctrl+t")
	_, cmd := focused.updateDetails(keyMsg("y"))
	require.NotNil(t, cmd)

	assert.EqualError(t, copyResultFrom(t, cmd).err, "no clipboard utility found")
}

// TestDetailsHeightLeavesTheListRoom covers the share the layout gives the section, which is
// never more than half the pane however tall the configured share is.
func TestDetailsHeightLeavesTheListRoom(t *testing.T) {
	m := detailsModel(&recordingTmux{})

	tests := []struct {
		name   string
		height float64
		want   int
	}{
		{name: "a share of the pane", height: 0.25, want: 6},
		{name: "a whole number of rows", height: 8, want: 8},
		{name: "more than the pane holds is capped at half of it", height: 40, want: 12},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.detailsHeight = tt.height

			assert.Equal(t, tt.want, m.detailsLines())
		})
	}
}

// TestDetailsOnTheOtherLists covers the section under the two lists that are not the agents
// tree. Each says what its own hovered row holds, and the section itself is the same one: the
// focus key takes the keys the same way from wherever it is drawn.
func TestDetailsOnTheOtherLists(t *testing.T) {
	t.Run("a schedule shows when it fires and what its runs are launched with", func(t *testing.T) {
		m := scheduleModel(&fakeSchedules{}, config.DefaultKeybindings())
		m.height, m.width = 24, 40
		m.detailsHeight = config.DefaultDetailsHeight

		view := m.detailsView()

		assert.Contains(t, view, "morning-ci")
		assert.Contains(t, view, "cron")
		assert.Contains(t, view, "0 9 * * 1-5")

		focused := send(t, m, "ctrl+t")

		assert.True(t, focused.details.focused)
	})

	t.Run("an archived agent shows what it was launched with", func(t *testing.T) {
		m := archiveModel(emptyLists{}, emptyLists{}, config.DefaultKeybindings())
		m.height, m.width = 24, 40
		m.detailsHeight = config.DefaultDetailsHeight

		view := m.detailsView()

		assert.Contains(t, view, "api-refactor")
		assert.Contains(t, view, "agent_id")

		focused := send(t, m, "ctrl+t")

		assert.True(t, focused.details.focused)
	})
}

// TestAgentFields covers what an agent shows in the section: what it was launched with,
// the schedule that started it when one did, and whatever it has recorded about its own work.
func TestAgentFields(t *testing.T) {
	agent := agentstore.Agent{
		ID:        "a1",
		Name:      "auth",
		Cwd:       "/repo",
		SessionID: "abc",
		CreatedAt: time.Date(2026, 7, 24, 15, 4, 0, 0, time.UTC),
	}

	tests := []struct {
		name string
		// schedule is the name of the schedule that launched the agent, empty for one a
		// person started.
		schedule string
		// metadata is what the agent recorded about its own work, empty for one that
		// recorded nothing.
		metadata   []agentstore.Metadata
		wantLabels []string
		wantValues []string
		wantAbsent []string
	}{
		{
			name:       "what it was launched with, in the order it was launched with",
			wantLabels: []string{"agent", "directory", "flags", "agent_id", "session_id"},
			wantValues: []string{"—", "/repo", "—", "a1", "abc"},
		},
		{
			name:       "an agent a schedule launched names the schedule",
			schedule:   "morning-ci",
			wantLabels: []string{"launched by"},
			wantValues: []string{"morning-ci"},
		},
		{
			name:       "one a person started claims no schedule",
			wantAbsent: []string{"launched by"},
		},
		{
			name:       "an agent that recorded nothing adds nothing for it",
			wantAbsent: []string{"pr", "branch"},
		},
		{
			name: "what the agent recorded comes after what it was launched with",
			metadata: []agentstore.Metadata{
				{Key: "branch", Value: "harry/rate-limits"},
				{Key: "pr", Value: "https://github.com/o/r/pull/312"},
			},
			wantLabels: []string{"branch", "pr"},
			wantValues: []string{"harry/rate-limits", "https://github.com/o/r/pull/312"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := agent
			agent.Metadata = tt.metadata

			fields := agentFields(agent, tt.schedule)

			labels := make([]string, 0, len(fields))
			values := make([]string, 0, len(fields))
			for _, field := range fields {
				labels = append(labels, field.label)
				values = append(values, field.value)
			}

			assert.Subset(t, labels, tt.wantLabels)
			assert.Subset(t, values, tt.wantValues)
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, labels, absent)
			}
		})
	}
}
