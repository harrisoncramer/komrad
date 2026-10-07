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
	"komrad/internal/ui/submitting"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
)

const (
	tickInterval  = time.Second
	agentsTimeout = 2 * time.Second
	saidTimeout   = 30 * time.Second
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

// withListing changes the listing the screens render from and puts each cursor back where it was.
func (m *model) withListing(change func(*listing)) tea.Cmd {
	m.frame.Begin()
	defer m.frame.Flush()

	treeRow := m.agents.list().hovered(m)
	triageSlot := m.agents.findZoneSlot(m)
	scheduleRow := m.schedules.list().hovered(m)
	archivedAgentRow := m.agents.archived.list().hovered(m)
	archivedScheduleRow := m.schedules.archived.list().hovered(m)
	worktreeRow := m.worktrees.list().hovered(m)

	before := slices.Clone(m.listing.workflows)
	beforeTypes := m.listing.documentTypeNames()
	change(&m.listing)

	now := time.Now()
	m.agents.triage.refresh(m.listing.agents, now)
	m.agents.apply(m, treeRow)
	m.agents.followAwaitingCLI(m)
	m.agents.landInTriage(m, treeRow, triageSlot)
	m.schedules.apply(m, scheduleRow)
	m.agents.archived.place(m, archivedAgentRow)
	m.schedules.archived.place(m, archivedScheduleRow)
	m.applyWorktrees(m.listing.worktrees, worktreeRow)
	m.sizeAgentWindows()

	// A form renders its options from the listing it was built over, so it is rendered again when
	// that listing moves under it.
	var rerendered tea.Cmd
	if !reflect.DeepEqual(before, m.listing.workflows) || !slices.Equal(beforeTypes, m.listing.documentTypeNames()) {
		rerendered = m.rerenderOpenForm()
	}

	return tea.Batch(rerendered, m.finishParked(), m.afterListing())
}

// afterListing asks for what the hovered rows are owed and stages the screen over them.
func (m *model) afterListing() tea.Cmd {
	inspect := tea.Batch(m.inspectHoveredWorktreeCmd(), m.inspectAgentWorktreeCmd(time.Now()), m.readDocumentsCmd(), m.readStatusChecksCmd(), m.readMovedLastScreenCmd())

	// A screen that renders what it holds beside its own list stages nothing at all.
	active := m.active()
	if s, ok := active.(rowStager); ok && active.doesRenderFromListing() && !m.form.isActive() {
		s.stage(m)
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
	// announce is what to say once the write has landed.
	announce *message
	// renamed is the row a rename typed over it was written for.
	renamed string
	// resumed is the agent a resume was written for.
	resumed  string
	unstaged *unstaged
	// awaits is what the panel shows once the write is answered, and the write is held until it does.
	awaits *awaits
	// keyMoved says a key moved the cursor while the write was parked.
	keyMoved bool
}

// focusGuestMsg hands the keyboard to a pane once the change that put it on the stage is sent.
type focusGuestMsg struct {
	pane string
}

type released struct {
	id   string
	name string
	// files is the files a reader had open over the agent.
	files []string
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

func (m *model) writeCmd(doing, summary string, write func(api) error) tea.Cmd {
	return m.awaitingWriteCmd(doing, summary, nil, write)
}

// awaitingWriteCmd sends a write that is held, once it lands, until the panel shows what it did.
func (m *model) awaitingWriteCmd(doing, summary string, awaits *awaits, write func(api) error) tea.Cmd {
	clients := m.api
	id := m.beginWrite(doing)

	return func() tea.Msg {
		return writeResult{
			summary: summary,
			err:     write(clients),
			write:   id,
			awaits:  awaits,
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

	return stampRelease(m.writeCmd("letting "+agent.Name+" go", summary, func(a api) error {
		return a.release(agent, said+touchedSaid(let.files))
	}), let)
}

// announced says on a write's result the message to show once it finishes.
func announced(cmd tea.Cmd, announce message) tea.Cmd {
	return func() tea.Msg {
		msg := cmd()

		result, ok := msg.(writeResult)
		if !ok {
			return msg
		}

		result.announce = &announce

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
func (m *model) rowWriteCmd(doing, summary string, write func(api) (written, error)) tea.Cmd {
	clients := m.api
	id := m.beginWrite(doing)

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
			awaits:  row.awaitShown(),
		}
	}
}

// agentWriteCmd is rowWriteCmd for a write answering with an agent, whose row is shown in the
// pane the agent is running in.
func (m *model) agentWriteCmd(doing, summary string, write func(api) (*agentv1.Agent, error)) tea.Cmd {
	return m.paneWriteCmd(doing, summary, false, write)
}

// openingWriteCmd is agentWriteCmd for a write that puts a conversation back in a pane, which
// ends with the keyboard in that pane the way the open key over a live agent does.
func (m *model) openingWriteCmd(doing, summary string, write func(api) (*agentv1.Agent, error)) tea.Cmd {
	return m.paneWriteCmd(doing, summary, true, write)
}

func (m *model) paneWriteCmd(doing, summary string, focus bool, write func(api) (*agentv1.Agent, error)) tea.Cmd {
	return m.rowWriteCmd(doing, summary, func(clients api) (written, error) {
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
	return m.agentWriteCmd("retrying "+agent.Name, fmt.Sprintf("Could not retry agent %q", agent.Name), func(clients api) (*agentv1.Agent, error) {
		return clients.retryAgent(retryAgentParams{
			ID:      agent.ID,
			Flags:   values.Flags,
			Prompt:  values.Prompt,
			Harness: values.Harness,
		})
	})
}

func (m *model) createCmd(params createAgentParams) tea.Cmd {
	return m.agentWriteCmd(startingLabel(params.Title), "Could not start the agent", func(clients api) (*agentv1.Agent, error) {
		return clients.createAgent(params)
	})
}

func startingLabel(title string) string {
	if title = strings.TrimSpace(title); title != "" {
		return "starting " + title
	}

	return "starting the agent"
}

func (m *model) adoptCmd(params adoptParams) tea.Cmd {
	return m.rowWriteCmd("adopting "+params.Ref, "Could not adopt", func(clients api) (written, error) {
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
	return m.rowWriteCmd("creating workflow "+cfg.Name, "Could not create the workflow", func(clients api) (written, error) {
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

// beginWrite counts a write out under what it is doing and answers with its number, which tells
// a landing result apart from one an earlier submit is still waiting on.
func (m *model) beginWrite(doing string) int {
	m.writes.count++

	if m.writes.inflight == nil {
		m.writes.inflight = map[int]submitting.Write{}
	}
	m.writes.inflight[m.writes.count] = submitting.Write{Doing: doing, Since: time.Now()}

	return m.writes.count
}

// clients is the service a write that lands its own message is sent through.
func (m *model) clients() api {
	return m.api
}

// countedWrite is a message that lands a write begun with beginWrite.
type countedWrite interface {
	countedWrite() int
}

func (w reviewWroteMsg) countedWrite() int {
	return w.write
}

func (w reviewPausedMsg) countedWrite() int {
	return w.write
}

// landWrite counts a write back in.
func (m *model) landWrite(write int) {
	delete(m.writes.inflight, write)
}

// applyWrite lands a write once the panel shows what it did, parking it until then.
func (m *model) applyWrite(result writeResult) tea.Cmd {
	m.frame.Begin()
	defer m.frame.Flush()

	caught := m.releaseWatch(m.writes.held.owns(result.write))

	if result.err == nil && result.awaits != nil && !result.awaits.isSettled(m) {
		m.parkWrite(result)

		return caught
	}

	return tea.Batch(caught, m.finishWrite(result))
}

// finishWrite reports a write, takes down its form and shows what it made in one change.
func (m *model) finishWrite(result writeResult) tea.Cmd {
	m.frame.Begin()
	defer m.frame.Flush()

	m.report(result.summary, result.err)

	if result.renamed != "" {
		m.agents.settleRename(m, result.renamed, result.err)
		m.documents.settleRename(m, result.renamed, result.err)
		m.agents.archived.settleRename(m, result.renamed, result.err)
		m.schedules.settleRename(m, result.renamed, result.err)
	}

	if result.resumed != "" {
		m.agents.settleResume(m, result.resumed, result.err)
	}

	if result.announce != nil && result.err == nil {
		m.pushMessage(*result.announce)
	}

	if result.released != nil && result.err == nil {
		m.forgetSpoken(result.released.id, result.released.files)
	}

	if result.unstaged != nil {
		m.settleUnstaged(result.unstaged)
	}

	_, walkedAway := m.writes.detached[result.write]
	delete(m.writes.detached, result.write)

	if result.written != nil && result.written.notice != nil {
		m.pushMessage(*result.written.notice)
	}

	var cmds []tea.Cmd
	if m.writes.held.owns(result.write) {
		m.writes.held = heldWrite{}

		if result.err == nil || !m.form.keepsOnRefusal {
			cmds = append(cmds, m.closeForm())
		} else {
			m.form.refusal = errui.GetReason(result.err)
			m.messages.setNewestHint("fix and submit again")
		}
	}

	// A refusal is reported into the region under the record, so the form is told to lay itself
	// out again.
	if m.form.isActive() {
		cmds = append(cmds, formResized())
	}

	switch {
	case result.written == nil || walkedAway:
	case result.written.archived && result.keyMoved:
		m.agents.kind = agentsArchived
		cmds = append(cmds, m.enterScreen(screenAgents))
	case result.written.archived:
		cmds = append(cmds, m.enterArchived(screenTarget{agentID: result.written.agent}))
	default:
		m.agents.kind = agentsLive
		cmds = append(cmds, m.enterScreen(screenAgents))

		m.agents.showHiddenFor(m, result.written.agent)

		rows := m.agents.rows(m)
		if key := findLandingKey(rows, m.agents.list().hovered(m), result.written.row); !result.keyMoved && rowIndexOfKey(rows, key) >= 0 {
			m.agents.apply(m, key)
		}

		if pane := result.written.pane; pane != "" {
			cmds = append(cmds, m.agents.closeEdit(m))
			m.frame.Solo(false)
			m.frame.ShowGuest(layout.Guest{Agent: result.written.agent, Pane: pane})
			delete(m.warm.since, pane)

			if result.written.focus {
				cmds = append(cmds, func() tea.Msg { return focusGuestMsg{pane: pane} })
			}
		} else {
			m.stageSelection()
		}
	}

	m.landWrite(result.write)

	return tea.Batch(cmds...)
}
