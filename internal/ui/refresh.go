package ui

import (
	"context"
	"sync"
	"time"

	"weave/internal/client"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"

	tea "github.com/charmbracelet/bubbletea"
)

// refreshInterval is how often the panel re-lists over RPC. It runs off the
// render loop, so a slow serve can never freeze the UI.
const (
	refreshInterval = time.Second
	agentsTimeout   = 2 * time.Second
)

// refreshTickMsg is the periodic prompt to re-list. It and a write landing are the only
// things that put rows on screen, so what the panel shows always came from a listing
// rather than a guess about what a write did.
type refreshTickMsg struct{}

func refreshTick() tea.Cmd {
	return tea.Tick(refreshInterval, func(time.Time) tea.Msg { return refreshTickMsg{} })
}

// refreshResult is one listing pass, everything the panel draws. Each listing carries its
// own error, so one that failed costs its own rows and not the ones that answered, and
// what it would have replaced is left alone rather than blanked.
type refreshResult struct {
	// epoch is the write generation the pass was requested under. A pass older than the
	// panel's own read the store before a write this panel has since made, so its rows
	// would put back what that write changed.
	epoch int

	agents    []agentItem
	agentsErr error

	workflows    []workflowstore.Workflow
	workflowsErr error

	schedules    []scheduleItem
	schedulesErr error

	// archived is listed only while the archive screen is up. archivedListed says it was
	// asked for, so an empty one reads as an empty archive rather than a skipped pass.
	archived       []agentstore.Agent
	archivedErr    error
	archivedListed bool
}

// refreshCmd lists everything the panel draws, off the UI goroutine so the render loop
// never blocks on the RPCs. The listings run concurrently under one deadline, so a pass
// costs the slowest rather than their sum.
//
// A write in flight stands the pass down: it would read the rows that write is about to
// replace, and every write asks for a pass of its own as it lands.
func (m model) refreshCmd() tea.Cmd {
	if m.pending > 0 {
		return nil
	}

	clients, epoch, withArchive := m.api, m.epoch, m.screen == screenArchive

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
		}
		wg.Wait()

		return result
	}
}

// applyRefresh puts a finished pass on screen. A pass the panel has moved past is dropped
// whole, since its listings were all read from the same store at the same moment.
func (m *model) applyRefresh(result refreshResult) {
	if result.epoch < m.epoch {
		return
	}

	m.unreachable = result.err()

	if result.workflowsErr == nil {
		// Workflows go first, since the tree's rows, and so the cursor the agents are
		// reapplied against, are built over them.
		m.applyWorkflows(result.workflows)
	}
	if result.agentsErr == nil {
		m.applyAgents(result.agents)
	}
	if result.schedulesErr == nil {
		m.applySchedules(result.schedules)
	}
	if result.archivedListed && result.archivedErr == nil {
		m.applyArchive(result.archived)
	}

	// A pass can change what the cursor is sitting on without the cursor moving, so the
	// stage is redrawn from the merged lists. It is a no-op when what it shows has not
	// changed, which keeps this from respawning its pane every tick.
	switch m.screen {
	case screenAgents:
		m.stageSelection()
	case screenSchedules:
		m.stageSchedule()
	case screenArchive:
		m.stageArchive()
	default:
		// Search, the actions list, and the forms put the stage to their own use.
	}
}

// err is the first listing in the pass that did not answer, or nil when they all did.
// Every listing goes to the same server, so one failing is the server not answering.
func (r refreshResult) err() error {
	switch {
	case r.agentsErr != nil:
		return r.agentsErr
	case r.workflowsErr != nil:
		return r.workflowsErr
	case r.schedulesErr != nil:
		return r.schedulesErr
	default:
		return r.archivedErr
	}
}

// writeResult is a write that has landed. Every write the panel makes resolves to one of
// these, so reporting a refusal and re-listing after a success happen in one place.
type writeResult struct {
	// summary is what the banner says if the write was refused. err is nil when it was
	// not, which clears whatever the last one said.
	summary string
	err     error
	// restored is set by the one write whose result the panel needs, and nil for the rest.
	restored *restoredAgent
}

// restoredAgent is what a restore hands back: the pane to stage and the name to put the
// cursor on, which may not be the one the agent went in with. screen is where the person
// lands, since a restore from the archive leaves them there while one from the tree or
// search lands on the agent itself.
type restoredAgent struct {
	pane   string
	name   string
	screen screen
}

// writeCmd runs a write off the render loop and resolves to the message that reports it,
// so a wedged server costs the write rather than every frame until it answers. It takes
// the service clients rather than the panel, so nothing the render loop is still editing
// crosses onto the other goroutine.
func (m *model) writeCmd(summary string, write func(api) error) tea.Cmd {
	clients := m.api
	m.beginWrite()

	return func() tea.Msg {
		return writeResult{summary: summary, err: write(clients)}
	}
}

// restoreCmd restores an agent off the render loop and carries back what the panel needs
// of the agent that came back. stay is the screen to land on once it does.
func (m *model) restoreCmd(summary, id string, stay screen) tea.Cmd {
	clients := m.api
	m.beginWrite()

	return func() tea.Msg {
		agent, err := clients.restoreAgent(id)
		if err != nil {
			return writeResult{summary: summary, err: err}
		}

		return writeResult{
			summary:  summary,
			restored: &restoredAgent{pane: agent.GetPaneId(), name: agent.GetName(), screen: stay},
		}
	}
}

// beginWrite records that a write is on its way to the store. The generation it bumps
// retires the passes already in flight, which would put back what this write changes.
func (m *model) beginWrite() {
	m.epoch++
	m.pending++
}

// applyWrite reports a landed write and hands back the pass that shows what it did. A
// refused write re-lists too, since what it left behind is a question for the store.
//
// A form waiting on its write closes only once the write was accepted. A refused one stays
// on screen under the banner with its fields as they were.
func (m model) applyWrite(result writeResult) (tea.Model, tea.Cmd) {
	m.pending--
	m.report(result.summary, result.err)

	if result.restored != nil {
		m.screen = result.restored.screen
		if result.restored.screen == screenAgents {
			// The pane came back with the agent, so it stages now. The cursor cannot move
			// until a row carries the name it came back with, which is the pass below.
			m.tree.unpin()
			m.tree.focus = result.restored.name
			m.stage.Agent(result.restored.pane)
		}
	}

	if m.pendingForm {
		if result.err == nil {
			closed, cmd := m.closeForm()
			if next, ok := closed.(model); ok {
				m = next
			}

			return m, tea.Batch(cmd, m.refreshCmd())
		}

		// esc backs out of the form rather than clearing the banner, so the hint says what
		// actually clears this one.
		m.banner.Hint = "fix and submit again"
	}

	return m, m.refreshCmd()
}
