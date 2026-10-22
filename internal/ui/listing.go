package ui

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"komrad/internal/errui"
	"komrad/internal/ui/layout"
	"komrad/internal/ui/list"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
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
		return "The panel keeps trying the engine until it answers"
	}

	return "Start it with: komrad serve"
}

// withListing changes the listing the screens read from and puts each cursor back where it was.
func (m *model) withListing(change func(*panel.Listing)) tea.Cmd {
	m.frame.Begin()
	defer m.frame.Flush()

	treeRow := m.agents.list().hovered(m)
	triageSlot := m.agents.findZoneSlot(m)
	scheduleRow := m.schedules.list().hovered(m)
	archiveShown := m.agents.shownArchive()
	var archivedAgentRow string
	if archiveShown != nil {
		archivedAgentRow = archiveShown.list().hovered(m)
	}
	archivedScheduleRow := m.schedules.archived.list().hovered(m)
	worktreeRow := m.worktrees.list().hovered(m)

	before := slices.Clone(m.listing.Workflows)
	beforeTypes := m.listing.DocumentTypeNames()
	change(&m.listing)

	now := time.Now()
	m.agents.triage.refresh(m.listing.Agents, m.listing.Tasks, now)
	m.agents.apply(m, treeRow)
	m.agents.followAwaitingCLI(m)
	m.agents.landInTriage(m, treeRow, triageSlot)
	m.schedules.apply(m, scheduleRow)
	if archiveShown != nil {
		archiveShown.place(m, archivedAgentRow)
	}
	m.schedules.archived.place(m, archivedScheduleRow)
	m.applyWorktrees(m.listing.Worktrees, worktreeRow)
	m.sizeAgentWindows()

	// A form renders its options from the listing it was built over, so it is rendered again when
	// that listing moves under it.
	var rerendered tea.Cmd
	if !reflect.DeepEqual(before, m.listing.Workflows) || !slices.Equal(beforeTypes, m.listing.DocumentTypeNames()) {
		rerendered = m.rerenderOpenForm()
	}

	return tea.Batch(rerendered, m.finishParked(), m.afterListing())
}

// afterListing asks for what the hovered rows are owed and stages the screen over them.
func (m *model) afterListing() tea.Cmd {
	inspect := tea.Batch(m.inspectHoveredWorktreeCmd(), m.inspectTaskWorktreeCmd(time.Now()), m.readDocumentsCmd(), m.readStatusChecksCmd(), m.readMovedLastScreenCmd())

	// A screen that renders what it holds beside its own list stages nothing at all.
	active := m.active()
	if s, ok := active.(rowStager); ok && active.doesRenderFromListing() && !m.form.isActive() {
		s.stage(m)
	}

	m.restageBeside()

	return inspect
}

type writeResult struct {
	summary string
	landed  screen.Landed
	// write is which write this result belongs to.
	write int
	// unstaged is the agents whose panes came off the stage before the write was sent.
	unstaged []panel.Agent
	// awaits is what the panel shows once the write is answered, and the write is held until it does.
	awaits screen.Awaits
	// keyMoved says a key moved the cursor while the write was parked.
	keyMoved bool
}

// focusGuestMsg hands the keyboard to a pane once the change that put it on the stage is sent.
type focusGuestMsg struct {
	pane string
}

// send runs a write off the panel's loop, counted in flight under what it is doing.
func (m *model) send(write screen.Write) tea.Cmd {
	id := 0
	if write.Doing != "" {
		id = m.beginWrite(write.Doing)
	}

	if len(write.Unstage) > 0 {
		m.unstageAhead(write.Unstage)
	}

	return func() tea.Msg {
		landed := write.Send()

		return writeResult{
			summary:  write.Summary,
			landed:   landed,
			write:    id,
			unstaged: write.Unstage,
			awaits:   awaitsOf(landed, write.Unstage),
		}
	}
}

func (m *model) write(doing, summary string, write func(api) error) screen.Write {
	return m.awaitingWrite(doing, summary, screen.Awaits{}, write)
}

// awaitingWrite is a write that is held, once it lands, until the panel shows what it did.
func (m *model) awaitingWrite(doing, summary string, awaits screen.Awaits, write func(api) error) screen.Write {
	clients := m.api

	return screen.Write{
		Doing:   doing,
		Summary: summary,
		Send: func() screen.Landed {
			return screen.Landed{Err: write(clients), Awaits: awaits}
		},
	}
}

