package ui

import (
	"strings"
	"testing"

	"weave/config"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/layout"
	"weave/internal/ui/paint"
	"weave/lib/xptr"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAgentsViewRowWidthIsStableRegardlessOfBranch covers the fixed slot left where the
// branch glyph used to draw: an agent with a branch and one without must produce rows of
// the same width, so nothing jitters as the cwd syncer catches up.
func TestAgentsViewRowWidthIsStableRegardlessOfBranch(t *testing.T) {
	withBranch := model{
		frame: layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:  config.DefaultKeybindings(),
		listing: listing{workflows: []workflowstore.Workflow{{ID: "w1", Name: "acmecorp"}}, agents: []agentItem{{agent: agentstore.Agent{
			ID:         "a1",
			Name:       "worker",
			WorkflowID: "w1",
			Branch:     xptr.Ptr("agent/a1"),
		}}}},
	}
	withoutBranch := withBranch
	withoutBranch.listing.agents = []agentItem{{agent: agentstore.Agent{
		ID:         "a1",
		Name:       "worker",
		WorkflowID: "w1",
	}}}

	rowWithBranch, ok := rowNamed(strings.Split(withBranch.agentsView(), "\n"), "worker")
	require.True(t, ok)
	assert.NotContains(t, rowWithBranch, "agent/a1", "the branch name itself is never drawn")

	rowWithoutBranch, ok := rowNamed(strings.Split(withoutBranch.agentsView(), "\n"), "worker")
	require.True(t, ok)

	assert.Len(t, rowWithoutBranch, len(rowWithBranch))
}

// TestUncertainStateMark covers the row weave draws for a pane it captured and could not read,
// which is its own glyph rather than the idle dot it used to be collapsed into.
func TestUncertainStateMark(t *testing.T) {
	assert.Equal(t, "?", stateIcon(agentstore.StateUncertain))
	assert.NotEqual(t, stateIcon(agentstore.StateIdle), stateIcon(agentstore.StateUncertain))
	assert.Equal(t, paint.ToneMuted, stateTone(agentstore.StateUncertain), "an unreadable pane is quiet, not an alarm")
}

// TestWorktreeMark covers the one glyph the row spends on the state of a checkout, which
// says a cwd is gone, a sequence is stopped in it, or nothing at all.
func TestWorktreeMark(t *testing.T) {
	tests := []struct {
		name    string
		agent   agentstore.Agent
		wantOne string
	}{
		{
			name: "a settled checkout is drawn as nothing",
		},
		{
			name:    "a cwd the syncer found gone is drawn with the removed icon",
			agent:   agentstore.Agent{WorktreeRemoved: true},
			wantOne: worktreeRemovedIcon,
		},
		{
			name:    "a checkout part-way through a rebase is drawn with the sequence icon",
			agent:   agentstore.Agent{SequenceInProgress: true},
			wantOne: sequenceInProgressIcon,
		},
		{
			name:    "a cwd that is gone outranks a sequence nothing can be doing in it",
			agent:   agentstore.Agent{WorktreeRemoved: true, SequenceInProgress: true},
			wantOne: worktreeRemovedIcon,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := worktreeMark(paint.Colored, agentItem{agent: tt.agent})
			if tt.wantOne == "" {
				assert.Empty(t, got)
				return
			}

			assert.Contains(t, got, tt.wantOne)
		})
	}
}

// TestAgentsViewShowsWorktreeRemoved covers the row itself: an agent the cwd syncer
// found gone shows the icon beside its name, and one it has not draws the row without it.
func TestAgentsViewShowsWorktreeRemoved(t *testing.T) {
	m := model{
		frame: layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:  config.DefaultKeybindings(),
		listing: listing{workflows: []workflowstore.Workflow{{ID: "w1", Name: "acmecorp"}}, agents: []agentItem{
			{agent: agentstore.Agent{
				ID:              "a1",
				Name:            "worker",
				WorkflowID:      "w1",
				WorktreeRemoved: true,
			}},
			{agent: agentstore.Agent{
				ID:         "a2",
				Name:       "reviewer",
				WorkflowID: "w1",
			}},
		}},
	}

	rows := strings.Split(m.agentsView(), "\n")
	worker, ok := rowNamed(rows, "worker")
	require.True(t, ok)
	assert.Contains(t, worker, worktreeRemovedIcon)

	reviewer, ok := rowNamed(rows, "reviewer")
	require.True(t, ok)
	assert.NotContains(t, reviewer, worktreeRemovedIcon)
}

