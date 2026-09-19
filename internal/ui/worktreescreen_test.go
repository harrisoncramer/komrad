package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"weave/config"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/layout"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// worktreesModel is the worktrees screen over the fixture listing, with the live agent on the
// agents list and the archived ones in the archive.
func worktreesModel() model {
	archived := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)

	return model{
		frame:         layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:          config.DefaultKeybindings(),
		screen:        screenWorktrees,
		detailsHeight: config.DefaultDetailsHeight,
		listing: listing{
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "weave"}},
			agents: []agentItem{{
				agent: agentstore.Agent{ID: "live", Name: "worktree-view", WorkflowID: "w1", Harness: "claude", State: agentstore.StateThinking, Cwd: "/trees/live"},
				alive: true,
			}, {
				agent: agentstore.Agent{ID: "occ", Name: "review-607", WorkflowID: "w1", Harness: "claude", Cwd: "/trees/live"},
				alive: true,
			}},
		},
		archive: archiveState{agents: []agentstore.Agent{
			{ID: "archived", Name: "review-column", WorkflowID: "w1", Harness: "claude", ArchivedAt: &archived, Cwd: "/trees/archived"},
			{ID: "admin", Name: "custom-cargo-fixes", WorkflowID: "w1", Harness: "claude", ArchivedAt: &archived, Cwd: "/trees/admin"},
		}},
		worktrees: worktreesState{items: worktreeFixture()},
	}
}

func pressWorktrees(t *testing.T, m model, key tea.KeyMsg) (model, tea.Cmd) {
	t.Helper()

	next, cmd := m.updateWorktrees(key)
	updated, ok := next.(model)
	require.True(t, ok)

	return updated, cmd
}

// TestApplyWorktreesSpendsTheFocusOnTheAgentsRow covers a focus set by the key that opened the
// screen landing the cursor on that agent's row, an occupant included, and being spent once.
func TestApplyWorktreesSpendsTheFocusOnTheAgentsRow(t *testing.T) {
	m := worktreesModel()
	m.worktrees.focus = "occ"

	m.applyWorktrees(worktreeListing{items: worktreeFixture()}, "")

	assert.Equal(t, "agent:occ", m.hoveredWorktreeKey())
	assert.Empty(t, m.worktrees.focus)
}

// TestApplyWorktreesKeepsTheCursorOnItsRow covers the cursor following its row as the listing
// changes under it, and the inspections of directories that left the listing being dropped.
func TestApplyWorktreesKeepsTheCursorOnItsRow(t *testing.T) {
	m := worktreesModel()
	m.worktrees.cursor = 6
	m.worktrees.inspected = map[string]worktreeInspection{
		"/trees/orphan": {baseRef: "origin/main"},
		"/trees/gone":   {baseRef: "origin/main"},
	}
	require.Equal(t, "tree:/trees/orphan", m.hoveredWorktreeKey())

	fresh := worktreeFixture()[1:]
	m.applyWorktrees(worktreeListing{items: fresh}, m.hoveredWorktreeKey())

	assert.Equal(t, "tree:/trees/orphan", m.hoveredWorktreeKey())
	assert.Contains(t, m.worktrees.inspected, "/trees/orphan")
	assert.NotContains(t, m.worktrees.inspected, "/trees/gone")
}

// TestOpenWorktreesLandsOnTheHoveredAgent covers the key from the agents list entering the
// screen with the cursor already on that agent's directory.
func TestOpenWorktreesLandsOnTheHoveredAgent(t *testing.T) {
	m := worktreesModel()
	m.screen = screenAgents

	next, _ := m.openWorktrees("archived")
	opened, ok := next.(model)
	require.True(t, ok)

	assert.Equal(t, screenWorktrees, opened.screen)
	assert.Equal(t, "tree:/trees/archived", opened.hoveredWorktreeKey())
}

// TestEnterOnAWorktreeLandsOnItsAgent covers the jump both ways: a live agent to the agents
// list, an archived one to the archive, and a directory with none staying put.
func TestEnterOnAWorktreeLandsOnItsAgent(t *testing.T) {
	t.Run("live", func(t *testing.T) {
		m := worktreesModel()
		m.worktrees.cursor = 3
		require.Equal(t, "tree:/trees/live", m.hoveredWorktreeKey())

		after, _ := pressWorktrees(t, m, keyMsg("enter"))

		assert.Equal(t, screenAgents, after.screen)
		assert.Equal(t, "agent:live", after.hoveredAgentKey())
	})

	t.Run("archived", func(t *testing.T) {
		m := worktreesModel()
		m.worktrees.cursor = 5
		require.Equal(t, "tree:/trees/archived", m.hoveredWorktreeKey())

		after, _ := pressWorktrees(t, m, keyMsg("enter"))

		assert.Equal(t, screenArchive, after.screen)
		assert.Equal(t, "agent:archived", after.hoveredArchiveKey())
	})

	t.Run("nobody's", func(t *testing.T) {
		m := worktreesModel()
		m.worktrees.cursor = 6

		after, _ := pressWorktrees(t, m, keyMsg("enter"))

		assert.Equal(t, screenWorktrees, after.screen)
		assert.False(t, m.canWorktreeRowOpen())
	})
}

