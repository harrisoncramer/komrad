package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"weave/config"
	"weave/lib/xptr"
	"weave/lib/xslice"

	agentstore "weave/internal/store/agent"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

	m.listing.agents[0].agent.Metadata = append([]agentstore.Metadata{
		{Key: "branch", Value: "harry/details-height"},
		{Key: "issue", Value: "WEAVE-1"},
		{Key: "summary", Value: "gave the record two more lines"},
	}, m.listing.agents[0].agent.Metadata...)

	view := m.detailsView()

	assert.Contains(t, view, "auth")
	assert.Contains(t, view, "directory")
	assert.Contains(t, view, "/repo")

	assert.Contains(t, view, "more")

	m.details.row = m.hoveredDetails().id
	m.details.cursor = len(m.hoveredDetails().fields) - 1

	assert.Contains(t, m.detailsView(), "pr")
	assert.Contains(t, m.detailsView(), "https://github.com/o/r/pu")

	m.tree.cursor = 0

	header := m.detailsView()

	assert.Contains(t, header, "backend")
	assert.Contains(t, header, "agents")
	assert.NotContains(t, header, "session_id", "a workflow has no session of its own")
}

// TestDetailsNameWhatItIsAbout covers the record opening on the name of whatever the cursor is
// standing on, agent or workflow, now that no title line says which it is.
func TestDetailsNameWhatItIsAbout(t *testing.T) {
	m := detailsModel(&recordingTmux{})

	assert.Equal(t, "auth", fieldValue(t, m.hoveredDetails(), "name"))

	m.tree.cursor = 0

	assert.Equal(t, "backend", fieldValue(t, m.hoveredDetails(), "name"))
}

// TestDetailsFollowTheCursor covers moving between rows, which puts the field cursor back at
// the top: the field it was on belongs to the row it was left on.
func TestDetailsFollowTheCursor(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.details.row, m.details.cursor = m.hoveredDetails().id, 3

	moved, _ := m.moveCursor(-1)

	assert.Equal(t, 0, moved.detailsCursor(moved.hoveredDetails()))
}

// TestDetailsFocusTakesTheKeys covers the focus key, after which movement runs down the fields
// while the list keeps its cursor on the row being read.
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

	bottom := send(t, focused, "G")
	copied, cmd := bottom.updateDetails(keyMsg("y"))
	panel, ok := copied.(model)
	require.True(t, ok)
	require.NotNil(t, cmd)

	msg := copyResultFrom(t, cmd)

	require.NoError(t, msg.err)
	assert.Equal(t, []string{"https://github.com/o/r/pull/312"}, clip.written)
	assert.Positive(t, panel.details.flashed, "the copied field flashes while the write runs")
}

// TestDetailsKeysStandDownForTheRow covers a key that would archive or rename an agent while
// the section holds the cursor, which does nothing rather than acting on the row above.
func TestDetailsKeysStandDownForTheRow(t *testing.T) {
	m := detailsModel(&recordingTmux{})

	focused := send(t, m, "ctrl+t")
	for _, key := range []string{"z", "r", "n"} {
		after := send(t, focused, key)

		assert.Equal(t, screenAgents, after.screen, "%q opened something", key)
		assert.False(t, after.form.isActive(), "%q opened a form", key)
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

// TestDetailsCursorStartsAtTheTopOfAnotherRow covers the row changing under a cursor left
// partway down the fields, which a refresh that reorders rows does with no key pressed.
func TestDetailsCursorStartsAtTheTopOfAnotherRow(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.details.row, m.details.cursor = "another-agent", 4

	assert.Equal(t, 0, m.detailsCursor(m.hoveredDetails()))
}

// TestDetailsKeysActOnTheFieldOnScreen covers the row changing under a focused section, where
// the section redraws from its top and the next key has to act on the field now pointed at.
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
		{
			name:   "a share of the pane",
			height: 0.25,
			want:   6,
		},
		{
			name:   "a whole number of rows",
			height: 8,
			want:   8,
		},
		{
			name:   "more than the pane holds is capped so the chrome and fields together take half of it",
			height: 40,
			want:   10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.detailsHeight = tt.height

			assert.Equal(t, tt.want, m.detailsLines())
		})
	}
}

