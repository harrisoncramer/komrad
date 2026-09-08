package ui

import (
	"strings"
	"testing"

	"weave/config"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/layout"
	agentv1 "weave/proto/gen/agent/v1"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHelpToggle covers the help key, which is the one binding that changes nothing but
// how much of the help is drawn.
func TestHelpToggle(t *testing.T) {
	m := helpModel()

	view := m.agentsView()
	assert.NotContains(t, view, "archive", "the help opens on its one line, dropping what the row also does")
	assert.Contains(t, view, "open", "it keeps what the row does first")
	assert.Contains(t, view, "show help", "and says the key that brings the rest out")

	next, _ := m.route(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	expanded, ok := next.(model)
	require.True(t, ok)
	require.True(t, expanded.helpExpanded)

	view = expanded.agentsView()
	assert.Contains(t, view, "archive", "the expanded help names every binding")
	assert.Contains(t, view, "hide help", "and says the key that puts it away")

	assert.Greater(t, m.listLines(m.agentsView()), expanded.listLines(expanded.agentsView()),
		"the rows the help takes are the list's")

	back, _ := expanded.route(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	collapsed, ok := back.(model)
	require.True(t, ok)
	assert.False(t, collapsed.helpExpanded, "the key toggles rather than only expanding")
}

// TestHelpToggleOnSearch pins that a question mark typed into the search query is a character
// to search for rather than the key that draws the legend.
func TestHelpToggleOnSearch(t *testing.T) {
	m := helpModel()
	m.screen = screenSearch
	m.search.typing = true

	next, _ := m.route(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	searched, ok := next.(model)
	require.True(t, ok)

	assert.False(t, searched.helpExpanded, "the help is left as it was")
	assert.Equal(t, "?", searched.search.query, "the key is typed instead")
}

// TestHelpColumnsFollowTheWidth pins that a sidebar wide enough for two columns draws the
// same bindings in half the lines.
func TestHelpColumnsFollowTheWidth(t *testing.T) {
	narrow := helpModel()
	narrow.helpExpanded = true

	wide := narrow
	wide.width = 74

	bindings := byLabel(narrow.agentHelp().full)
	require.Len(t, helpColumns(bindings, narrow.width), 1, "a narrow sidebar holds one column")
	require.Len(t, helpColumns(bindings, wide.width), 2, "a wider one holds two")

	narrowHelp := ansi.Strip(narrow.renderHelp(narrow.agentHelp()))
	wideHelp := ansi.Strip(wide.renderHelp(wide.agentHelp()))

	assert.Equal(t, len(bindings), strings.Count(narrowHelp, "\n"), "one binding a line down the one column")
	assert.Less(t, strings.Count(wideHelp, "\n"), strings.Count(narrowHelp, "\n"), "two columns are shorter")
	for _, binding := range bindings {
		assert.Contains(t, wideHelp, binding.Help().Desc, "and drop nothing")
	}
}

// TestNoLegendIsDrawnAsAnEllipsis draws every screen's expanded legend at the narrowest sidebar a
// configured split leaves, which is the ordinary case rather than an edge, since a width nobody
// has dragged is widened to nameWidth and a dragged one is the reader's own doing.
func TestNoLegendIsDrawnAsAnEllipsis(t *testing.T) {
	legends := map[screen]func(model) keyMap{
		screenAgents:    model.agentHelp,
		screenSchedules: model.scheduleHelp,
		screenArchive:   model.archiveHelp,
		screenSearch:    model.searchHelp,
		screenMail:      model.mailHelp,
		screenReview:    model.reviewHelp,
	}

	for s, legend := range legends {
		m := helpModel()
		m.screen = s
		m.helpExpanded = true
		m = m.at(nameWidth)

		k := legend(m)
		drawn := ansi.Strip(m.renderHelp(k))

		assert.NotEqual(t, "…", strings.TrimSpace(drawn), "screen %d draws its legend as an ellipsis", s)

		// Checked by the key rather than by what it is described as, since a refused expansion
		// re-labels the legend's own key: the claim is that every key the short form names is
		// findable, not that a particular wording survived.
		for _, binding := range byLabel(k.short) {
			assert.Contains(t, drawn, binding.Help().Key,
				"screen %d loses the key for %q at %d columns", s, binding.Help().Desc, nameWidth)
		}
	}
}

// TestARefusedExpansionSaysItShowedNothing covers the key on a screen whose expanded legend will
// not fit: it draws the short form, which is the right answer, but helpExpanded is true underneath
// and the row would otherwise claim to be hiding help it never showed, so the key reads as broken
// rather than as refused.
func TestARefusedExpansionSaysItShowedNothing(t *testing.T) {
	m := helpModel()
	m.helpExpanded = true
	m = m.at(nameWidth)

	drawn := ansi.Strip(m.renderHelp(m.agentHelp()))

	require.NotContains(t, drawn, "move", "the agents legend does not fit a sidebar this narrow")
	assert.Contains(t, drawn, "show help", "so the key says it has help left to show")
	assert.NotContains(t, drawn, "hide help", "rather than claiming to have hidden any")
}

func helpModel() model {
	return model{
		keys:    config.DefaultKeybindings(),
		frame:   layout.New(&recordingTmux{}, config.DefaultLayout()),
		height:  24,
		width:   46,
		listing: listing{workflows: []workflowstore.Workflow{{ID: "w1", Name: "everything"}}, agents: []agentItem{{agent: agentstore.Agent{Name: "api-refactor", WorkflowID: "w1"}}}}, tree: treeState{rowCursor: rowCursor{cursor: 1}},
	}
}

// TestNoScreenBindsOneKeyTwice pins that every legend a screen draws names each key once, since
// a key two actions claim reaches whichever one the update function tries first and leaves the
// other looking broken.
func TestNoScreenBindsOneKeyTwice(t *testing.T) {
	base := helpModel()
	base.mail.inbox = []*agentv1.Mail{{Id: "m1"}}
	base.schedules.items = []scheduleItem{{}}

	tests := []struct {
		name   string
		screen screen
		legend func(model) keyMap
	}{
		{name: "the agents tree", screen: screenAgents, legend: model.agentHelp},
		{name: "the archive", screen: screenArchive, legend: model.archiveHelp},
		{name: "the schedules", screen: screenSchedules, legend: model.scheduleHelp},
		{name: "the mailbox", screen: screenMail, legend: model.mailHelp},
		{name: "the search", screen: screenSearch, legend: model.searchHelp},
		{name: "the review", screen: screenReview, legend: model.reviewHelp},
		{name: "the details under a list", screen: screenAgents, legend: model.detailsHelp},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := base
			m.screen = tt.screen

			held := map[string]string{}
			for _, b := range tt.legend(m).full {
				for _, k := range b.Keys() {
					desc := b.Help().Desc
					if was, taken := held[k]; taken {
						assert.Failf(t, "one key, two actions",
							"%q is bound to both %q and %q", k, was, desc)

						continue
					}
					held[k] = desc
				}
			}
		})
	}
}