// TestWorktreeFateDecidesTheDeletePath covers the three paths and the one stuck row that takes
// the remover's.
func TestWorktreeFateDecidesTheDeletePath(t *testing.T) {
	m := worktreesModel()
	items := worktreeFixture()

	assert.Equal(t, fateLive, m.worktreeFate(items[0]))
	assert.Equal(t, fateArchived, m.worktreeFate(items[1]))
	assert.Equal(t, fateUnowned, m.worktreeFate(items[2]))
	assert.Equal(t, fateUnowned, m.worktreeFate(items[3]))

	stuck := items[0]
	stuck.agent = &worktreeAgent{id: "stuck", name: "stuck", state: agentstore.StateFailedToCleanup}
	assert.Equal(t, fateLive, m.worktreeFate(stuck), "a readable directory is its agent's however the cleanup went")
	stuck.readable = false
	assert.Equal(t, fateUnowned, m.worktreeFate(stuck))
}

// TestDeleteOnAWorktreeOpensTheFormItsFateNames covers the three keys: the archive choice on a
// live row, the delete form on an archived one and on one nothing answers for, and nothing on
// a header.
func TestDeleteOnAWorktreeOpensTheFormItsFateNames(t *testing.T) {
	tests := []struct {
		name   string
		cursor int
		kind   formKind
	}{
		{name: "a header opens nothing", cursor: 2, kind: formNone},
		{name: "a live agent is offered the archive", cursor: 3, kind: formAgentArchive},
		{name: "an archived agent is offered the delete", cursor: 5, kind: formWorktreesDelete},
		{name: "nobody's directory is offered the delete", cursor: 6, kind: formWorktreesDelete},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := worktreesModel()
			m.width, m.height = 200, 40
			m.worktrees.cursor = tt.cursor

			after, _ := pressWorktrees(t, m, keyMsg("d"))

			assert.Equal(t, tt.kind, after.form.kind)
		})
	}
}

// TestClearWorktreesTakesEverythingButTheLive covers the clear key gathering the archived and
// the unowned directories and leaving the live one alone.
func TestClearWorktreesTakesEverythingButTheLive(t *testing.T) {
	m := worktreesModel()
	m.width, m.height = 200, 40

	after, _ := pressWorktrees(t, m, keyMsg("D"))

	require.Equal(t, formWorktreesDelete, after.form.kind)
	assert.Contains(t, formText(after.form.form), "Delete 4 worktrees")
}

// TestDeleteChosenWorktreesRefusesALiveRow covers a bulk delete over a selection holding a
// live agent being refused in the message region rather than archiving anything.
func TestDeleteChosenWorktreesRefusesALiveRow(t *testing.T) {
	m := worktreesModel()
	m.width, m.height = 200, 40
	m.worktrees.selected = selection{keys: []string{"tree:/trees/live", "tree:/trees/orphan"}}

	after, _ := pressWorktrees(t, m, keyMsg("d"))

	assert.Equal(t, formNone, after.form.kind)
	require.NotEmpty(t, after.messages.items)
	assert.Contains(t, after.messages.items[0].summary, "live agent")
}

// TestDeleteWorktreesFormSaysWhatGoes covers the title counting the directories and the size,
// and the branch line following the inspection.
func TestDeleteWorktreesFormSaysWhatGoes(t *testing.T) {
	items := worktreeFixture()
	kept := true

	form, _ := deleteWorktreesForm(worktreeDeletion{agents: items[1:2]}, &kept, config.FixedFormKeys())
	drawn := formText(form)
	assert.Contains(t, drawn, "Delete the worktree of review-column, 49M?")
	assert.Contains(t, drawn, "The branch is kept")

	form, _ = deleteWorktreesForm(worktreeDeletion{agents: items[1:2], unowned: items[2:4]}, nil, config.FixedFormKeys())
	drawn = formText(form)
	assert.Contains(t, drawn, "Delete 3 worktrees, 49M?")
	assert.Contains(t, drawn, "2 directories nothing answers for")
	assert.NotContains(t, drawn, "branch is kept")
}

// formText is a form as it draws once it has been given a window, without the styling.
func formText(form *huh.Form) string {
	form.Init()
	form.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	return ansi.Strip(form.View())
}

