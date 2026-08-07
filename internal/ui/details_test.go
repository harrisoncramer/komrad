package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"weave/config"

	agentstore "weave/internal/store/agent"

	tea "github.com/charmbracelet/bubbletea"
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

	// Pushed taller than the share the layout gives it, ahead of the pr field the rest of
	// this test reads off the bottom.
	m.listing.agents[0].agent.Metadata = append([]agentstore.Metadata{
		{Key: "branch", Value: "harry/details-height"},
		{Key: "issue", Value: "WEAVE-1"},
	}, m.listing.agents[0].agent.Metadata...)

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

// TestDetailsTitleNamesWhatItIsAbout covers the line over the fields. The same name can be an
// agent on one row and the workflow it sits under on the next, so the title says which, and it
// stands over a blank line the way a list's own title does.
func TestDetailsTitleNamesWhatItIsAbout(t *testing.T) {
	m := detailsModel(&recordingTmux{})

	assert.Contains(t, m.detailsView(), "Agent: auth\n\n")

	m.tree.cursor = 0

	assert.Contains(t, m.detailsView(), "Workflow: backend\n\n")
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

// TestDetailsKeysActOnTheFieldOnScreen covers the row changing under a focused section, which
// a refresh does when the agent the cursor was on is archived from somewhere else: the cursor
// lands on whatever now sits at that index. The section draws that record from its top, so the
// next key has to move from the top and the copy key has to take the field being pointed at.
func TestDetailsKeysActOnTheFieldOnScreen(t *testing.T) {
	clip := &fakeClipboard{}
	m := detailsModel(&recordingTmux{})
	m.clip = clip
	m.tree.placed = true

	focused := send(t, m, "ctrl+t")
	down := send(t, send(t, send(t, focused, "j"), "j"), "j")
	require.Equal(t, 3, down.details.cursor)

	down.applyRefresh(refreshResult{workflows: down.listing.workflows})

	held := down.hoveredDetails()
	require.Equal(t, "w1", held.id, "the workflow header took the archived agent's place")
	require.Equal(t, 0, down.detailsCursor(held), "the section draws the record it swapped to from its top")

	moved := send(t, down, "j")

	assert.Equal(t, 1, moved.detailsCursor(moved.hoveredDetails()))

	copied, cmd := down.updateDetails(keyMsg("y"))
	panel := asModel(t, copied)
	require.NotNil(t, cmd)
	require.NoError(t, copyResultFrom(t, cmd).err)

	assert.Equal(t, []string{held.fields[0].value}, clip.written, "the copy key takes the field on screen")
	assert.Equal(t, 1, panel.details.flashed, "and flashes the line it took it from")
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

// TestDetailsAreDrawnBetweenTwoRules covers the lines that fence the section off: one under
// the list above it and one over the legend below, so the block reads as its own half of the
// sidebar rather than as more rows.
func TestDetailsAreDrawnBetweenTwoRules(t *testing.T) {
	m := detailsModel(&recordingTmux{})

	view := m.detailsView()

	assert.True(t, strings.HasPrefix(view, strings.Repeat("─", m.width)), "the section opens on a rule")
	assert.True(t, strings.HasSuffix(view, strings.Repeat("─", m.width)+"\n"), "and closes on one")
}

// TestDetailsHeightLeavesTheListRoom covers the field lines the layout gives the section,
// which together with its chrome are never more than half the pane however tall the
// configured share is.
func TestDetailsHeightLeavesTheListRoom(t *testing.T) {
	m := detailsModel(&recordingTmux{})

	tests := []struct {
		name   string
		height float64
		want   int
	}{
		{name: "a share of the pane", height: 0.25, want: 6},
		{name: "a whole number of rows", height: 8, want: 8},
		{name: "more than the pane holds is capped so the chrome and fields together take half of it", height: 40, want: 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.detailsHeight = tt.height

			assert.Equal(t, tt.want, m.detailsLines())
		})
	}
}

// TestDetailsShrinksToAShortRecord covers a record shorter than the section's configured
// share: the block draws only the fields the record has, rather than padding out to fill
// the share it was given.
func TestDetailsShrinksToAShortRecord(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	held := m.hoveredDetails()

	require.Less(t, len(held.fields), m.detailsLines(), "the record must be shorter than the share for this to prove anything")

	fieldLines := strings.Count(m.detailsView(), "\n") - detailsChromeLines

	assert.Equal(t, len(held.fields), fieldLines)
}

// TestDetailsNeedsRoomForChromeAndAField covers the guard that closes the section entirely
// when the pane cannot fit the chrome and even one field inside the half it is allowed.
func TestDetailsNeedsRoomForChromeAndAField(t *testing.T) {
	m := detailsModel(&recordingTmux{})

	m.height = 8
	assert.Empty(t, m.detailsView(), "half the pane cannot fit the chrome, let alone a field beside it")

	m.height = 10
	assert.NotEmpty(t, m.detailsView(), "half the pane now fits the chrome and one field")
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

		assert.Contains(t, view, "Schedule: morning-ci")
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

		assert.Contains(t, view, "Agent: api-refactor")
		assert.Contains(t, view, "directory")

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

// strandedModel is a details model with a second agent under the cursor, so the list has a row
// to move onto. The bug being covered is the list going deaf while the section holds the keys
// off screen, which cannot be seen on a cursor already sitting at the last row.
func strandedModel() model {
	m := detailsModel(&recordingTmux{})
	m.listing.agents = append(m.listing.agents, agentItem{
		agent: agentstore.Agent{ID: "a2", Name: "billing", Pane: "%10", Cwd: "/repo", WorkflowID: "w1"},
		alive: true,
	})

	return m
}

// assertKeysWithTheList checks what a section holding the keys off screen got wrong, none of
// it read off the focus itself: the list answers movement, no cursor runs down fields that are
// not drawn, the accent stays on the list, and the legend is still the list's own.
func assertKeysWithTheList(t *testing.T, m model) {
	t.Helper()

	require.Empty(t, m.detailsView(), "the section must draw nothing for this to prove anything")

	moved := send(t, m, "j")

	assert.Equal(t, 2, moved.tree.cursor, "the list answers movement")
	assert.Equal(t, m.details.cursor, moved.details.cursor, "no cursor runs down fields that are not drawn")
	assert.Equal(t, colorAccent, m.titleStyle().GetForeground(), "the list keeps the accent")
	assert.Equal(t, colorAccent, m.rowCursorStyle().GetForeground())
	assert.NotContains(t, m.renderHelp(m.agentHelp()), "back to the list", "and the legend is its own")
}

// TestDetailsFocusNeedsRoomToDrawIn covers the focus key pressed at a pane too short for the
// section to be drawn at all. The keys stay with the list rather than going to a block that is
// not on screen.
func TestDetailsFocusNeedsRoomToDrawIn(t *testing.T) {
	m := strandedModel()
	m.height = 8

	assertKeysWithTheList(t, send(t, m, "ctrl+t"))
}

// TestDetailsFocusGivenUpWhenThePaneShrinks covers the terminal shrinking under a focused
// section, which takes the section off screen without anyone pressing a key. The keys go back
// to the list at once rather than a keypress being spent taking them back.
func TestDetailsFocusGivenUpWhenThePaneShrinks(t *testing.T) {
	focused := send(t, strandedModel(), "ctrl+t")
	require.NotEmpty(t, focused.detailsView())

	assertKeysWithTheList(t, asModel(t, resize(focused, 40, 8)))
}

// TestDetailsShareTooSmallToDrawAField covers a configured share that rounds down to no field
// lines in a pane with room to spare, which a small details_height does without any resize.
func TestDetailsShareTooSmallToDrawAField(t *testing.T) {
	m := strandedModel()
	m.height, m.detailsHeight = 19, 0.05
	require.Equal(t, 0, m.detailsLines(), "the share rounds down to nothing while half the pane would hold five lines")

	assertKeysWithTheList(t, send(t, m, "ctrl+t"))
}

// TestDetailsFocusReturnsWithTheSection covers the terminal shrinking and being pulled back
// out. The keys go to the list while the section is off screen and come back to the section
// with it, on the field they were left on, rather than the focus being spent by the resize.
func TestDetailsFocusReturnsWithTheSection(t *testing.T) {
	focused := send(t, send(t, strandedModel(), "ctrl+t"), "j")
	require.Equal(t, 1, focused.details.cursor)

	shrunk := asModel(t, resize(focused, 40, 8))
	assertKeysWithTheList(t, shrunk)

	grown := asModel(t, resize(shrunk, 40, 24))
	require.NotEmpty(t, grown.detailsView())

	moved := send(t, grown, "j")

	assert.Equal(t, 2, moved.details.cursor, "the keys are back on the fields")
	assert.Equal(t, 1, moved.tree.cursor, "and off the list")
	assert.Equal(t, colorMuted, grown.titleStyle().GetForeground(), "the accent goes back with them")
}

func resize(m model, width, height int) tea.Model {
	next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})

	return next
}
