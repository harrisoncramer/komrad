package ui

import (
	"fmt"
	"testing"
	"time"

	"weave/config"
	agentstore "weave/internal/store/agent"
	schedulestore "weave/internal/store/schedule"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/layout"

	tea "github.com/charmbracelet/bubbletea"
)

// loadedModel is every listing at the size a panel used for months holds.
func loadedModel() model {
	const (
		liveAgents     = 27
		archivedAgents = 630
		worktrees      = 307
		schedules      = 4
		workflows      = 10
	)

	tmux := &recordingTmux{}
	m := model{
		tmux:          tmux,
		popup:         NewPopupWindow(tmux, "test-session"),
		frame:         layout.New(tmux, config.DefaultLayout()),
		keys:          config.DefaultKeybindings(),
		width:         200,
		height:        50,
		detailsHeight: config.DefaultDetailsHeight,
		drawn:         &lastFrame{},
	}
	m.frame.Attach("%0")

	base := time.Date(2026, time.June, 1, 9, 0, 0, 0, time.UTC)
	for i := range workflows {
		id := fmt.Sprintf("w%d", i)
		if i < 2 {
			m.archive.workflows = append(m.archive.workflows, archivedWorkflow(id, "workflow "+id))

			continue
		}
		m.listing.workflows = append(m.listing.workflows, workflowstore.Workflow{ID: id, Name: "workflow " + id})
	}
	for i := range liveAgents {
		id := fmt.Sprintf("l%02d", i)
		m.listing.agents = append(m.listing.agents, agentItem{
			agent: agentstore.Agent{
				ID:         id,
				Name:       fmt.Sprintf("Live-%d-%s", i%7, id),
				WorkflowID: fmt.Sprintf("w%d", 2+i%8),
				State:      agentstore.StateThinking,
				CreatedAt:  base.Add(time.Duration(i) * time.Minute),
			},
			alive: true,
		})
	}
	for i := range archivedAgents {
		id := fmt.Sprintf("a%04d", i)
		archived := base.Add(time.Duration(i) * time.Hour)
		agent := agentstore.Agent{
			ID:         id,
			Name:       fmt.Sprintf("Agent-%d-%s", i%37, id),
			WorkflowID: fmt.Sprintf("w%d", i%5),
			State:      agentstore.StateIdle,
			CreatedAt:  base.Add(time.Duration(i) * time.Minute),
			ArchivedAt: &archived,
		}
		if i%7 == 3 {
			parent := fmt.Sprintf("a%04d", i-1)
			agent.ParentAgentID = &parent
		}
		m.archive.agents = append(m.archive.agents, agent)
	}
	for i := range worktrees {
		id := fmt.Sprintf("t%03d", i)
		item := worktreeItem{
			dir:          "/home/h/.local/state/weave/worktrees/" + id,
			repoDir:      fmt.Sprintf("/home/h/repo%d", i%3),
			branch:       "agent/" + id,
			readable:     true,
			size:         &worktreeSize{bytes: int64(i) * 1024 * 1024, measuredAt: base},
			lastActiveAt: base.Add(time.Duration(i) * time.Hour),
		}
		if i%2 == 0 {
			item.agent = &worktreeAgent{id: fmt.Sprintf("a%04d", i), name: fmt.Sprintf("Agent-%d", i)}
		}
		m.worktrees.items = append(m.worktrees.items, item)
	}
	for i := range schedules {
		m.schedules.items = append(m.schedules.items, scheduleItem{schedule: schedulestore.Schedule{
			ID:   fmt.Sprintf("s%d", i),
			Name: fmt.Sprintf("schedule %d", i),
		}})
	}
	m.search.query = "agent"
	m.search.turns = hits("a0001", "a0002", "l01", "l02", "a0100", "a0200", "a0300", "a0400", "a0500", "a0600")

	m.tree.cursor, m.archive.cursor, m.worktrees.cursor, m.schedules.cursor, m.search.cursor = 1, 1, 1, 1, 1

	return m
}

// BenchmarkKeypress is one cursor move on each list screen, the update and the frame drawn after
// it, over the loaded listings.
func BenchmarkKeypress(b *testing.B) {
	down := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}

	tests := []struct {
		name   string
		screen screen
		solo   bool
	}{
		{
			name:   "agents",
			screen: screenAgents,
			solo:   false,
		},
		{
			name:   "archive",
			screen: screenArchive,
			solo:   true,
		},
		{
			name:   "worktrees",
			screen: screenWorktrees,
			solo:   true,
		},
		{
			name:   "schedules",
			screen: screenSchedules,
			solo:   true,
		},
		{
			name:   "search",
			screen: screenSearch,
			solo:   true,
		},
	}

	for _, tt := range tests {
		b.Run(tt.name, func(b *testing.B) {
			m := loadedModel()
			m.screen = tt.screen
			m.frame.Solo(tt.solo)

			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				next, _ := m.Update(down)
				m = next.(model)
				_ = m.View()
			}
		})
	}
}
