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

type refreshTickMsg struct{}

func refreshTick() tea.Cmd {
	return tea.Tick(refreshInterval, func(time.Time) tea.Msg { return refreshTickMsg{} })
}

type refreshResult struct {
	epoch int

	agents    []agentItem
	agentsErr error

	workflows    []workflowstore.Workflow
	workflowsErr error

	schedules    []scheduleItem
	schedulesErr error

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
	summary string
	err     error

	written   *written
	setupPane bool
}

type written struct {
	row  string
	pane string
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

	// Only an action that ran leaves its reason in the pane, and every other failure is left
	// unexplained if the banner stays quiet on the strength of the pane being open.
	setupRan := result.setupPane && errui.SetupRan(result.err)
	if setupRan {
		m.reportBrief(result.summary, "the setup action failed, and its output is in the pane it ran in", result.err)
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