// TestWorktreeFieldsReadTheListingAndTheInspection covers the record drawn at once from the
// listing and filled in once the inspection lands, with the agent named through the listing
// that holds it.
func TestWorktreeFieldsReadTheListingAndTheInspection(t *testing.T) {
	m := worktreesModel()
	m.worktrees.cursor = 5

	before := fieldMap(m.worktreesDetails().fields)
	assert.Equal(t, "review-column", before["name"])
	assert.Equal(t, "49M", before["size"])
	assert.Equal(t, "agent/archived", before["branch"])
	assert.Equal(t, "claude, archived Sep 17 at 8:00am", before["agent"])
	assert.Equal(t, "weave", before["workflow"])
	assert.NotContains(t, before, "base")

	m.applyWorktreeInspection(worktreeInspectedMsg{dir: "/trees/archived", inspection: worktreeInspection{
		baseRef:   "origin/main",
		ahead:     2,
		behind:    12,
		modified:  []string{"a.go"},
		untracked: []string{"b.txt", "c.txt"},
	}})

	after := fieldMap(m.worktreesDetails().fields)
	assert.Equal(t, "origin/main  ↑2 ↓12", after["base"])
	assert.Equal(t, "1 modified, 2 untracked", after["changes"])
}

// TestWorktreeFieldsOfAnUnreadableDirectory covers the short record for a registration git
// cannot read.
func TestWorktreeFieldsOfAnUnreadableDirectory(t *testing.T) {
	m := worktreesModel()
	m.worktrees.cursor = 8
	require.Equal(t, "tree:/trees/broken", m.hoveredWorktreeKey())

	fields := fieldMap(m.worktreesDetails().fields)
	assert.Equal(t, "/var/folders/T/repo", fields["repository"])
	assert.Equal(t, "none", fields["agent"])
	assert.NotContains(t, fields, "branch")
}

// TestWorktreeDocDrawsTheInspection covers the document's three blocks, the wait before the
// inspection lands, and an error being drawn rather than asked again.
func TestWorktreeDocDrawsTheInspection(t *testing.T) {
	m := worktreesModel()
	m.worktrees.cursor = 3

	assert.Contains(t, m.worktreeDoc(), "reading the directory")

	m.applyWorktreeInspection(worktreeInspectedMsg{dir: "/trees/live", inspection: worktreeInspection{
		entries:   []worktreeEntry{{path: "local", bytes: 40 * 1024 * 1024, dir: true}, {path: "go.mod", bytes: 2048}},
		modified:  []string{"a.go"},
		untracked: []string{"b.txt"},
		baseRef:   "origin/main",
		commits:   []worktreeCommit{{sha: "87a3bcc98c47", subject: "fix(view): drop the column", committedAt: time.Now().Add(-2 * time.Hour)}},
	}})

	doc := m.worktreeDoc()
	assert.Contains(t, doc, "# worktree-view")
	assert.Contains(t, doc, "local/")
	assert.Contains(t, doc, "40M")
	assert.Contains(t, doc, "M a.go")
	assert.Contains(t, doc, "? b.txt")
	assert.Contains(t, doc, "## Commits since origin/main")
	assert.Contains(t, doc, "87a3bcc98c47  2h   fix(view): drop the column")

	m.applyWorktreeInspection(worktreeInspectedMsg{dir: "/trees/live", inspection: worktreeInspection{err: errors.New("git exploded")}})
	assert.Contains(t, m.worktreeDoc(), "git exploded")
}

// TestEntryLinesCutTheListAndCountTheRest covers the entries beyond the first ten folding
// into one line with their total.
func TestEntryLinesCutTheListAndCountTheRest(t *testing.T) {
	entries := make([]worktreeEntry, 0, 12)
	for i := range 12 {
		entries = append(entries, worktreeEntry{path: strings.Repeat("x", i+1), bytes: 1024})
	}

	lines := entryLines(entries)

	require.Len(t, lines, 11)
	assert.Contains(t, lines[10], "+ 2 more")
	assert.Contains(t, lines[10], "2.0K")
}

// TestStageTickOnTheWorktreesScreenAsksForTheInspection covers the rest of the cursor being
// what sends get_worktree, not the move itself.
func TestStageTickOnTheWorktreesScreenAsksForTheInspection(t *testing.T) {
	m := worktreesModel()
	m.worktrees.cursor = 5

	moved, cmd := pressWorktrees(t, m, keyMsg("j"))
	require.NotNil(t, cmd, "a move schedules a rest")
	assert.Equal(t, "tree:/trees/orphan", moved.hoveredWorktreeKey())

	inspect := moved.applyStageTick(stageTickMsg{move: moved.staging})
	assert.NotNil(t, inspect, "the rest asks for the hovered directory")

	stale := moved.applyStageTick(stageTickMsg{move: moved.staging - 1})
	assert.Nil(t, stale, "a rest the cursor moved on from asks for nothing")
}

// TestLogTargetOnTheWorktreesScreenIsTheHoveredAgent covers the log and terminal keys reading
// this screen's cursor rather than the agents list's.
func TestLogTargetOnTheWorktreesScreenIsTheHoveredAgent(t *testing.T) {
	m := worktreesModel()
	m.worktrees.cursor = 5

	agent, ok := m.logTarget()
	require.True(t, ok)
	assert.Equal(t, "archived", agent.ID)

	m.worktrees.cursor = 6
	_, ok = m.logTarget()
	assert.False(t, ok)
}

func fieldMap(fields []recordField) map[string]string {
	byKey := map[string]string{}
	for _, field := range fields {
		byKey[field.label] = field.value
	}

	return byKey
}
