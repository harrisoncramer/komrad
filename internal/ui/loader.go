package ui

import (
	"slices"
	"time"

	"komrad/internal/loader"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	"komrad/internal/ui/submitting"
	"komrad/internal/view"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	tea "charm.land/bubbletea/v2"
)

type loaderTickMsg struct{}

func (m *model) loaderTick() tea.Cmd {
	return m.clock.After(loader.Interval, func(time.Time) tea.Msg { return loaderTickMsg{} })
}

type loadingState struct {
	style   loader.Style
	step    int
	ticking bool
}

func (l loadingState) frame() string {
	return l.style.Frame(l.step)
}

func (m *model) isLoading() bool {
	if len(m.writes.inflight) > 0 {
		return true
	}

	screen, ok := m.active().(loadingReporter)

	return ok && screen.isLoading(m, m.inputs())
}

func (a *agentsScreen) isLoading(m *model, in screen.Inputs) bool {
	return slices.ContainsFunc(a.rows(m, in), func(r agentRow) bool { return isRowLoading(in.Listing, r) })
}

func (r reviewScreen) isLoading(m *model, in screen.Inputs) bool {
	if r.loading {
		return true
	}

	item, ok := in.Listing.LiveAgent(r.agentID)

	return ok && isChecksRunning(in.Listing.Tasks.FindForAgent(item.Agent).PullRequest)
}

func isRowLoading(l panel.Listing, r agentRow) bool {
	if r.isTask() {
		writer, ok := r.writer()

		return isChecksRunning(r.task.PullRequest) || ok && isAgentLoading(writer)
	}
	if r.header {
		return r.orchestrator != nil && isThinking(*r.orchestrator)
	}

	return isChecksRunning(l.Tasks.FindForAgent(r.item.Agent).PullRequest) || isAgentLoading(r.item)
}

// isAgentLoading reports whether an agent is setting up or at work, either of which turns the loader.
func isAgentLoading(item panel.AgentRow) bool {
	return item.Agent.State == panel.StateSettingUp || isThinking(item)
}

// isThinking reports whether an agent is at work behind a pane still running, which turns the
// loader after its name.
func isThinking(item panel.AgentRow) bool {
	return snapshotProcess(item) == view.ProcessAlive && snapshotStatus(item) == view.StatusThinking
}

func isChecksRunning(standing panel.PullRequestStanding) bool {
	return standing.State == pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHECKS_RUNNING
}

func (m *model) spinLoader() tea.Cmd {
	if m.loading.ticking || !m.isLoading() {
		return nil
	}

	m.loading.ticking = true

	return m.loaderTick()
}

func (m *model) advanceLoader() tea.Cmd {
	m.loading.ticking = false
	if !m.isLoading() {
		return nil
	}

	m.loading.step++
	m.loading.ticking = true

	return m.loaderTick()
}

// heldWrite is the write the form on screen is held for.
func (m *model) heldWrite() (submitting.Write, bool) {
	if !m.writes.held.active {
		return submitting.Write{}, false
	}

	write, ok := m.writes.inflight[m.writes.held.write]

	return write, ok
}

// looseWrites are the writes in flight that no form on screen is held for.
func (m *model) looseWrites() []submitting.Write {
	loose := make([]submitting.Write, 0, len(m.writes.inflight))
	for id, write := range m.writes.inflight {
		if m.writes.held.owns(id) {
			continue
		}

		loose = append(loose, write)
	}

	return loose
}

func (m *model) submittingParams(width int) submitting.Params {
	return submitting.Params{
		Palette: paint.Panel,
		Frame:   m.loading.frame(),
		Now:     m.clock.Now(),
		Width:   width,
	}
}