// TestDetailsPadsToItsShare covers a record shorter than the section's configured share: the
// block fills the rest of its share with blank lines rather than shrinking, so the list above
// it does not reflow as the cursor moves between records with different field counts.
func TestDetailsPadsToItsShare(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.detailsHeight = 8
	held := m.hoveredDetails()

	require.Less(t, len(held.fields), m.detailsLines(), "the record must be shorter than the share for this to prove anything")

	fieldLines := strings.Count(m.detailsView(), "\n") - detailsChromeLines

	assert.Equal(t, m.detailsLines(), fieldLines)
}

// TestDetailsNeedsRoomForChromeAndAField covers the guard that closes the section entirely
// when the pane cannot fit the chrome and even one field inside the half it is allowed.
func TestDetailsNeedsRoomForChromeAndAField(t *testing.T) {
	m := detailsModel(&recordingTmux{})

	m.height = 4
	assert.Empty(t, m.detailsView(), "half the pane cannot fit the chrome, let alone a field beside it")

	m.height = 6
	assert.NotEmpty(t, m.detailsView(), "half the pane now fits the chrome and one field")
}

// TestDetailsOnTheOtherLists covers the same section drawn under the two lists that are not
// the agents tree, where the focus key behaves the same wherever it is pressed.
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
		name       string
		schedule   string
		branch     *string
		metadata   []agentstore.Metadata
		wantLabels []string
		wantValues []string
		wantAbsent []string
		wantOnce   []string
	}{
		{
			name:       "what it was launched with, in the order it was launched with",
			wantLabels: []string{"name", "agent", "directory", "flags", "agent_id", "session_id"},
			wantValues: []string{"auth", "—", "/repo", "—", "a1", "abc"},
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
		{
			name:       "the branch the checkout is on shows without the agent recording it",
			branch:     xptr.Ptr("harry/rate-limits"),
			wantLabels: []string{"branch"},
			wantValues: []string{"harry/rate-limits"},
		},
		{
			name:       "the branch read from the checkout wins over one the agent recorded itself",
			branch:     xptr.Ptr("harry/rate-limits"),
			metadata:   []agentstore.Metadata{{Key: "branch", Value: "harry/stale"}},
			wantValues: []string{"harry/rate-limits"},
			wantAbsent: []string{"harry/stale"},
			wantOnce:   []string{"branch"},
		},
		{
			name: "why a create or cleanup failed is left to the failed placeholder, not repeated here",
			metadata: []agentstore.Metadata{
				{Key: "error", Value: "the provisioner failed: exited 1"},
				{Key: "branch", Value: "harry/rate-limits"},
			},
			wantLabels: []string{"branch"},
			wantAbsent: []string{"error"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := agent
			agent.Metadata = tt.metadata
			agent.Branch = tt.branch

			fields := agentFields(agent, tt.schedule, "")
			require.NotEmpty(t, fields)
			assert.Equal(t, "name", fields[0].label, "the name it is filed under leads")

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
				assert.NotContains(t, values, absent)
			}
			for _, once := range tt.wantOnce {
				assert.Equal(t, 1, xslice.Count(labels, func(label string) bool { return label == once }))
			}
		})
	}
}

func strandedModel() model {
	m := detailsModel(&recordingTmux{})
	m.listing.agents = append(m.listing.agents, agentItem{
		agent: agentstore.Agent{
			ID:         "a2",
			Name:       "billing",
			Pane:       "%10",
			Cwd:        "/repo",
			WorkflowID: "w1",
		},
		alive: true,
	})

	return m
}

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

// TestDetailsFocusNeedsRoomToDrawIn covers the focus key in a pane too short to draw the
// section, where the keys stay with the list rather than going somewhere off screen.
func TestDetailsFocusNeedsRoomToDrawIn(t *testing.T) {
	m := strandedModel()
	m.height = 4

	assertKeysWithTheList(t, send(t, m, "ctrl+t"))
}

// TestDetailsFocusGivenUpWhenThePaneShrinks covers the terminal shrinking a focused section
// off screen, where the keys go back to the list without a keypress being spent on it.
func TestDetailsFocusGivenUpWhenThePaneShrinks(t *testing.T) {
	focused := send(t, strandedModel(), "ctrl+t")
	require.NotEmpty(t, focused.detailsView())

	assertKeysWithTheList(t, asModel(t, resize(focused, 40, 4)))
}

