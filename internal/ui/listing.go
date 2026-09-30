package ui

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"komrad/internal/errui"
	"komrad/internal/ui/layout"
	"komrad/internal/ui/panel"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
)

const (
	tickInterval  = time.Second
	agentsTimeout = 2 * time.Second
)

var startTimer = tea.Tick

type tickMsg struct {
	at time.Time
}

func tick() tea.Cmd {
	return startTimer(tickInterval, func(at time.Time) tea.Msg { return tickMsg{at: at} })
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

	return "Start it with: komrad serve"
}

// withListing changes the listing the screens draw from and puts each cursor back where it was.
func (m *model) withListing(change func(*listing)) tea.Cmd {
	treeRow := m.agents.list().hovered(m.shared())
	scheduleRow := m.schedules.list().hovered(m.shared())
	archiveRow := m.archive.list().hovered(m.shared())
	archiveScheduleRow := m.archive.scheduleList().hovered(m.shared())
	worktreeRow := m.worktrees.list().hovered(m.shared())
	searchRow := m.search.list().hovered(m.shared())
	typeRow := m.documentTypes.list().hovered(m.shared())

	before := slices.Clone(m.listing.workflows)
	beforeTypes := m.listing.documentTypeNames()
	change(&m.listing)

	m.agents.apply(m.shared(), m.landedRow(treeRow, time.Now()))
	m.schedules.apply(m, m.shared(), scheduleRow)
	m.archive.place(m.shared(), archiveRow)
	m.archive.placeSchedule(m.shared(), archiveScheduleRow)
	m.applyWorktrees(m.listing.worktrees, worktreeRow)
	m.documentTypes.keepCursor(m.shared(), typeRow)
	m.sizeAgentWindows()

	preview := m.search.place(m.shared(), searchRow)

	// A form draws its options from the listing it was built over, so it is drawn again when
	// that listing moves under it.
	var redrawn tea.Cmd
	if !reflect.DeepEqual(before, m.listing.workflows) || !slices.Equal(beforeTypes, m.listing.documentTypeNames()) {
		redrawn = m.redrawOpenForm()
	}

	return tea.Batch(preview, redrawn, m.afterListing())
}

// afterListing asks for what the hovered rows are owed and stages the screen over them.
func (m *model) afterListing() tea.Cmd {
	inspect := tea.Batch(m.inspectHoveredWorktreeCmd(), m.inspectAgentWorktreeCmd(time.Now()), m.readDocumentsCmd(), m.readMovedLastScreenCmd())

	// A screen that draws what it holds beside its own list stages nothing at all.
	active := m.active()
	if s, ok := active.(staged); ok && active.doesDrawFromListing() && !m.form.isActive() {
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
	// renamed is the row a rename typed over it was written for.
	renamed string
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
	row     string
	agent   string
	pane    string
	harness string
	// focus says the pane the write made is to be handed the keyboard once it is shown.
	focus bool
	// archived says the row is in the archive rather than the agents list.
	archived bool
	// notice is what to say once the panel has landed on the row.
	notice *message
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
			agent:   agent.GetId(),
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
	return m.rowWriteCmd("Could not adopt", func(clients api) (written, error) {
		adopted, err := clients.adopt(params)
		if err != nil {
			return written{}, err
		}

		agent := adopted.GetAgent()
		row := written{
			row:     agentRowKey(agent.GetId()),
			agent:   agent.GetId(),
			pane:    agent.GetPaneId(),
			harness: agent.GetHarness(),
		}

		if adopted.GetExisting() {
			row.archived = agent.ArchivedAt != nil
			row.notice = &message{
				kind:    messageInfo,
				summary: "This agent already exists",
				detail:  fmt.Sprintf("%s is already on %s", agent.GetName(), params.Ref),
			}
		}

		return row, nil
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
			return written{row: workflowRowKey(workflow.GetId())}, nil //nolint:nilerr // the workflow was created
		}

		return written{
			row:     workflowRowKey(workflow.GetId()),
			agent:   agent.GetId(),
			pane:    agent.GetPaneId(),
			harness: agent.GetHarness(),
			focus:   true,
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

	if result.renamed != "" {
		m.agents.settleRename(m.shared(), result.renamed, result.err)
		m.documents.settleRename(m.shared(), result.renamed, result.err)
		m.documentTypes.settleRename(m.shared(), result.renamed, result.err)
	}

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

	if result.written != nil && result.written.notice != nil {
		m.pushMessage(*result.written.notice)
	}

	var left tea.Cmd
	switch {
	case result.written == nil || walkedAway:
	case result.written.archived:
		left = m.enter(screenArchive, screenTarget{agentID: result.written.agent})
	default:
		left = m.enterScreen(screenAgents)
		m.landOn(*result.written, time.Now())

		if result.written.pane != "" {
			left = tea.Batch(left, m.agents.closeEdit(&m))
			m.dropScratch()
			guest := layout.Guest{Agent: result.written.agent, Pane: result.written.pane}
			if m.isPaneWarm(result.written.pane, result.written.harness) {
				m.frame.Solo(false)
				m.frame.ShowGuest(guest)

				if result.written.focus && m.frame.GuestPane() == result.written.pane {
					m.tmux.SelectPane(result.written.pane)
				}
			} else {
				m.warm.hold(guest, result.written.harness, result.written.focus)
			}
		}
	}

	if m.held.owns(result.write) {
		// A form taken down now would leave the panel drawn over the whole window until the pane
		// it made turns up, so it is left standing and closed by the reveal instead.
		if result.err == nil && m.warm.pending != "" {
			m.warm.closesForm = true

			return m, left
		}

		m.held = heldWrite{}

		if result.err == nil {
			closed, cmd := m.closeForm()

			return closed, tea.Batch(left, cmd)
		}

		m.messages.setNewestHint("fix and submit again")
	}

	// A refusal is reported into the region under the record, so the form is told to lay itself
	// out again.
	if m.form.isActive() {
		return m, tea.Batch(left, formResized())
	}

	return m, left
}

// landingWait is how long a write's landing waits for the watch to deliver the row it made.
const landingWait = 2 * time.Second

// landing is the row a write made, which the cursor goes to once the watch delivers it.
type landing struct {
	row   string
	until time.Time
}

// landOn puts the cursor on the row a write made, or holds it for the watch to deliver.
func (m *model) landOn(row written, now time.Time) {
	if rowIndexOfKey(m.agents.rows(m.shared()), row.row) >= 0 {
		m.landing = landing{}
		m.agents.apply(m.shared(), row.row)

		return
	}

	m.landing = landing{row: row.row, until: now.Add(landingWait)}
}

// isLandingHeld reports whether a write's landing is still waiting for the watch to deliver its row.
func (m *model) isLandingHeld(now time.Time) bool {
	return m.landing.row != "" && !now.After(m.landing.until)
}

// landedRow is the row a held landing goes to once the listing holds it, or the hovered row.
func (m *model) landedRow(hovered string, now time.Time) string {
	target := m.landing
	if !m.isLandingHeld(now) {
		m.landing = landing{}

		return hovered
	}

	if rowIndexOfKey(m.agents.rows(m.shared()), target.row) < 0 {
		return hovered
	}

	m.landing = landing{}

	return target.row
}
