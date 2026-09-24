package ui

import (
	"weave/config"
	"weave/internal/gittest"
	"weave/internal/ui/layout"
	"weave/internal/ui/panel"
)

func infoModel(tmux *recordingTmux) model {
	frame := layout.New(tmux, config.DefaultLayout())
	frame.Attach("%0")

	return model{
		tmux:          tmux,
		git:           gittest.NoRepo{},
		frame:         frame,
		shell:         tmux.GetDefaultShell(),
		keys:          config.DefaultKeybindings(),
		screen:        screenAgents,
		detailsHeight: config.DefaultDetailsHeight,
		listing: listing{
			workflows: []panel.Workflow{{
				ID:       "w1",
				Name:     "backend",
				Worktree: panel.WorktreeConfig{Enabled: true},
			}},
			agents: []panel.AgentRow{{
				Agent: panel.Agent{
					Name:       "auth",
					Pane:       "%9",
					Cwd:        "/repo",
					WorkflowID: "w1",
				},
				Alive: true,
			}},
		},
		agents: agentsScreen{rowCursor: rowCursor{cursor: 1}},
	}
}