// TestDetailsShareTooSmallToDrawAField covers a configured share that rounds down to no field
// lines in a pane with room to spare, which a small details_height does without any resize.
func TestDetailsShareTooSmallToDrawAField(t *testing.T) {
	m := strandedModel()
	m.height, m.detailsHeight = 19, 0.05
	require.Equal(t, 0, m.detailsLines(), "the share rounds down to nothing while half the pane would hold five lines")

	assertKeysWithTheList(t, send(t, m, "ctrl+t"))
}

// TestDetailsFocusReturnsWithTheSection covers the terminal shrinking and growing again, where
// the keys come back to the section on the field they were left on.
func TestDetailsFocusReturnsWithTheSection(t *testing.T) {
	focused := send(t, send(t, strandedModel(), "ctrl+t"), "j")
	require.Equal(t, 1, focused.details.cursor)

	shrunk := asModel(t, resize(focused, 40, 4))
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

// TestAgentFieldsReviewingRow pins a reviewer's record drawing whose work it reads exactly
// once.
func TestAgentFieldsReviewingRow(t *testing.T) {
	tests := []struct {
		name       string
		role       agentstore.Role
		metadata   []agentstore.Metadata
		wantLabels []string
		wantOnce   []string
		wantAbsent []string
	}{
		{
			name:       "a reviewer that recorded the key too draws one row, not two",
			role:       agentstore.RoleReviewer,
			metadata:   []agentstore.Metadata{{Key: "reviewing", Value: "01a0-stale"}},
			wantLabels: []string{"reviewing"},
			wantOnce:   []string{"reviewing"},
			wantAbsent: []string{"01a0-stale", "attached to"},
		},
		{
			name:       "an ordinary child is drawn as attached rather than reviewing",
			role:       agentstore.RoleImplementer,
			wantLabels: []string{"attached to"},
			wantAbsent: []string{"reviewing"},
		},
		{
			name:       "a child that recorded the key without the role keeps what it recorded",
			role:       agentstore.RoleImplementer,
			metadata:   []agentstore.Metadata{{Key: "reviewing", Value: "01a0-someone"}},
			wantLabels: []string{"attached to", "reviewing"},
			wantOnce:   []string{"reviewing"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := agentstore.Agent{
				Name:          "reader",
				Role:          tt.role,
				ParentAgentID: xptr.Ptr("01a0-parent"),
				Metadata:      tt.metadata,
			}

			fields := agentFields(agent, "", "author")

			labels := make([]string, 0, len(fields))
			values := make([]string, 0, len(fields))
			for _, field := range fields {
				labels = append(labels, field.label)
				values = append(values, field.value)
			}

			assert.Subset(t, labels, tt.wantLabels)
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, labels, absent)
				assert.NotContains(t, values, absent)
			}
			for _, once := range tt.wantOnce {
				assert.Equal(t, 1, countOf(labels, once), "the row is drawn once")
			}
		})
	}
}

func countOf(labels []string, want string) int {
	seen := 0
	for _, label := range labels {
		if label == want {
			seen++
		}
	}

	return seen
}

// TestDetailsKeepsToNineLinesByDefault covers the section on a tall sidebar, where the default is
// a line count rather than a share, so the room the list gains from a taller pane stays with it.
func TestDetailsKeepsToNineLinesByDefault(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.height = 60
	m.detailsHeight = config.DefaultDetailsHeight

	assert.Equal(t, 9, m.detailsLines())
}

// TestDetailsScrollPastTheLinesItHas covers a record with more fields than the section draws,
// which windows around the cursor and says how many are left over on either side of it.
func TestDetailsScrollPastTheLinesItHas(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.height, m.detailsHeight = 60, config.DefaultDetailsHeight
	for i := range 12 {
		m.listing.agents[0].agent.Metadata = append(m.listing.agents[0].agent.Metadata, agentstore.Metadata{
			Key:   fmt.Sprintf("key%d", i),
			Value: fmt.Sprintf("value%d", i),
		})
	}

	held := m.hoveredDetails()
	require.Greater(t, len(held.fields), m.detailsLines())

	assert.Contains(t, m.detailsView(), "↓")

	bottom := m.setDetailsCursor(held, len(held.fields)-1)

	assert.Contains(t, bottom.detailsView(), "↑")
}