// TestUnreadMailMark pins that the glyph is drawn only for an agent with mail waiting.
func TestUnreadMailMark(t *testing.T) {
	assert.Empty(t, unreadMailMark(paint.Colored, false))
	assert.Contains(t, unreadMailMark(paint.Colored, true), unreadMailIcon)
}

// TestAgentsViewUnreadMail pins the mail glyph drawn per agent, so one with mail waiting
// shows it and the one beside it does not.
func TestAgentsViewUnreadMail(t *testing.T) {
	m := model{
		frame: layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:  config.DefaultKeybindings(),
		listing: listing{workflows: []workflowstore.Workflow{{ID: "w1", Name: "acmecorp"}}, agents: []agentItem{
			{agent: agentstore.Agent{
				ID:         "a1",
				Name:       "waiting-on-reply",
				WorkflowID: "w1",
			}, unreadMail: true},
			{agent: agentstore.Agent{
				ID:         "a2",
				Name:       "caught-up",
				WorkflowID: "w1",
			}, unreadMail: false},
		}},
	}

	rows := strings.Split(m.agentsView(), "\n")
	waiting, ok := rowNamed(rows, "waiting-on-reply")
	require.True(t, ok)
	assert.Contains(t, waiting, unreadMailIcon)

	caughtUp, ok := rowNamed(rows, "caught-up")
	require.True(t, ok)
	assert.NotContains(t, caughtUp, unreadMailIcon)
}

func rowNamed(rows []string, name string) (string, bool) {
	for _, row := range rows {
		if strings.Contains(row, name) {
			return row, true
		}
	}

	return "", false
}

// TestFocusColours pins the accent marking which half of the sidebar has the keys, while the
// other draws grey throughout.
func TestFocusColours(t *testing.T) {
	m := detailsModel(&recordingTmux{})

	assert.Equal(t, paint.Colored.Color(paint.ToneAccent), m.titleStyle().GetForeground(), "the list has the keys to begin with")
	assert.Equal(t, paint.Colored.Color(paint.ToneAccent), m.rowCursorStyle().GetForeground())
	assert.Equal(t, paint.Colored.Color(paint.ToneMuted), paint.Grey.Header().GetForeground(), "the details title is grey under it")
	assert.Equal(t, paint.Colored.Color(paint.ToneMuted), paint.Grey.Cursor().GetForeground(), "and its cursor greys with it")

	m.details.focused = true

	assert.Equal(t, paint.Colored.Color(paint.ToneMuted), m.titleStyle().GetForeground(), "the accent goes with the keys")
	assert.Equal(t, paint.Colored.Color(paint.ToneMuted), m.rowCursorStyle().GetForeground())
	assert.Equal(t, paint.Colored.Color(paint.ToneAccent), paint.Colored.Header().GetForeground())
	assert.Equal(t, paint.Colored.Color(paint.ToneAccent), paint.Colored.Cursor().GetForeground())
}

// TestFocusKeepsTheMarkers covers what does not change with the focus: both cursors stay
// drawn, so which row a record is about, and which field the keys would copy, are on screen
// whichever half is being typed at.
func TestFocusKeepsTheMarkers(t *testing.T) {
	m := detailsModel(&recordingTmux{})

	assert.Contains(t, m.detailsView(), "›", "the field cursor is drawn while the list has the keys")

	m.details.focused = true

	assert.Contains(t, m.agentsView(), "›", "and the row cursor while the details have them")
}

