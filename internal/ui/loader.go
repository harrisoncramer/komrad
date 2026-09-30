package ui

import (
	"slices"
	"time"

	"komrad/internal/loader"
	"komrad/internal/ui/panel"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	tea "charm.land/bubbletea/v2"
)

type loaderTickMsg struct{}

func loaderTick() tea.Cmd {
	return startTimer(loader.Interval, func(time.Time) tea.Msg { return loaderTickMsg{} })
}

type loadingState struct {
	style   loader.Style
	step    int
	ticking bool
}

func (l loadingState) frame() string {
	return l.style.Frame(l.step)
}

func (s screen) drawsLoaders() bool {
	switch s {
	case screenAgents, screenArchive, screenReview:
		return true
	case screenSchedules, screenSearch, screenWorktrees, screenDocuments, screenLogs:
		return false
	}

	return false
}

func (m model) isLoading() bool {
	if !m.screen.drawsLoaders() {
		return false
	}

	rows := m.listing.agents
	if m.screen == screenArchive {
		rows = m.listing.archivedAgents
	}

	return slices.ContainsFunc(rows, isChecksRunning)
}

func isChecksRunning(row panel.AgentRow) bool {
	return row.PullRequest.State == pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHECKS_RUNNING
}

func (m *model) spinLoader() tea.Cmd {
	if m.loading.ticking || !m.isLoading() {
		return nil
	}

	m.loading.ticking = true

	return loaderTick()
}

func (m *model) advanceLoader() tea.Cmd {
	m.loading.ticking = false
	if !m.isLoading() {
		return nil
	}

	m.loading.step++
	m.loading.ticking = true

	return loaderTick()
}
