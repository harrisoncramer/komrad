package ui

import (
	"context"
	"sync"
	"time"

	"weave/internal/client"
	"weave/internal/errui"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	refreshInterval = time.Second
	agentsTimeout   = 2 * time.Second
)

type refreshTickMsg struct {
	at time.Time
}

func refreshTick() tea.Cmd {
	return tea.Tick(refreshInterval, func(at time.Time) tea.Msg { return refreshTickMsg{at: at} })
}

// viewedDwell is how long the cursor has to stay on one agent before the panel counts the pane
// it stages as read, which is what keeps the rows a person travels over from being cleared.
const viewedDwell = time.Second

// hoverState is the agent the cursor is sitting on, when it arrived, and whether the person has
// set that row's mark by hand and does not want the dwell moving it back.
type hoverState struct {
	agentID string
	since   time.Time
	held    bool
}

// dwellViewed claims the hovered agent once the cursor has been on it long enough to have read
// what it said, counting only a live row, since a dormant one stages a placeholder rather than
// a pane, and writing only an unviewed one, so a settled list makes no calls at all.
func (m model) dwellViewed(now time.Time) (model, tea.Cmd) {
	item, ok := m.dwelledAgent()
	if !ok {
		m.hover = hoverState{}

		return m, nil
	}

	// The row is tracked whether or not it is unviewed, so that a mark set by hand survives its
	// own write landing and is let go of only when the cursor moves somewhere else.
	if m.hover.agentID != item.agent.ID {
		m.hover = hoverState{agentID: item.agent.ID, since: now}
	}

	// A row with nothing to read keeps restarting the dwell, so the turn that lands next has to
	// be looked at for a second of its own rather than being claimed on arrival by a cursor
	// that was parked there through the turn before it.
	if !item.unviewed {
		m.hover.since = now

		return m, nil
	}

	if m.hover.held {
		return m, nil
	}

	if now.Sub(m.hover.since) < viewedDwell {
		return m, nil
	}

	// Asked only here rather than every tick, since this is the one moment the answer changes
	// anything, and a hidden panel restarts the dwell so coming back to it is a fresh look
	// rather than a mark that was earned while nobody was watching.
	if !m.popup.IsShown() {
		m.hover.since = now

		return m, nil
	}

	return m, m.markViewedCmd(item.agent.ID)
}

func (m model) dwelledAgent() (agentItem, bool) {
	if m.screen != screenAgents || m.form.form != nil {
		return agentItem{}, false
	}

	item, ok := m.selectedAgent()
	if !ok || !item.alive {
		return agentItem{}, false
	}

	return item, true
}

type refreshResult struct {
	epoch                int
	agents               []agentItem
	agentsErr            error
	workflows            []workflowstore.Workflow
	workflowsErr         error
	schedules            []scheduleItem
	schedulesErr         error
	archived             []agentstore.Agent
	archivedErr          error
	archivedWorkflows    []workflowstore.Workflow
	archivedWorkflowsErr error
	archivedListed       bool
}

func (m model) refreshCmd() tea.Cmd {
	if m.pending > 0 {
		return nil
	}

	clients, epoch, withArchive := m.api, m.epoch, m.screen.spec().needsArchive

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), agentsTimeout)
		defer cancel()

		result := refreshResult{epoch: epoch, archivedListed: withArchive}

		var wg sync.WaitGroup
		wg.Go(func() {
			listed, err := client.ListAllAgents(ctx, clients.agents)
			result.agents, result.agentsErr = agentItemsFrom(listed), err
		})
		wg.Go(func() {
			listed, err := client.ListAllWorkflows(ctx, clients.workflows)
			result.workflows, result.workflowsErr = workflowsFrom(listed), err
		})
		wg.Go(func() {
			listed, err := client.ListAllSchedules(ctx, clients.schedules)
			result.schedules, result.schedulesErr = scheduleItemsFrom(listed), err
		})
		if withArchive {
			wg.Go(func() {
				listed, err := client.ListAllArchivedAgents(ctx, clients.agents)
				result.archived, result.archivedErr = archivedAgentsFrom(listed), err
			})
			wg.Go(func() {
				listed, err := client.ListAllArchivedWorkflows(ctx, clients.workflows)
				result.archivedWorkflows, result.archivedWorkflowsErr = workflowsFrom(listed), err
			})
		}
		wg.Wait()

		return result
	}
}

func (m *model) applyRefresh(result refreshResult) bool {
	if result.epoch < m.epoch {
		return false
	}

	m.unreachable = result.err()

	editorClosed := m.reconcileInfo()

	treeRow := rowKeyAt(m.agentRows(), m.tree.cursor)
	archiveRow := rowKeyAt(m.archiveRows(), m.archive.cursor)

	if result.workflowsErr == nil {
		m.applyWorkflows(result.workflows)
	}
	if result.agentsErr == nil {
		m.applyAgents(result.agents)
	}
	if result.workflowsErr == nil || result.agentsErr == nil {
		m.placeTreeCursor(treeRow)
	}
	if result.schedulesErr == nil {
		m.applySchedules(result.schedules)
	}

	if result.archivedListed && result.archivedErr == nil && result.archivedWorkflowsErr == nil {
		m.applyArchive(result.archived, result.archivedWorkflows, archiveRow)
	}

	// A form drawn over the list holds the stage as it was when it went up, and the review
	// draws a diff this pass knows nothing about.
	if spec := m.screen.spec(); spec.fromListing && !m.form.active() {
		spec.stage(m)
	}

	return editorClosed && m.screen == screenReview
}

