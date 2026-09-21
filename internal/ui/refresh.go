package ui

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"weave/internal/client"
	"weave/internal/errui"

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

// viewedDwell is how long a failed run's log has to be on screen before the panel counts it as read.
const viewedDwell = time.Second

// hoverState is the agent the cursor is sitting on and when it arrived.
type hoverState struct {
	agentID string
	since   time.Time
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
	worktrees            []worktreeItem
	worktreesErr         error
	worktreesListed      bool
	agentMarks           map[string]string
	workflowMarks        map[string]string
}

func (m model) refreshCmd() tea.Cmd {
	if m.pending > 0 {
		return nil
	}

	spec := m.screen.spec()
	clients, epoch, withArchive, withWorktrees := m.api, m.epoch, spec.needsArchive, spec.needsWorktrees

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), agentsTimeout)
		defer cancel()

		result := refreshResult{
			epoch:           epoch,
			archivedListed:  withArchive,
			worktreesListed: withWorktrees,
		}

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
		if withWorktrees {
			wg.Go(func() {
				listed, err := clients.listWorktrees()
				if err == nil {
					result.worktrees = worktreeItemsFrom(listed)
				}
				result.worktreesErr = err
			})
		}
		wg.Wait()

		return result
	}
}

// reportUnreachable says the server has stopped answering on the pass that finds it gone.
func (m *model) reportUnreachable(err error) {
	was := m.unreachable
	m.unreachable = err

	if err == nil || was != nil {
		return
	}

	reason := errui.GetReason(err)
	if m.addr != "" && !strings.Contains(reason, m.addr) {
		reason = m.addr + ": " + reason
	}

	m.reportBrief("Cannot reach the server", reason, err)
	m.messages.setNewestHint("Start it with: weave serve")
}

func (m *model) applyRefresh(result refreshResult) tea.Cmd {
	if result.epoch < m.epoch {
		return nil
	}

	m.reportUnreachable(result.err())

	treeRow := rowKeyAt(m.agentRows(), m.tree.cursor)
	archiveRow := rowKeyAt(m.archiveRows(), m.archive.cursor)
	worktreeRow := rowKeyAt(m.worktreeRows(), m.worktrees.cursor)
	searchRow := m.hoveredSearchID()

	before := m.listing

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

	m.sizeAgentWindows()

	if result.archivedListed && result.archivedErr == nil && result.archivedWorkflowsErr == nil {
		m.applyArchive(result.archived, result.archivedWorkflows, archiveRow)
	}
	if result.worktreesListed && result.worktreesErr == nil {
		m.applyWorktrees(result.worktrees, worktreeRow)
	}

	preview := m.placeSearchAgent(searchRow)
	inspect := tea.Batch(m.inspectHoveredWorktreeCmd(), m.inspectAgentWorktreeCmd(time.Now()))
	pullRequests := m.listHoveredPullRequestsCmd()

	// A form draws its options from the listing it was built over, so it is drawn again when
	// that listing moves under it.
	var redrawn tea.Cmd
	if !reflect.DeepEqual(before.workflows, m.listing.workflows) {
		redrawn = m.redrawOpenForm()
	}

	// A screen that draws what it holds beside its own list stages nothing at all.
	if spec := m.screen.spec(); spec.fromListing && spec.stage != nil && !m.form.isActive() {
		spec.stage(m)
	}

	return tea.Batch(preview, redrawn, inspect, pullRequests)
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
	case r.archivedWorkflowsErr != nil:
		return r.archivedWorkflowsErr
	default:
		return r.worktreesErr
	}
}

type writeResult struct {
	summary string
	err     error
	written *written
	// epoch is the write this result belongs to.
	epoch int
	// released is the agent a landed release let go.
	released *released
	// queued is what to say once a write that queues a background job has landed.
	queued *message
}

type released struct {
	id   string
	name string
	// files is the files a reader had open over the agent.
	files []string
	// silent says the release named none of them.
	silent bool
}

type written struct {
	row     string
	pane    string
	harness string
	// focus says the pane the write made is to be handed the keyboard once it is shown.
	focus bool
}

// dwellRunsRead claims an agent's failed runs once its run log has been on screen long enough to
// have been read.
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

// runLogTarget is the agent whose runs the info pane is showing, and none while that pane is
// showing anything else.
func (m model) runLogTarget() (agentstore.Agent, bool) {
	if m.info.kind != infoRunLog {
		return agentstore.Agent{}, false
	}

	return m.logTarget()
}

// isFailedRunShowing reports whether the agent the run-log pane is on is still carrying a mark.
func (m model) isFailedRunShowing(agentID string) bool {
	for _, item := range m.listing.agents {
		if item.agent.ID == agentID {
			return item.failedRun
		}
	}

	return false
}

// markRunsReadCmd claims an agent's failed runs without going through the write path.
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
		return writeResult{
			summary: summary,
			err:     write(clients),
			epoch:   epoch,
		}
	}
}

