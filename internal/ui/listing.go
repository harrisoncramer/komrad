package ui

import (
	"reflect"
	"slices"
	"strings"
	"time"

	"komrad/internal/assemble"
	"komrad/internal/errui"
	"komrad/internal/ui/layout"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	"komrad/internal/ui/submitting"

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
func (m *model) withListing(change func(*panel.Listing) assemble.Changed) tea.Cmd {
	m.frame.Begin()
	defer m.frame.Flush()

	was := m.listing
	changed := change(&m.listing)
	m.forgetUnlistedInspections()
	relisted, _ := m.routeResult(screen.Relisted{Was: was, Changed: changed})
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

	m.agents.ShowLive()
	cmds := []tea.Cmd{m.enterScreen(screen.Agents)}
	cmds = append(cmds, m.apply(m.agents.Land(m.inputs(), made, keyMoved)))

	if made.Pane == "" {
		m.stageSelection()

		return tea.Batch(cmds...)
	}

	pane := made.Pane
	m.frame.Solo(false)
	m.frame.ShowGuest(layout.Guest{Agent: made.Agent, Pane: pane})
	delete(m.warm.since, pane)

	if made.Focus {
		cmds = append(cmds, func() tea.Msg { return focusGuestMsg{pane: pane} })
	}

	return tea.Batch(cmds...)
}
