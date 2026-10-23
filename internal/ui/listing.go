package ui

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"komrad/internal/errui"
	"komrad/internal/ui/api"
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

type tickMsg struct {
	at time.Time
}

func (m *model) tick() tea.Cmd {
	return m.clock.After(tickInterval, func(at time.Time) tea.Msg { return tickMsg{at: at} })
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
		kind:    screen.Error,
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

// withListing changes the listing the screens read from and tells each screen it changed.
func (m *model) withListing(change func(*panel.Listing)) tea.Cmd {
	m.frame.Begin()
	defer m.frame.Flush()

	was := m.listing
	change(&m.listing)
	m.forgetUnlistedInspections()
	relisted, _ := m.routeResult(screen.Relisted{Was: was})
	m.sizeAgentWindows()

	// A form renders its options from the listing it was built over, so it is rendered again when
	// that listing moves under it.
	var rerendered tea.Cmd
	if !reflect.DeepEqual(was.Workflows, m.listing.Workflows) || !slices.Equal(was.DocumentTypeNames(), m.listing.DocumentTypeNames()) {
		rerendered = m.rerenderOpenForm()
	}

	return tea.Batch(relisted, rerendered, m.finishParked(), m.afterListing())
}

// afterListing asks for what the hovered rows are owed and stages the screen over them.
func (m *model) afterListing() tea.Cmd {
	inspect := tea.Batch(m.apply(m.inspectTaskWorktree(m.clock.Now())), m.readMovedLastScreenCmd())

	// A screen that renders what it holds beside its own list stages nothing at all.
	active := m.active()
	var staged tea.Cmd
	if s, ok := active.(rowStager); ok && active.DoesRenderFromListing() && !m.form.isActive() {
		staged = m.apply(s.Stage())
	}

	m.restageBeside()

	return tea.Batch(inspect, staged)
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

// rowWrite is a write whose landing takes the panel to the row it made.
func rowWrite(clients api.Clients, doing, summary string, write func(api.Clients) (*screen.Made, error)) screen.Write {
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
func agentWrite(clients api.Clients, doing, summary string, write func(api.Clients) (*agentv1.Agent, error)) screen.Write {
	return rowWrite(clients, doing, summary, madeAgentOf(write, false))
}

// openingWrite is agentWrite for a write that puts a conversation back in a pane, which
// ends with the keyboard in that pane the way the open key over a live agent does.
func openingWrite(clients api.Clients, doing, summary string, write func(api.Clients) (*agentv1.Agent, error)) screen.Write {
	return rowWrite(clients, doing, summary, madeAgentOf(write, true))
}

// madeAgentOf turns a write answering with an agent into one answering with the agent's row.
func madeAgentOf(write func(api.Clients) (*agentv1.Agent, error), focus bool) func(api.Clients) (*screen.Made, error) {
	return func(clients api.Clients) (*screen.Made, error) {
		agent, err := write(clients)
		if err != nil {
			return nil, err
		}

		return madeAgent(agent, focus), nil
	}
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

func retryWrite(clients api.Clients, agent panel.Agent, values retryFormValues) screen.Write {
	return agentWrite(clients, "retrying "+agent.Name, fmt.Sprintf("Could not retry agent %q", agent.Name), func(clients api.Clients) (*agentv1.Agent, error) {
		return clients.RetryAgent(api.RetryAgentParams{
			ID:      agent.ID,
			Flags:   values.Flags,
			Prompt:  values.Prompt,
			Harness: values.Harness,
		})
	})
}

func createWrite(clients api.Clients, params api.CreateAgentParams) screen.Write {
	return agentWrite(clients, startingLabel(params.Title), "Could not start the agent", func(clients api.Clients) (*agentv1.Agent, error) {
		return clients.CreateAgent(params)
	})
}

func startingLabel(title string) string {
	if title = strings.TrimSpace(title); title != "" {
		return "starting " + title
	}

	return "starting the agent"
}

func adoptWrite(clients api.Clients, params api.AdoptParams) screen.Write {
	return screen.Write{
		Doing:   "adopting " + params.Ref,
		Summary: "Could not adopt",
		Send: func() screen.Landed {
			adopted, err := clients.Adopt(params)
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

func createWorkflowWrite(clients api.Clients, cfg panel.WorkflowConfig) screen.Write {
	return rowWrite(clients, "creating workflow "+cfg.Name, "Could not create the workflow", func(clients api.Clients) (*screen.Made, error) {
		workflow, err := clients.CreateWorkflow(cfg)
		if err != nil {
			return nil, err
		}

		// A new workflow comes with the agent that stands behind it, and stands on its own when
		// that agent will not start.
		agent, err := clients.StartOrchestrator(workflow.GetId())
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
	m.writes.inflight[m.writes.count] = submitting.Write{Doing: doing, Since: m.clock.Now()}

	return m.writes.count
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

	if len(result.unstaged) > 0 {
		m.settleUnstaged(result.unstaged, landed.Killed)
	}

	if landed.Notice != nil && landed.Err == nil {
		m.pushMessage(message{
			kind:    screen.Info,
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
		return m.apply(m.enterArchived(screen.Target{}))
	case made.Archived:
		return m.apply(m.enterArchived(screen.Target{AgentID: made.Agent}))
	}

	m.agents.kind = agentsLive
	cmds := []tea.Cmd{m.enterScreen(screen.Agents)}

	m.agents.showHiddenFor(m.listing, made.Agent)

	rows := m.agents.rows(m.inputs())
	if key := findLandingKey(rows, m.agents.list().Hovered(m.inputs()), made.Row); !keyMoved && list.IndexOf(rows, key) >= 0 {
		m.agents.apply(m.inputs(), key)
	}

	if made.Pane == "" {
		m.stageSelection()

		return tea.Batch(cmds...)
	}

	pane := made.Pane
	cmds = append(cmds, m.apply(m.agents.dropEdit()))
	m.frame.Solo(false)
	m.frame.ShowGuest(layout.Guest{Agent: made.Agent, Pane: pane})
	delete(m.warm.since, pane)

	if made.Focus {
		cmds = append(cmds, func() tea.Msg { return focusGuestMsg{pane: pane} })
	}

	return tea.Batch(cmds...)
}
