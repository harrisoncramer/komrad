package ui

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"weave/internal/errui"
	"weave/internal/ui/panel"
	agentv1 "weave/proto/gen/agent/v1"
	watchv1 "weave/proto/gen/watch/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	tickInterval  = time.Second
	agentsTimeout = 2 * time.Second
)

type tickMsg struct {
	at time.Time
}

func tick() tea.Cmd {
	return tea.Tick(tickInterval, func(at time.Time) tea.Msg { return tickMsg{at: at} })
}

// viewedDwell is how long a failed run's log has to be on screen before the panel counts it as read.
const viewedDwell = time.Second

// hoverState is the agent the cursor is sitting on and when it arrived.
type hoverState struct {
	agentID string
	since   time.Time
}

// reportUnreachable says the server has stopped answering the first time it is found gone.
func (m *model) reportUnreachable(err error) {
	was := m.unreachable
	m.unreachable = err

	if err == nil || was != nil {
		return
	}

	reason := errui.GetReason(err)
	if m.settings.Addr != "" && !strings.Contains(reason, m.settings.Addr) {
		reason = m.settings.Addr + ": " + reason
	}

	m.pushMessage(message{
		kind:    messageError,
		summary: "Cannot reach the server",
		detail:  reason,
		trace:   errui.GetTrace(err),
	})
	m.messages.setNewestHint(m.unreachableHint())
}

// unreachableHint is what the user can do about a server that has stopped answering.
func (m *model) unreachableHint() string {
	if m.frame.IsRemote() {
		return "The tunnel to the engine is dialled again until it answers"
	}

	return "Start it with: weave serve"
}

// withListing changes the listing the screens draw from and puts each cursor back where it was.
func (m *model) withListing(change func(*listing)) tea.Cmd {
	treeRow := m.agents.list().hovered(m.shared())
	scheduleRow := m.schedules.list().hovered(m.shared())
	archiveRow := m.archive.list().hovered(m.shared())
	worktreeRow := m.worktrees.list().hovered(m.shared())
	searchRow := m.search.list().hovered(m.shared())

	before := slices.Clone(m.listing.workflows)
	change(&m.listing)

	m.agents.apply(m.shared(), treeRow)
	m.schedules.apply(m, m.shared(), scheduleRow)
	m.archive.place(m.shared(), archiveRow)
	m.applyWorktrees(m.listing.worktrees, worktreeRow)
	m.sizeAgentWindows()

	preview := m.search.place(m.shared(), searchRow)

	// A form draws its options from the listing it was built over, so it is drawn again when
	// that listing moves under it.
	var redrawn tea.Cmd
	if !reflect.DeepEqual(before, m.listing.workflows) {
		redrawn = m.redrawOpenForm()
	}

	return tea.Batch(preview, redrawn, m.afterListing())
}

// afterListing asks for what the hovered rows are owed and stages the screen over them.
func (m *model) afterListing() tea.Cmd {
	inspect := tea.Batch(m.inspectHoveredWorktreeCmd(), m.inspectAgentWorktreeCmd(time.Now()), m.readScheduleRunsCmd())

	// A screen that draws what it holds beside its own list stages nothing at all.
	active := m.active()
	if s, ok := active.(staged); ok && active.needs().fromListing && !m.form.isActive() {
		s.stage(m, m.shared())
	}

	return inspect
}