// releaseCmd lets an agent go, naming the files a reader had open over it.
func (m *model) releaseCmd(summary string, agent agentstore.Agent, said string) tea.Cmd {
	let := released{
		id:    agent.ID,
		name:  agent.Name,
		files: slices.Clone(m.edited[agent.ID]),
	}

	return stampRelease(m.writeCmd(summary, func(a api) error {
		return a.release(agent, said+touchedSaid(let.files))
	}), let)
}

// releaseInSilenceCmd lets an agent go without typing at it.
func (m *model) releaseInSilenceCmd(summary string, agent agentstore.Agent) tea.Cmd {
	let := released{
		id:     agent.ID,
		name:   agent.Name,
		files:  slices.Clone(m.edited[agent.ID]),
		silent: true,
	}

	return stampRelease(m.writeCmd(summary, func(a api) error {
		return a.release(agent, "")
	}), let)
}

// queuedCmd runs a write that queues a background job and says so once it has landed.
func (m *model) queuedCmd(summary string, queued message, write func(api) error) tea.Cmd {
	cmd := m.writeCmd(summary, write)

	return func() tea.Msg {
		msg := cmd()

		result, ok := msg.(writeResult)
		if !ok {
			return msg
		}

		result.queued = &queued

		return result
	}
}

// stampRelease says on a write's result which agent it let go and which files it carried.
func stampRelease(cmd tea.Cmd, let released) tea.Cmd {
	return func() tea.Msg {
		msg := cmd()

		result, ok := msg.(writeResult)
		if !ok {
			return msg
		}

		result.released = &let

		return result
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
			return writeResult{
				summary: summary,
				err:     err,
				epoch:   epoch,
			}
		}

		return writeResult{
			summary: summary,
			written: &row,
			epoch:   epoch,
		}
	}
}

// agentWriteCmd is rowWriteCmd for a write answering with an agent, whose row is shown in the
// pane the agent is running in.
func (m *model) agentWriteCmd(summary string, write func(api) (*agentv1.Agent, error)) tea.Cmd {
	return m.paneWriteCmd(summary, false, write)
}

// openingWriteCmd is agentWriteCmd for a write that puts a conversation back in a pane, which
// ends with the keyboard in that pane the way the open key over a live agent does.
func (m *model) openingWriteCmd(summary string, write func(api) (*agentv1.Agent, error)) tea.Cmd {
	return m.paneWriteCmd(summary, true, write)
}

func (m *model) paneWriteCmd(summary string, focus bool, write func(api) (*agentv1.Agent, error)) tea.Cmd {
	return m.rowWriteCmd(summary, func(clients api) (written, error) {
		agent, err := write(clients)
		if err != nil {
			return written{}, err
		}

		return written{
			row:     agentRowKey(agent.GetId()),
			pane:    agent.GetPaneId(),
			harness: agent.GetHarness(),
			focus:   focus,
		}, nil
	})
}

func (m *model) resumeCmd(params resumeParams) tea.Cmd {
	return m.openingWriteCmd(params.summary, func(clients api) (*agentv1.Agent, error) {
		return clients.resumeAgent(params.id)
	})
}

func (m *model) retryCmd(agent agentstore.Agent, values retryFormValues) tea.Cmd {
	return m.agentWriteCmd(fmt.Sprintf("Could not retry agent %q", agent.Name), func(clients api) (*agentv1.Agent, error) {
		return clients.retryAgent(retryAgentParams{
			ID:      agent.ID,
			Flags:   values.Flags,
			Prompt:  values.Prompt,
			Harness: values.Harness,
		})
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

		// A new workflow comes with the agent that stands behind it, and stands on its own when
		// that agent will not start.
		agent, err := clients.startOrchestrator(workflow.GetId())
		if err != nil {
			return written{row: workflowRowKey(workflow.GetId())}, nil //nolint:nilerr // the workflow was created
		}

		return written{
			row:     workflowRowKey(workflow.GetId()),
			pane:    agent.GetPaneId(),
			harness: agent.GetHarness(),
			focus:   true,
		}, nil
	})
}

// beginWrite counts a write out and answers with the epoch it belongs to, which tells a landing
// result apart from one an earlier submit is still waiting on.
func (m *model) beginWrite() int {
	m.epoch++
	m.pending++

	return m.epoch
}

func (m model) applyWrite(result writeResult) (tea.Model, tea.Cmd) {
	m.pending--

	m.report(result.summary, result.err)

	if result.queued != nil && result.err == nil {
		m.pushMessage(*result.queued)
	}

	if result.released != nil && result.err == nil {
		m.forgetSpoken(result.released.id, result.released.files)

		if result.released.silent {
			m.reportDropped(result.released.name, result.released.files)
		}
	}

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
				m.frame.ShowGuest(result.written.pane)

				if result.written.focus && m.frame.GuestPane() == result.written.pane {
					m.tmux.SelectPane(result.written.pane)
				}
			} else {
				m.warm.hold(result.written.pane, result.written.harness, result.written.focus)
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

		m.messages.setNewestHint("fix and submit again")
	}

	// A refusal is reported into the region under the record, so the form is told to lay itself
	// out again.
	if m.form.isActive() {
		return m, tea.Batch(m.refreshCmd(), formResized())
	}

	return m, m.refreshCmd()
}