// TestTheBlockBesideTheListGreysWithTheKeys covers the document drawn beside the list, which was
// in the panel's own colours whichever half of the sidebar was being read.
func TestTheBlockBesideTheListGreysWithTheKeys(t *testing.T) {
	m := detailsModel(&recordingTmux{})

	require.Equal(t, string(paint.Colored.Color(paint.ToneAccent)), *m.columnStyle().H1.Color, "the list has the keys to begin with")

	m.details.focused = true

	assert.Equal(t, string(paint.Colored.Color(paint.ToneMuted)), *m.columnStyle().H1.Color, "and the block greys when the record takes them")
}

// TestKeysOnAnotherPaneGreyThePanel covers the keyboard moving off the panel onto an agent, which
// leaves nothing on the panel being read.
func TestKeysOnAnotherPaneGreyThePanel(t *testing.T) {
	m := detailsModel(&recordingTmux{})

	require.Equal(t, paint.Colored.Color(paint.ToneAccent), m.titleStyle().GetForeground())

	m.keysElsewhere = true

	assert.Equal(t, paint.Colored.Color(paint.ToneMuted), m.titleStyle().GetForeground(), "the title greys with the keys")
	assert.Equal(t, string(paint.Colored.Color(paint.ToneMuted)), *m.columnStyle().H1.Color, "and so does the block beside the list")
}

// TestOnlyTheRegionHoldingTheKeysIsColoured covers the rule the whole palette exists for: one
// region of the panel draws in colour and the rest draw grey, whichever region that is.
func TestOnlyTheRegionHoldingTheKeysIsColoured(t *testing.T) {
	areas := []region{regionList, regionColumn, regionDetails, regionMessages, regionThread}

	m := detailsModel(&recordingTmux{})

	coloured := []region{}
	for _, area := range areas {
		if m.palette(area) == paint.Colored {
			coloured = append(coloured, area)
		}
	}
	require.Equal(t, []region{regionList, regionColumn, regionMessages}, coloured,
		"the list and the block beside it read as one region, and the alerts stand apart from both")

	m.details.focused = true

	assert.Equal(t, paint.Colored, m.palette(regionDetails), "the record takes the colour")
	assert.Equal(t, paint.Grey, m.palette(regionList), "and the list gives it up")
	assert.Equal(t, paint.Grey, m.palette(regionColumn))
}

// TestKeysOnAnotherPaneLeaveNoRegionColoured covers the keyboard moving onto an agent's own pane,
// which leaves no region of the panel being read.
func TestKeysOnAnotherPaneLeaveNoRegionColoured(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.keysElsewhere = true

	for _, area := range []region{regionList, regionColumn, regionDetails, regionThread} {
		assert.Equal(t, paint.Grey, m.palette(area), "region %d greys with the rest", area)
	}
}

// TestTheAlertsKeepTheirColour covers the region a refused write is reported in, which is there to
// be noticed rather than to say where the keys are.
func TestTheAlertsKeepTheirColour(t *testing.T) {
	m := detailsModel(&recordingTmux{})

	require.Equal(t, paint.Colored, m.palette(regionMessages), "coloured while the list has the keys")

	m.keysElsewhere = true

	assert.Equal(t, paint.Colored, m.palette(regionMessages), "and still coloured with the keys off the panel")
	assert.Equal(t, paint.Grey, m.palette(regionList), "while everything around it greys")
}

// TestRowMarksGreyWithTheirList covers the marks a row wears, which are drawn in the palette the
// list they sit in was handed rather than in colours of their own.
func TestRowMarksGreyWithTheirList(t *testing.T) {
	restore := lipgloss.ColorProfile()
	t.Cleanup(func() { lipgloss.SetColorProfile(restore) })
	lipgloss.SetColorProfile(termenv.TrueColor)

	assert.NotEqual(t, unviewedMark(paint.Colored, true), unviewedMark(paint.Grey, true),
		"the unread mark follows the list it is drawn in")
	assert.Equal(t, paint.Grey.Style(paint.ToneMuted).Render(stateIcon(agentstore.StateThinking)),
		stateMark(paint.Grey, agentstore.StateThinking),
		"a thinking agent is grey while the keys are elsewhere")
}
