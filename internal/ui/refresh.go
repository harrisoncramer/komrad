package ui

import (
	"context"
	"sync"
	"time"

	"weave/internal/client"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	agentv1 "weave/proto/gen/agent/v1"

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
	agentMarks           map[string]string
	workflowMarks        map[string]string
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
			result.agentMarks = marksFromAgents(listed)
		})
		wg.Go(func() {
			listed, err := client.ListAllWorkflows(ctx, clients.workflows)
			result.workflows, result.workflowsErr = workflowsFrom(listed), err
			result.workflowMarks = marksFromWorkflows(listed)
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

func (m *model) applyRefresh(result refreshResult) {
	if result.epoch < m.epoch {
		return
	}

	m.unreachable = result.err()

	treeRow := rowKeyAt(m.agentRows(), m.tree.cursor)
	archiveRow := rowKeyAt(m.archiveRows(), m.archive.cursor)

	if result.workflowsErr == nil {
		m.applyWorkflows(result.workflows)
	}
	if result.agentsErr == nil {
		m.applyAgents(result.agents)
	}
	if result.workflowsErr == nil && result.agentsErr == nil {
		m.applyMarks(result.agentMarks, result.workflowMarks)
	}
	if result.workflowsErr == nil || result.agentsErr == nil {
		m.placeTreeCursor(treeRow)
	}
	if result.schedulesErr == nil {
		m.applySchedules(result.schedules)
	}

	// Done here as well as on a resize, since an agent spawned since the last pass has a window of
	// its own that has never been put at the size it will be shown at.
	m.sizeAgentWindows()

	if result.archivedListed && result.archivedErr == nil && result.archivedWorkflowsErr == nil {
		m.applyArchive(result.archived, result.archivedWorkflows, archiveRow)
	}

	// Explainer: a form drawn over the list holds the stage as it was when it went up, and the
	// review draws a diff this pass knows nothing about.
	//
	// A screen that draws what it holds beside its own list stages nothing at all, since the pass
	// that changed the rows has already changed what is drawn from them.
	if spec := m.screen.spec(); spec.fromListing && spec.stage != nil && !m.form.isActive() {
		spec.stage(m)
	}
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
	written *written
	// epoch is the write this result belongs to, which is how a held form tells its own landing
	// from one an earlier submit is still waiting on.
	epoch int
}

type written struct {
	row     string
	pane    string
	harness string
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

// dwellRunsRead claims an agent's failed runs once its run log has been on screen long enough
// to have been read, on the same terms the transcript's own mark is claimed under.
func (m model) dwellRunsRead(now time.Time) (model, tea.Cmd) {
	agent, ok := m.runLogTarget()
	if !ok {
		m.runHover = hoverState{}

		return m, nil
	}

	// The row is tracked whether or not it is marked, so a run that fails while the pane is
	// already parked on the agent earns a look of its own rather than being claimed on arrival.
	if m.runHover.agentID != agent.ID {
		m.runHover = hoverState{agentID: agent.ID, since: now}

		// The pane follows the cursor, so the run it shows is worked out again for the agent it
		// has just reached rather than left at the position the last one opened on.
		if at := m.runAt(agent.ID, m.failedRunLog(agent.ID)); at != m.info.run {
			m.info.run = at
			m.stageInfo()
		}
	}

	if !m.isFailedRunShowing(agent.ID) {
		m.runHover.since = now

		return m, nil
	}

	if now.Sub(m.runHover.since) < viewedDwell {
		return m, nil
	}

	// A hidden panel restarts the dwell, so coming back to it is a fresh look rather than a
	// mark that was earned while nobody was watching.
	if !m.popup.IsShown() {
		m.runHover.since = now

		return m, nil
	}

	return m, m.markRunsReadCmd(agent.ID)
}

// runLogTarget is the agent whose runs the info pane is showing, which is none at all while
// that pane is showing anything else.
func (m model) runLogTarget() (agentstore.Agent, bool) {
	if m.info.kind != infoRunLog {
		return agentstore.Agent{}, false
	}

	return m.logTarget()
}

// isFailedRunShowing reports whether the agent the run-log pane is on is still carrying a mark,
// so a settled panel makes no calls at all.
func (m model) isFailedRunShowing(agentID string) bool {
	for _, item := range m.listing.agents {
		if item.agent.ID == agentID {
			return item.failedRun
		}
	}

	return false
}

// markRunsReadCmd claims an agent's failed runs without going through the write path, since the
// mark is a note about a row already on screen, and one that does not land is made again by the
// next tick that still finds the row marked.
func (m model) markRunsReadCmd(id string) tea.Cmd {
	clients := m.api

	return func() tea.Msg {
		_ = clients.markRunsRead(id) //nolint:errcheck // the next tick makes the call again

		return nil
	}
}

func (m *model) writeCmd(summary string, write func(api) error) tea.Cmd {
	clients := m.api
	epoch := m.beginWrite()

	return func() tea.Msg {
		return writeResult{summary: summary, err: write(clients), epoch: epoch}
	}
}

type resumeParams struct {
	summary string
	id      string
}

// rowWriteCmd sends a write whose landing takes the panel to the row it made, and stamps the
// result with the epoch so a held form knows whether the landing is the one it is waiting on.
func (m *model) rowWriteCmd(summary string, write func(api) (written, error)) tea.Cmd {
	clients := m.api
	epoch := m.beginWrite()

	return func() tea.Msg {
		row, err := write(clients)
		if err != nil {
			return writeResult{summary: summary, err: err, epoch: epoch}
		}

		return writeResult{summary: summary, written: &row, epoch: epoch}
	}
}

// agentWriteCmd is rowWriteCmd for a write answering with an agent, whose row is shown in the
// pane the agent is running in.
func (m *model) agentWriteCmd(summary string, write func(api) (*agentv1.Agent, error)) tea.Cmd {
	return m.rowWriteCmd(summary, func(clients api) (written, error) {
		agent, err := write(clients)
		if err != nil {
			return written{}, err
		}

		return written{row: agentRowKey(agent.GetId()), pane: agent.GetPaneId(), harness: agent.GetHarness()}, nil
	})
}

func (m *model) resumeCmd(params resumeParams) tea.Cmd {
	return m.agentWriteCmd(params.summary, func(clients api) (*agentv1.Agent, error) {
		return clients.resumeAgent(params.id)
	})
}

func (m *model) createCmd(params createAgentParams) tea.Cmd {
	return m.agentWriteCmd("Could not start the agent", func(clients api) (*agentv1.Agent, error) {
		return clients.createAgent(params)
	})
}

func (m *model) adoptCmd(params adoptParams) tea.Cmd {
	return m.agentWriteCmd("Could not adopt", func(clients api) (*agentv1.Agent, error) {
		return clients.adopt(params)
	})
}

func (m *model) createWorkflowCmd(cfg workflowstore.Config) tea.Cmd {
	return m.rowWriteCmd("Could not create the workflow", func(clients api) (written, error) {
		workflow, err := clients.createWorkflow(cfg)
		if err != nil {
			return written{}, err
		}

		return written{row: workflowRowKey(workflow.GetId())}, nil
	})
}

// beginWrite counts a write out and answers with the epoch it belongs to, which is what tells
// a landing result apart from one an earlier submit is still waiting on.
func (m *model) beginWrite() int {
	m.epoch++
	m.pending++

	return m.epoch
}

func (m model) applyWrite(result writeResult) (tea.Model, tea.Cmd) {
	m.pending--

	m.report(result.summary, result.err)

	_, walkedAway := m.detached[result.epoch]
	delete(m.detached, result.epoch)

	if result.written != nil && !walkedAway {
		m.enterScreen(screenAgents)

		m.tree.focus = result.written.row
		if result.written.pane != "" {
			// The cursor only catches up on the next listing, so the window is given up on the
			// strength of the pane itself rather than left zoomed over the thing being shown.
			if m.isPaneWarm(result.written.pane, result.written.harness) {
				m.frame.Solo(false)
				m.resize()
				m.frame.ShowGuest(result.written.pane)
			} else {
				m.holdForWarmup(result.written.pane, result.written.harness)
			}
		}
	}

	if m.held.owns(result.epoch) {
		// A form taken down now would leave the panel drawn over the whole window until the pane
		// it made turns up, so it is left standing and closed by the reveal instead.
		if result.err == nil && m.warm.pending != "" {
			m.warm.closesForm = true

			return m, m.refreshCmd()
		}

		m.held = heldWrite{}

		if result.err == nil {
			closed, cmd := m.closeForm()
			if next, ok := closed.(model); ok {
				m = next
			}

			return m, tea.Batch(cmd, m.refreshCmd())
		}

		m.banner.Hint = "fix and submit again"
	}

	// A refusal puts a banner above the form, which is chrome the form was not measured against
	// and which nothing else is going to tell it about.
	if m.form.isActive() {
		return m, tea.Batch(m.refreshCmd(), formResized())
	}

	return m, m.refreshCmd()
}