type writeResult struct {
	summary string
	err     error
	written *written
	// write is which write this result belongs to.
	write int
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

// written is the row a write made, as the write answered with it.
type written struct {
	row string
	// agent and workflow are the rows the write answered with, inserted where the listing lacks
	// them.
	agent    *agentv1.Agent
	workflow *workflowv1.Workflow
	pane     string
	harness  string
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
func (m model) runLogTarget() (panel.Agent, bool) {
	if m.info.kind != infoRunLog {
		return panel.Agent{}, false
	}

	return m.logTarget()
}

// isFailedRunShowing reports whether the agent the run-log pane is on is still carrying a mark.
func (m model) isFailedRunShowing(agentID string) bool {
	for _, item := range m.listing.agents {
		if item.Agent.ID == agentID {
			return item.FailedRun
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
	id := m.beginWrite()

	return func() tea.Msg {
		return writeResult{
			summary: summary,
			err:     write(clients),
			write:   id,
		}
	}
}

// noteEdited keeps the files a reader opened over a paused agent until whoever holds that pause
// lets it go.
func (m *model) noteEdited(agentID string, touched []string) {
	if len(touched) == 0 {
		return
	}

	if m.edited == nil {
		m.edited = map[string][]string{}
	}

	for _, path := range touched {
		if !slices.Contains(m.edited[agentID], path) {
			m.edited[agentID] = append(m.edited[agentID], path)
		}
	}
}

// forgetSpoken drops the files a release has already carried.
func (m *model) forgetSpoken(agentID string, carried []string) {
	kept := slices.DeleteFunc(m.edited[agentID], func(path string) bool {
		return slices.Contains(carried, path)
	})

	if len(kept) == 0 {
		delete(m.edited, agentID)

		return
	}

	m.edited[agentID] = kept
}

// releaseCmd lets an agent go, naming the files a reader had open over it.
func (m *model) releaseCmd(summary string, agent panel.Agent, said string) tea.Cmd {
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
func (m *model) releaseInSilenceCmd(summary string, agent panel.Agent) tea.Cmd {
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

// rowWriteCmd sends a write whose landing takes the panel to the row it made.
func (m *model) rowWriteCmd(summary string, write func(api) (written, error)) tea.Cmd {
	clients := m.api
	id := m.beginWrite()

	return func() tea.Msg {
		row, err := write(clients)
		if err != nil {
			return writeResult{
				summary: summary,
				err:     err,
				write:   id,
			}
		}

		return writeResult{
			summary: summary,
			written: &row,
			write:   id,
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
			agent:   agent,
			pane:    agent.GetPaneId(),
			harness: agent.GetHarness(),
			focus:   focus,
		}, nil
	})
}

func (m *model) retryCmd(agent panel.Agent, values retryFormValues) tea.Cmd {
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

func (m *model) createWorkflowCmd(cfg panel.WorkflowConfig) tea.Cmd {
	return m.rowWriteCmd("Could not create the workflow", func(clients api) (written, error) {
		workflow, err := clients.createWorkflow(cfg)
		if err != nil {
			return written{}, err
		}

		// A new workflow comes with the agent that stands behind it, and stands on its own when
		// that agent will not start.
		agent, err := clients.startOrchestrator(workflow.GetId())
		if err != nil {
			return written{row: workflowRowKey(workflow.GetId()), workflow: workflow}, nil //nolint:nilerr // the workflow was created
		}

		return written{
			row:      workflowRowKey(workflow.GetId()),
			agent:    agent,
			workflow: workflow,
			pane:     agent.GetPaneId(),
			harness:  agent.GetHarness(),
			focus:    true,
		}, nil
	})
}

// beginWrite counts a write out and answers with its number, which tells a landing result
// apart from one an earlier submit is still waiting on.
func (m *model) beginWrite() int {
	m.writes++
	m.pending++

	return m.writes
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

	_, walkedAway := m.detached[result.write]
	delete(m.detached, result.write)

	var landed tea.Cmd
	if result.written != nil && !walkedAway {
		m.enterScreen(screenAgents)
		landed = m.landOn(*result.written)

		if result.written.pane != "" {
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

	if m.held.owns(result.write) {
		// A form taken down now would leave the panel drawn over the whole window until the pane
		// it made turns up, so it is left standing and closed by the reveal instead.
		if result.err == nil && m.warm.pending != "" {
			m.warm.closesForm = true

			return m, landed
		}

		m.held = heldWrite{}

		if result.err == nil {
			closed, cmd := m.closeForm()

			return closed, tea.Batch(landed, cmd)
		}

		m.messages.setNewestHint("fix and submit again")
	}

	// A refusal is reported into the region under the record, so the form is told to lay itself
	// out again.
	if m.form.isActive() {
		return m, tea.Batch(landed, formResized())
	}

	return m, landed
}

// landOn puts the row a write made into the world where the watch has not delivered it yet,
// and the cursor on it.
func (m *model) landOn(row written) tea.Cmd {
	var puts []*watchv1.Row
	if row.workflow != nil && m.listing.world.Workflows[row.workflow.GetId()] == nil {
		puts = append(puts, &watchv1.Row{Row: &watchv1.Row_Workflow{Workflow: row.workflow}})
	}
	if row.agent != nil && m.listing.world.Agents[row.agent.GetId()] == nil {
		puts = append(puts, &watchv1.Row{Row: &watchv1.Row_Agent{Agent: row.agent}})
	}
	changed := m.listing.world.Apply(&watchv1.WatchEvent{Event: &watchv1.WatchEvent_Changes{Changes: &watchv1.Changes{Puts: puts}}})

	m.listing.assemble(time.Now())
	m.sizeAgentWindows()
	m.agents.apply(m.shared(), row.row)

	return m.react(changed)
}
