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

type loaderScreen interface {
	isLoading(s panelShared) bool
}

func (m *model) isLoading() bool {
	screen, ok := m.active().(loaderScreen)

	return ok && screen.isLoading(m.shared())
}

func (a *agentsScreen) isLoading(s panelShared) bool {
	return slices.ContainsFunc(a.rows(s), isRowLoading)
}

func (a *archiveScreen) isLoading(s panelShared) bool {
	return slices.ContainsFunc(a.listRows(s), isRowLoading)
}

func (r reviewScreen) isLoading(s panelShared) bool {
	item, ok := s.listing.liveAgent(r.agentID)

	return ok && isChecksRunning(item)
}

func isRowLoading(r agentRow) bool {
	return !r.header && isChecksRunning(r.item)
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
