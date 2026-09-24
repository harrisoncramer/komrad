package ui

import (
	"time"

	"weave/config"
	"weave/internal/ui/layout"
	"weave/internal/ui/panel"
)

// worktreesModel is the worktrees screen over the fixture listing, with the live agent on the
// agents list and the archived ones in the archive.
func worktreesModel() model {
	archived := time.Date(2026, 9, 17, 12, 0, 0, 0, time.Local)

	return model{
		frame:         layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:          config.DefaultKeybindings(),
		screen:        screenWorktrees,
		detailsHeight: config.DefaultDetailsHeight,
		listing: listing{
			workflows: []panel.Workflow{{ID: "w1", Name: "weave"}},
			agents: []panel.AgentRow{
				{
					Agent: panel.Agent{
						ID:         "live",
						Name:       "worktree-view",
						WorkflowID: "w1",
						Harness:    "claude",
						State:      panel.StateThinking,
						Cwd:        "/trees/live",
					},
					Alive: true,
				},
				{
					Agent: panel.Agent{
						ID:         "occ",
						Name:       "review-607",
						WorkflowID: "w1",
						Harness:    "claude",
						Cwd:        "/trees/live",
					},
					Alive: true,
				},
			},
			archivedAgents: archivedItems([]panel.Agent{
				{
					ID:         "archived",
					Name:       "review-column",
					WorkflowID: "w1",
					Harness:    "claude",
					ArchivedAt: &archived,
					Cwd:        "/trees/archived",
				},
				{
					ID:         "admin",
					Name:       "custom-cargo-fixes",
					WorkflowID: "w1",
					Harness:    "claude",
					ArchivedAt: &archived,
					Cwd:        "/trees/admin",
				},
			}),
			worktrees: worktreeFixture(),
		},
		worktrees: worktreesScreen{items: worktreeFixture()},
	}
}

// worktreesHeader is the worktrees header as the frame draws it.
func (m model) worktreesHeader() string {
	params := m.worktrees.listParams(m.shared())

	return m.tabbedHeader(params.title, params.tally)
}

// worktreesHelp is the worktrees legend as the frame draws it.
func (m model) worktreesHelp() keyMap {
	return m.listHelpOf(m.worktrees.listParams(m.shared()))
}