func (r refreshResult) err() error {
	switch {
	case r.agentsErr != nil:
		return r.agentsErr
	case r.workflowsErr != nil:
		return r.workflowsErr
	case r.schedulesErr != nil:
		return r.schedulesErr
	case r.archivedErr != nil:
		return r.archivedErr
	default:
		return r.archivedWorkflowsErr
	}
}

type writeResult struct {
	summary   string
	err       error
	written   *written
	setupPane bool
}

type written struct {
	row  string
	pane string
}

// markViewedCmd claims what an agent has finished without going through the write path, since
// the mark is a note about a row already on screen rather than a change the listing has to show,
// and one that does not land is made again by the next tick that still finds the row unviewed.
func (m model) markViewedCmd(id string) tea.Cmd {
	clients := m.api

	return func() tea.Msg {
		_ = clients.setViewed(id, true) //nolint:errcheck // the next tick makes the mark again

		return nil
	}
}

// markRunsReadCmd claims the failed runs of the agent whose run log is on screen, on the same
// terms the transcript's own mark is claimed: what the person is looking at counts as read, and
// a call that does not land is made again by the next tick that still finds the row marked.
func (m model) markRunsReadCmd() tea.Cmd {
	if m.info.kind != infoRunLog {
		return nil
	}

	agent, ok := m.logTarget()
	if !ok || !m.failedRunShowing(agent.ID) {
		return nil
	}

	clients := m.api
	id := agent.ID

	return func() tea.Msg {
		_ = clients.markRunsRead(id) //nolint:errcheck // the next tick makes the call again

		return nil
	}
}

// failedRunShowing reports whether the agent the run-log pane is on is still carrying a mark,
// so a settled panel makes no calls at all.
func (m model) failedRunShowing(agentID string) bool {
	for _, item := range m.listing.agents {
		if item.agent.ID == agentID {
			return item.failedRun
		}
	}

	return false
}

func (m *model) writeCmd(summary string, write func(api) error) tea.Cmd {
	clients := m.api
	m.beginWrite()

	return func() tea.Msg {
		return writeResult{summary: summary, err: write(clients)}
	}
}

type resumeParams struct {
	summary string
	id      string
}

func (m *model) resumeCmd(params resumeParams) tea.Cmd {
	clients := m.api
	m.beginWrite()

	return func() tea.Msg {
		agent, err := clients.resumeAgent(params.id)
		if err != nil {
			return writeResult{summary: params.summary, err: err}
		}

		return writeResult{
			summary: params.summary,
			written: &written{row: agentRowKey(agent.GetId()), pane: agent.GetPaneId()},
		}
	}
}

func (m *model) createCmd(params createAgentParams) tea.Cmd {
	clients := m.api
	m.beginWrite()

	return func() tea.Msg {
		agent, err := clients.createAgent(params)
		if err != nil {
			return writeResult{
				summary:   "Could not start the agent",
				err:       err,
				setupPane: params.SetupPane != "",
			}
		}

		return writeResult{
			summary:   "Could not start the agent",
			written:   &written{row: agentRowKey(agent.GetId()), pane: agent.GetPaneId()},
			setupPane: params.SetupPane != "",
		}
	}
}

func (m *model) adoptCmd(params adoptParams) tea.Cmd {
	clients := m.api
	m.beginWrite()

	return func() tea.Msg {
		const summary = "Could not adopt"

		agent, err := clients.adopt(params)
		if err != nil {
			return writeResult{
				summary:   summary,
				err:       err,
				setupPane: params.SetupPane != "",
			}
		}

		return writeResult{
			summary:   summary,
			written:   &written{row: agentRowKey(agent.GetId()), pane: agent.GetPaneId()},
			setupPane: params.SetupPane != "",
		}
	}
}

func (m *model) createWorkflowCmd(cfg workflowstore.Config) tea.Cmd {
	clients := m.api
	m.beginWrite()

	return func() tea.Msg {
		const summary = "Could not create the workflow"

		workflow, err := clients.createWorkflow(cfg)
		if err != nil {
			return writeResult{summary: summary, err: err}
		}

		return writeResult{
			summary: summary,
			written: &written{row: workflowRowKey(workflow.GetId())},
		}
	}
}

func (m *model) beginWrite() {
	m.epoch++
	m.pending++
}

func (m model) applyWrite(result writeResult) (tea.Model, tea.Cmd) {
	m.pending--

	// Only a script that ran leaves its reason in the pane, and every other failure is left
	// unexplained if the banner stays quiet on the strength of the pane being open.
	setupRan := result.setupPane && errui.SetupRan(result.err)
	if setupRan {
		m.reportBrief(result.summary, "the setup failed, and its output is in the pane it ran in", result.err)
	} else {
		m.report(result.summary, result.err)
	}

	if result.written != nil {
		m.enterScreen(screenAgents)

		m.tree.focus = result.written.row
		if result.written.pane != "" {
			m.frame.ShowGuest(result.written.pane)
		}
	}

	if result.setupPane {
		m.setupPending = false

		if m.info.kind == infoSetup && !setupRan {
			m = m.closeInfo()
		}
	}

	if m.pendingForm {
		m.pendingForm = false

		if result.err == nil {
			closed, cmd := m.closeForm()
			if next, ok := closed.(model); ok {
				m = next
			}

			return m, tea.Batch(cmd, m.refreshCmd())
		}

		m.banner.Hint = "fix and submit again"
	}

	return m, m.refreshCmd()
}
