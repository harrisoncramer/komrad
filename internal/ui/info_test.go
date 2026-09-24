package ui

import (
	"testing"

	"weave/config"
	"weave/internal/gittest"
	"weave/internal/ui/layout"
	"weave/internal/ui/panel"

	"github.com/stretchr/testify/assert"
)

// TestInfoStaysShut pins that only the log key and a script's editor open the pane below
// the stage.
func TestInfoStaysShut(t *testing.T) {
	tmux := &recordingTmux{}
	m := infoModel(tmux)

	m.stageSelection()

	assert.Equal(t, infoNothing, m.info.kind)
	assert.Equal(t, "%9", m.frame.GuestPane(), "the hovered agent has the stage to itself")
	assert.Empty(t, tmux.belowAnchors, "nothing is split under it")
	assert.NotContains(t, tmux.respawnPanes, "%2", "and nothing is paged into a pane below")
}

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
		listing: listing{workflows: []panel.Workflow{{
			ID:       "w1",
			Name:     "backend",
			Worktree: panel.WorktreeConfig{Enabled: true},
		}}, agents: []panel.AgentRow{
			{Agent: panel.Agent{
				Name:       "auth",
				Pane:       "%9",
				Cwd:        "/repo",
				WorkflowID: "w1",
			}, Alive: true},
		}}, agents: agentsScreen{rowCursor: rowCursor{cursor: 1}},
	}
}