// archivingWriteParams is a write that archives agents, their panes taken off the stage before it
// is sent.
type archivingWriteParams struct {
	doing   string
	summary string
	agents  []panel.Agent
	awaits  screen.Awaits
	write   func(api) error
}

func (m *model) archivingWrite(params archivingWriteParams) screen.Write {
	clients := m.api

	return screen.Write{
		Doing:   params.doing,
		Summary: params.summary,
		Unstage: params.agents,
		Send: func() screen.Landed {
			err := params.write(clients)

			return screen.Landed{Err: err, Awaits: params.awaits, Killed: err == nil}
		},
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

// releaseWrite is the write that lets an agent go, naming the files a reader had open over it.
func (m *model) releaseWrite(summary string, agent panel.Agent, said string) screen.Write {
	clients := m.api
	let := screen.Released{
		ID:    agent.ID,
		Name:  agent.Name,
		Files: slices.Clone(m.edited[agent.ID]),
	}

	return screen.Write{
		Doing:   "letting " + agent.Name + " go",
		Summary: summary,
		Send: func() screen.Landed {
			return screen.Landed{
				Err:      clients.release(agent, said+touchedSaid(let.Files)),
				Released: &let,
			}
		},
	}
}

// rowWrite is a write whose landing takes the panel to the row it made.
func (m *model) rowWrite(doing, summary string, write func(api) (*screen.Made, error)) screen.Write {
	clients := m.api

	return screen.Write{
		Doing:   doing,
		Summary: summary,
		Send: func() screen.Landed {
			made, err := write(clients)
			if err != nil {
				return screen.Landed{Err: err}
			}

			return screen.Landed{Made: made}
		},
	}
}

// agentWrite is rowWrite for a write answering with an agent, whose row is shown in the
// pane the agent is running in.
func (m *model) agentWrite(doing, summary string, write func(api) (*agentv1.Agent, error)) screen.Write {
	return m.paneWrite(doing, summary, false, write)
}

// openingWrite is agentWrite for a write that puts a conversation back in a pane, which
// ends with the keyboard in that pane the way the open key over a live agent does.
func (m *model) openingWrite(doing, summary string, write func(api) (*agentv1.Agent, error)) screen.Write {
	return m.paneWrite(doing, summary, true, write)
}

func (m *model) paneWrite(doing, summary string, focus bool, write func(api) (*agentv1.Agent, error)) screen.Write {
	return m.rowWrite(doing, summary, func(clients api) (*screen.Made, error) {
		agent, err := write(clients)
		if err != nil {
			return nil, err
		}

		return madeAgent(agent, focus), nil
	})
}

// madeAgent is the row of an agent a write made, shown in the pane the agent is running in.
func madeAgent(agent *agentv1.Agent, focus bool) *screen.Made {
	return &screen.Made{
		Row:     agentRowKey(agent.GetId()),
		Agent:   agent.GetId(),
		Pane:    agent.GetPaneId(),
		Harness: agent.GetHarness(),
		Focus:   focus,
	}
}

func (m *model) retryWrite(agent panel.Agent, values retryFormValues) screen.Write {
	return m.agentWrite("retrying "+agent.Name, fmt.Sprintf("Could not retry agent %q", agent.Name), func(clients api) (*agentv1.Agent, error) {
		return clients.retryAgent(retryAgentParams{
			ID:      agent.ID,
			Flags:   values.Flags,
			Prompt:  values.Prompt,
			Harness: values.Harness,
		})
	})
}

func (m *model) createWrite(params createAgentParams) screen.Write {
	return m.agentWrite(startingLabel(params.Title), "Could not start the agent", func(clients api) (*agentv1.Agent, error) {
		return clients.createAgent(params)
	})
}

func startingLabel(title string) string {
	if title = strings.TrimSpace(title); title != "" {
		return "starting " + title
	}

	return "starting the agent"
}

func (m *model) adoptWrite(params adoptParams) screen.Write {
	clients := m.api

	return screen.Write{
		Doing:   "adopting " + params.Ref,
		Summary: "Could not adopt",
		Send: func() screen.Landed {
			adopted, err := clients.adopt(params)
			if err != nil {
				return screen.Landed{Err: err}
			}

			agent := adopted.GetAgent()
			landed := screen.Landed{Made: madeAgent(agent, false)}
			if adopted.GetExisting() {
				landed.Made.Archived = agent.ArchivedAt != nil
				landed.Notice = &screen.Notice{
					Summary: "This agent already exists",
					Detail:  fmt.Sprintf("%s is already on %s", agent.GetName(), params.Ref),
				}
			}

			return landed
		},
	}
}

func (m *model) createWorkflowWrite(cfg panel.WorkflowConfig) screen.Write {
	return m.rowWrite("creating workflow "+cfg.Name, "Could not create the workflow", func(clients api) (*screen.Made, error) {
		workflow, err := clients.createWorkflow(cfg)
		if err != nil {
			return nil, err
		}

		// A new workflow comes with the agent that stands behind it, and stands on its own when
		// that agent will not start.
		agent, err := clients.startOrchestrator(workflow.GetId())
		if err != nil {
			return &screen.Made{Row: workflowRowKey(workflow.GetId())}, nil //nolint:nilerr // the workflow was created
		}

		return &screen.Made{
			Row:     workflowRowKey(workflow.GetId()),
			Agent:   agent.GetId(),
			Pane:    agent.GetPaneId(),
			Harness: agent.GetHarness(),
			Focus:   true,
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

// clients is the server a write that lands its own message is sent through.
func (m *model) clients() api {
	return m.api
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

	if result.landed.Err == nil && !m.isSettled(result.awaits) {
		m.parkWrite(result)

		return caught
	}

	return tea.Batch(caught, m.finishWrite(result))
}

// finishWrite reports a write, takes down its form, shows what it made and hands the asking
// screen its message in one change.
func (m *model) finishWrite(result writeResult) tea.Cmd {
	m.frame.Begin()
	defer m.frame.Flush()

	landed := result.landed
	if result.summary != "" {
		m.report(result.summary, landed.Err)
	}

	if landed.Released != nil && landed.Err == nil {
		m.forgetSpoken(landed.Released.ID, landed.Released.Files)
	}

	if len(result.unstaged) > 0 {
		m.settleUnstaged(result.unstaged, landed.Killed)
	}

	if landed.Notice != nil && landed.Err == nil {
		m.pushMessage(message{
			kind:    messageInfo,
			summary: landed.Notice.Summary,
			detail:  landed.Notice.Detail,
		})
	}

	_, walkedAway := m.writes.detached[result.write]
	delete(m.writes.detached, result.write)

	var cmds []tea.Cmd
	if m.writes.held.owns(result.write) {
		m.writes.held = heldWrite{}

		switch {
		case landed.Replaces:
		case landed.Err == nil || m.form.ClosesOnRefusal:
			cmds = append(cmds, m.closeForm())
		default:
			m.form.refusal = errui.GetReason(landed.Err)
			m.messages.setNewestHint("fix and submit again")
		}
	}

	// A refusal is reported into the region under the record, so the form is told to lay itself
	// out again.
	if m.form.isActive() {
		cmds = append(cmds, formResized())
	}

	if made := landed.Made; made != nil && !walkedAway {
		cmds = append(cmds, m.landOnMade(*made, result.keyMoved))
	}

	m.landWrite(result.write)

	if landed.Then != nil {
		cmds = append(cmds, m.route(landed.Then))
	}

	return tea.Batch(cmds...)
}

// landOnMade takes the panel to the row a write made, showing its pane.
func (m *model) landOnMade(made screen.Made, keyMoved bool) tea.Cmd {
	switch {
	case made.Archived && keyMoved:
		return m.enterArchived(screen.Target{})
	case made.Archived:
		return m.enterArchived(screen.Target{AgentID: made.Agent})
	}

	m.agents.kind = agentsLive
	cmds := []tea.Cmd{m.enterScreen(screen.Agents)}

	m.agents.showHiddenFor(m, made.Agent)

	rows := m.agents.rows(m)
	if key := findLandingKey(rows, m.agents.list().hovered(m), made.Row); !keyMoved && list.IndexOf(rows, key) >= 0 {
		m.agents.apply(m, key)
	}

	if made.Pane == "" {
		m.stageSelection()

		return tea.Batch(cmds...)
	}

	pane := made.Pane
	cmds = append(cmds, m.agents.closeEdit(m))
	m.frame.Solo(false)
	m.frame.ShowGuest(layout.Guest{Agent: made.Agent, Pane: pane})
	delete(m.warm.since, pane)

	if made.Focus {
		cmds = append(cmds, func() tea.Msg { return focusGuestMsg{pane: pane} })
	}

	return tea.Batch(cmds...)
}
