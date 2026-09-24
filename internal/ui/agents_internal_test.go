package ui

import (
	"testing"
	"time"

	"weave/internal/ui/panel"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/require"
)

// agentHelp is the agents legend as the frame draws it.
func (m model) agentHelp() keyMap {
	return m.listHelp(m.agents.help(m.shared()))
}

// agentsHeader is the list's header as the frame draws it.
func (m model) agentsHeader() string {
	params := m.agents.listParams(m.shared())

	return m.tabbedHeader(params.title, params.tally)
}

func (m model) agentsListView() string {
	return m.screenListView(m.agents.listParams(m.shared()))
}

func detailsModel(tmux *recordingTmux) model {
	m := infoModel(tmux)
	m.height, m.width = 24, 40
	m.clip = &fakeClipboard{}
	m.listing.agents[0].Agent.ID = "a1"
	m.listing.agents[0].Agent.SessionID = "abc"
	m.listing.agents[0].Agent.Metadata = []panel.Metadata{
		{Key: "pr", Value: "https://github.com/o/r/pull/312"},
	}

	return m
}

func send(t *testing.T, m model, key string) model {
	t.Helper()

	next, _ := m.Update(keyMsg(key))
	shown, ok := next.(model)
	require.True(t, ok)

	return shown
}

func resize(m model, width, height int) tea.Model {
	next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})

	return next
}

// readWorktree is a directory read through git as the tests hand it to the record.
func readWorktree(now time.Time) worktreeInspection {
	return worktreeInspection{
		answered: true,
		readAt:   now,
		item: worktreeItem{
			dir:       "/repo",
			readable:  true,
			branch:    "agent/x",
			occupants: []worktreeAgent{{id: "a2", name: "reader"}},
			size:      &worktreeSize{bytes: 1200 * 1024 * 1024, measuredAt: now},
		},
		modified:   []string{"a.go", "b.go"},
		untracked:  []string{"notes.md"},
		baseRef:    "origin/main",
		ahead:      3,
		behind:     12,
		landed:     1,
		sequencing: true,
		commits: []worktreeCommit{
			{
				sha:         "87a3bcc98c47",
				subject:     "fix(view): drop the column",
				committedAt: now.Add(-2 * time.Hour),
			},
			{
				sha:         "1234567890ab",
				subject:     "feat(ui): a thing",
				committedAt: now.Add(-3 * time.Hour),
			},
		},
	}
}
