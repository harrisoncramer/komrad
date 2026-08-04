package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"weave/config"
	"weave/internal/errui"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"

	"connectrpc.com/connect"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// refusingAgents refuses every write and lists nothing. The embedded interface is
// nil, so a method the test does not need panics rather than quietly returning a
// zero value and hiding which call the code under test actually made.
type refusingAgents struct {
	agentv1connect.AgentServiceClient
	err error
}

func (r refusingAgents) ArchiveAgents(context.Context, *connect.Request[agentv1.ArchiveAgentsRequest]) (*connect.Response[emptypb.Empty], error) {
	return nil, r.err
}

func (r refusingAgents) RestoreAgents(context.Context, *connect.Request[agentv1.RestoreAgentsRequest]) (*connect.Response[agentv1.RestoreAgentsResponse], error) {
	return nil, r.err
}

func (r refusingAgents) ResumeAgent(context.Context, *connect.Request[agentv1.ResumeAgentRequest]) (*connect.Response[agentv1.ResumeAgentResponse], error) {
	return nil, r.err
}

func (r refusingAgents) ListAgents(context.Context, *connect.Request[agentv1.ListAgentsRequest]) (*connect.Response[agentv1.ListAgentsResponse], error) {
	return connect.NewResponse(&agentv1.ListAgentsResponse{}), nil
}

// TestRefusedWriteIsReported covers the case the panel used to swallow. Every
// write discarded its error, and since the list reloads straight after, a refused
// archive looked exactly like a keypress that never registered.
func TestRefusedWriteIsReported(t *testing.T) {
	agent := agentstore.Agent{Name: "api-refactor", State: agentstore.StateIdle, WorkflowID: "w1"}

	m := model{
		api: api{
			agents:    refusingAgents{err: connect.NewError(connect.CodeFailedPrecondition, errors.New("already archived"))},
			workflows: emptyLists{},
		},
		tmux:   &recordingTmux{},
		stage:  NewStage(&recordingTmux{}, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		screen: screenAgents,
		tree: treeState{
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "backend"}},
			agents:    []agentItem{{agent: agent, alive: false}},
		},
	}
	m.tree.cursor = firstAgentRowIndex(m.agentRows())

	opened, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	pressed, ok := opened.(model)
	require.True(t, ok)

	*pressed.values.confirmed = true
	pressed.form.State = huh.StateCompleted
	updated, cmd := pressed.updateAgentArchive(keyMsg("enter"))
	pressed, ok = updated.(model)
	require.True(t, ok)

	m = settle(t, pressed, cmd)

	alert, ok := m.newestAlert()
	require.True(t, ok, "a refused archive is reported rather than swallowed")
	assert.Equal(t, `Could not archive agent "api-refactor"`, alert.Summary)
	assert.Equal(t, "already archived", alert.Detail,
		"the service's message is shown rather than the connect code in front of it")
}

// TestRefusedResumeIsReported covers the other write the agents screen makes. A
// refused resume leaves the row dormant, which is how it looked before the key was
// pressed, so without the banner nothing on screen changed.
func TestRefusedResumeIsReported(t *testing.T) {
	m := model{
		api: api{
			agents:    refusingAgents{err: connect.NewError(connect.CodeNotFound, errors.New("no such agent"))},
			workflows: emptyLists{},
		},
		tmux:   &recordingTmux{},
		stage:  NewStage(&recordingTmux{}, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		screen: screenAgents,
		tree: treeState{
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "backend"}},
			agents: []agentItem{
				{agent: agentstore.Agent{Name: "api-refactor", State: agentstore.StateIdle, WorkflowID: "w1"}, alive: false},
			},
		},
	}
	m.tree.cursor = firstAgentRowIndex(m.agentRows())

	updated, cmd := m.openAgent()
	pressed, ok := updated.(model)
	require.True(t, ok)

	got := settle(t, pressed, cmd)

	alert, ok := got.newestAlert()
	require.True(t, ok)
	assert.Equal(t, `Could not resume agent "api-refactor"`, alert.Summary)
	assert.Equal(t, "no such agent", alert.Detail,
		"the wrapper the panel put around the call is not what the reader is shown")
}

// TestAlertSurvivesTheFormItWasReportedFrom covers a failure reported by a form,
// which returns to the agents screen as it closes. The alert is chrome above
// whatever screen the panel lands on, so it outlives the form that caused it.
func TestAlertSurvivesTheFormItWasReportedFrom(t *testing.T) {
	m := model{
		screen: screenRename,
		keys:   config.DefaultKeybindings(),
	}
	m.alerts.push(errui.LevelError, `Could not rename agent "api-refactor"`, "name is taken")
	m.form, m.values.rename = renameForm("api-refactor", "", m.keys.Form)
	m.form.Init()

	view := m.View()

	assert.Contains(t, view, "Could not rename agent")
	assert.Contains(t, view, "name is taken")
	assert.Contains(t, view, "Rename", "the form is still on screen under the alert")
}

// acceptingAgents accepts writes, for the case where a write succeeds alongside an
// already-queued alert.
type acceptingAgents struct {
	agentv1connect.AgentServiceClient
}

func (acceptingAgents) ArchiveAgents(context.Context, *connect.Request[agentv1.ArchiveAgentsRequest]) (*connect.Response[emptypb.Empty], error) {
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (acceptingAgents) ListAgents(context.Context, *connect.Request[agentv1.ListAgentsRequest]) (*connect.Response[agentv1.ListAgentsResponse], error) {
	return connect.NewResponse(&agentv1.ListAgentsResponse{}), nil
}

// TestSuccessfulWriteLeavesEarlierAlertsQueued covers the point of the queue over the single
// slot it replaced: a second failure used to silently destroy the first, and a success used
// to clear it outright. Neither happens now, since report only ever pushes on a refusal.
func TestSuccessfulWriteLeavesEarlierAlertsQueued(t *testing.T) {
	m := model{
		api:    api{agents: acceptingAgents{}, workflows: emptyLists{}},
		tmux:   &recordingTmux{},
		stage:  NewStage(&recordingTmux{}, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		screen: screenAgents,
		tree: treeState{
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "backend"}},
			agents: []agentItem{
				{agent: agentstore.Agent{Name: "api-refactor", State: agentstore.StateIdle, WorkflowID: "w1"}},
			},
		},
	}
	m.alerts.push(errui.LevelError, `Could not archive agent "gone"`, "stale")
	m.tree.cursor = firstAgentRowIndex(m.agentRows())

	opened, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	pressed, ok := opened.(model)
	require.True(t, ok)

	*pressed.values.confirmed = true
	pressed.form.State = huh.StateCompleted
	updated, cmd := pressed.updateAgentArchive(keyMsg("enter"))
	pressed, ok = updated.(model)
	require.True(t, ok)

	got := settle(t, pressed, cmd)

	alert, ok := got.newestAlert()
	require.True(t, ok, "the earlier alert is still queued, since a success does not clear it")
	assert.Equal(t, `Could not archive agent "gone"`, alert.Summary)
}

// TestAlertRendersOverTheList covers that an alert reports without taking the screen,
// unlike the config and unreachable screens. The row it is about is in the list
// underneath, so hiding the list would lose the context.
func TestAlertRendersOverTheList(t *testing.T) {
	m := model{
		screen: screenAgents,
		keys:   config.DefaultKeybindings(),
		tree: treeState{
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "backend"}},
			agents: []agentItem{
				{agent: agentstore.Agent{Name: "api-refactor", State: agentstore.StateIdle, WorkflowID: "w1"}},
			},
		},
	}
	m.alerts.push(errui.LevelError, `Could not archive agent "api-refactor"`, "already archived")

	view := m.View()

	assert.Contains(t, view, "Could not archive agent")
	assert.Contains(t, view, "already archived")
	assert.Contains(t, view, "esc to dismiss")
	assert.Contains(t, view, "api-refactor", "the list is still on screen under the alert")
}

// TestAlertYieldsToTakeoverScreens pins the precedence. An alert over a list that
// cannot be trusted would be reporting one failed write while every row behind it was
// stale.
func TestAlertYieldsToTakeoverScreens(t *testing.T) {
	tests := []struct {
		name        string
		unreachable error
		configErr   error
		want        string
	}{
		{
			name:        "an unreachable service takes the screen from the alert",
			unreachable: errors.New("refused"),
			want:        "Cannot reach the server",
		},
		{
			name:      "and so does a config error",
			configErr: errors.New("bad"),
			want:      "Your configuration could not be read",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := model{unreachable: tt.unreachable, configErr: tt.configErr}
			m.alerts.push(errui.LevelError, "Could not delete", "")

			view := m.View()

			assert.Contains(t, view, tt.want)
			assert.NotContains(t, view, "Could not delete")
		})
	}
}

// TestAlertDismissal covers what dismisses the alert on screen. It is chrome rather
// than a screen, so esc dismisses it, but a form owns its own esc and has to keep it.
func TestAlertDismissal(t *testing.T) {
	tests := []struct {
		name      string
		screen    screen
		key       tea.KeyMsg
		wantClear bool
	}{
		{
			name:      "esc dismisses the alert",
			screen:    screenAgents,
			key:       tea.KeyMsg{Type: tea.KeyEsc},
			wantClear: true,
		},
		{
			name:   "a navigation key leaves it up",
			screen: screenAgents,
			key:    tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}},
		},
		{
			name:   "and a form keeps its own esc",
			screen: screenRename,
			key:    tea.KeyMsg{Type: tea.KeyEsc},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := model{
				screen: tt.screen,
				keys:   config.DefaultKeybindings(),
				tmux:   &recordingTmux{},
				stage:  NewStage(&recordingTmux{}, config.DefaultLayout()),
			}
			m.alerts.push(errui.LevelError, "Could not delete", "nope")
			if tt.screen == screenRename {
				m.form, m.values.rename = renameForm("api-refactor", "", m.keys.Form)
			}

			updated, _ := m.Update(tt.key)
			got, ok := updated.(model)
			require.True(t, ok)

			_, stillThere := got.newestAlert()
			assert.Equal(t, !tt.wantClear, stillThere)
		})
	}
}

// TestAlertQueueReplacement covers the point of the queue over the single slot it
// replaced: a second alert no longer destroys the first, it renders over it and the
// first is revealed again once the second is dismissed.
func TestAlertQueueReplacement(t *testing.T) {
	var q alertQueue

	q.push(errui.LevelError, "first failure", "")
	q.push(errui.LevelError, "second failure", "")

	alert, behind, ok := q.newest()
	require.True(t, ok)
	assert.Equal(t, "second failure", alert.Summary, "the newest alert is what renders")
	assert.Equal(t, 1, behind, "the first is queued behind it rather than gone")

	q.dismissNewest()

	alert, behind, ok = q.newest()
	require.True(t, ok)
	assert.Equal(t, "first failure", alert.Summary, "dismissing reveals what was behind it")
	assert.Equal(t, 0, behind)
}

// TestAlertQueueCoalescing covers identical alerts collapsing into one entry with a
// count, rather than stacking a duplicate a chatty caller would otherwise flood the
// queue with. Two alerts sharing a summary but not a detail are different failures,
// so they still queue separately.
func TestAlertQueueCoalescing(t *testing.T) {
	var q alertQueue

	q.push(errui.LevelWarn, "action failed", "timeout")
	q.push(errui.LevelWarn, "action failed", "timeout")
	q.push(errui.LevelWarn, "action failed", "timeout")

	alert, behind, ok := q.newest()
	require.True(t, ok)
	assert.Equal(t, 0, behind, "identical alerts coalesce rather than queueing a duplicate")
	assert.Equal(t, "action failed (×3)", alert.Summary, "the count is on the summary rather than hidden")

	q.push(errui.LevelWarn, "action failed", "a different reason")

	_, behind, ok = q.newest()
	require.True(t, ok)
	assert.Equal(t, 1, behind, "a matching summary with a different detail is a different failure")
}

// TestAlertQueueDismissOrder covers the order alerts are read in: newest first, and
// dismissing walks back to what happened first, last.
func TestAlertQueueDismissOrder(t *testing.T) {
	var q alertQueue

	q.push(errui.LevelError, "first", "")
	q.push(errui.LevelError, "second", "")
	q.push(errui.LevelError, "third", "")

	var seen []string
	for {
		alert, _, ok := q.newest()
		if !ok {
			break
		}
		seen = append(seen, alert.Summary)
		q.dismissNewest()
	}

	assert.Equal(t, []string{"third", "second", "first"}, seen)
	assert.Equal(t, []string{"first", "second", "third"}, historySummaries(q),
		"history holds them oldest-dismissed-last, newest-dismissed-first")
}

// historySummaries pulls the summaries off a queue's history, in the order it holds them.
func historySummaries(q alertQueue) []string {
	summaries := make([]string, len(q.history))
	for i, a := range q.history {
		summaries[i] = a.Summary
	}

	return summaries
}

// TestAlertQueueStaleTickDropped covers the safeguard an info alert's expiry needs: the
// tick is named for the sequence number it was scheduled against, so one landing after its
// alert already left the queue some other way is dropped rather than expiring whatever
// happens to be on screen by then.
func TestAlertQueueStaleTickDropped(t *testing.T) {
	var q alertQueue

	tick := q.push(errui.LevelInfo, "saved", "")
	require.NotNil(t, tick, "an info alert schedules its own expiry")
	staleSeq := q.queue[0].seq

	// The alert is dismissed by hand before its own tick ever fires, the way esc would.
	q.dismissNewest()

	// A second, unrelated alert takes the front the first alert's tick knows nothing about.
	q.push(errui.LevelError, "a later failure", "")

	q.expire(staleSeq)

	alert, _, ok := q.newest()
	require.True(t, ok, "the stale tick did not touch the alert actually on screen")
	assert.Equal(t, "a later failure", alert.Summary)
}

// TestAlertQueueExpiry covers an info alert leaving the queue on its own tick, landing in
// history so it is still readable even though nobody dismissed it.
func TestAlertQueueExpiry(t *testing.T) {
	var q alertQueue

	q.push(errui.LevelInfo, "saved", "")
	seq := q.queue[0].seq

	q.expire(seq)

	_, _, ok := q.newest()
	assert.False(t, ok, "the expired alert is off the queue")
	assert.Equal(t, []string{"saved"}, historySummaries(q), "and still readable in history")
}

// TestAlertQueuePushOnlySchedulesATickForInfo covers that warn and error alerts get no
// tick of their own, since they stay until esc dismisses them rather than expiring.
func TestAlertQueuePushOnlySchedulesATickForInfo(t *testing.T) {
	var q alertQueue

	assert.Nil(t, q.push(errui.LevelError, "an error", ""), "an error stays until dismissed")
	assert.Nil(t, q.push(errui.LevelWarn, "a warning", ""), "so does a warning")
	assert.NotNil(t, q.push(errui.LevelInfo, "an info", ""), "only info schedules its own expiry")
}

// TestAlertChromeStaysOneLineForTheQueueBehind covers the count line staying a single line
// no matter how deep the queue runs, since a queue that rendered a line per alert behind
// the newest would eat rows the list below it needs.
func TestAlertChromeStaysOneLineForTheQueueBehind(t *testing.T) {
	var m model
	for i := range 5 {
		m.alerts.push(errui.LevelError, fmt.Sprintf("failure %d", i), "")
	}

	chrome := m.alertChrome()

	assert.Equal(t, 1, strings.Count(chrome, "more queued"), "one line names how many are behind, not one per alert")
}

// TestConfigErrorView covers the screen shown when the config file will not parse.
// The panel does not start, so this screen is all the reader gets: it has to carry
// the error itself, name the file to go and fix, and show nothing that would read
// as a working panel.
func TestConfigErrorView(t *testing.T) {
	tests := []struct {
		name        string
		unreachable error
		wantContain []string
		wantAbsent  []string
	}{
		{
			name: "the error and the file it names reach the screen",
			wantContain: []string{
				"Your configuration could not be read",
				"keybindings.list is not a setting",
				"config.toml",
			},
			wantAbsent: []string{"should-not-render"},
		},
		{
			name:        "a config error outranks an unreachable service",
			unreachable: errors.New("connection refused"),
			wantContain: []string{"Your configuration could not be read"},
			wantAbsent:  []string{"Cannot reach the server"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := model{
				configErr:   errors.New("failed to parse config /home/u/.config/weave/config.toml: keybindings.list is not a setting; [keybindings] takes actions, agents, archive, filter, form, help, nav, schedules, selection, workflows"),
				unreachable: tt.unreachable,
				addr:        "127.0.0.1:8787",
				tree:        treeState{agents: []agentItem{{agent: agentstore.Agent{Name: "should-not-render", State: agentstore.StateIdle}}}},
			}

			// The error wraps at the screen width, so the text is compared with its
			// line breaks flattened away.
			view := strings.Join(strings.Fields(m.View()), " ")

			for _, want := range tt.wantContain {
				assert.Contains(t, view, want)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, view, absent)
			}
		})
	}
}

// TestConfigErrorScreenIsTerminal covers that nothing runs behind the screen and
// that any key ends the process. A panel that never read its config has no list to
// poll and nothing to come back to, so the screen is the whole life of the process.
func TestConfigErrorScreenIsTerminal(t *testing.T) {
	m := model{configErr: errors.New("failed to parse config: bad")}

	assert.Nil(t, m.Init(), "no poll is started behind the error screen")

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	require.NotNil(t, cmd, "any key dismisses the screen")
	assert.Equal(t, tea.Quit(), cmd(), "and dismissing quits")
}

// TestStartupErrorView covers the screen shown when the server could not be
// started. The panel is the only thing on screen at that point, so a failure it does
// not render is a failure nobody sees: the process is the command of the tmux
// session behind the popup, and exiting takes the pane the message was on with it.
func TestStartupErrorView(t *testing.T) {
	tests := []struct {
		name        string
		configErr   error
		unreachable error
		wantContain []string
		wantAbsent  []string
	}{
		{
			name: "the reason the server gave reaches the screen",
			wantContain: []string{
				"The agent server could not be started",
				"connection refused",
			},
			wantAbsent: []string{"should-not-render"},
		},
		{
			name:        "it outranks an unreachable service, which is what it caused",
			unreachable: errors.New("dial tcp: connection refused"),
			wantContain: []string{"The agent server could not be started"},
			wantAbsent:  []string{"Cannot reach the server"},
		},
		{
			name:        "a config error outranks it, having happened first",
			configErr:   errors.New("failed to parse config: bad"),
			wantContain: []string{"Your configuration could not be read"},
			wantAbsent:  []string{"The agent server could not be started"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := model{
				configErr:   tt.configErr,
				startupErr:  errors.New("the agent server stopped during startup: failed to reach db: connection refused"),
				unreachable: tt.unreachable,
				addr:        "127.0.0.1:8787",
				tree:        treeState{agents: []agentItem{{agent: agentstore.Agent{Name: "should-not-render", State: agentstore.StateIdle}}}},
			}

			// The error wraps at the screen width, so the text is compared with its
			// line breaks flattened away.
			view := strings.Join(strings.Fields(m.View()), " ")

			for _, want := range tt.wantContain {
				assert.Contains(t, view, want)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, view, absent)
			}
		})
	}
}

// TestStartupErrorScreenIsTerminal covers that nothing runs behind the screen and
// that any key ends the process. A panel whose server never started has nothing to
// poll: the server is started once, so there is nothing coming back to wait for.
func TestStartupErrorScreenIsTerminal(t *testing.T) {
	m := model{startupErr: errors.New("the agent server stopped during startup")}

	assert.Nil(t, m.Init(), "no poll is started behind the error screen")

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	require.NotNil(t, cmd, "any key dismisses the screen")
	assert.Equal(t, tea.Quit(), cmd(), "and dismissing quits")
}

// TestUnreachableView covers what the panel shows when the service stops
// answering. A swallowed poll would leave the last good list on screen, so a
// dead server would read as a panel whose agents had all been archived.
func TestUnreachableView(t *testing.T) {
	tests := []struct {
		name         string
		start        error
		msg          refreshResult
		wantTakeover bool
	}{
		{
			name: "while the service answers, the list renders as usual",
			msg:  refreshResult{agents: []agentItem{{agent: agentstore.Agent{Name: "still-here", State: agentstore.StateIdle}}}},
		},
		{
			name:         "a failed poll takes the screen over",
			msg:          refreshResult{agentsErr: errors.New("dial tcp 127.0.0.1:8787: connect: connection refused")},
			wantTakeover: true,
		},
		{
			name:  "and it clears itself once the service answers again",
			start: errors.New("connection refused"),
			msg:   refreshResult{agents: []agentItem{{agent: agentstore.Agent{Name: "back", State: agentstore.StateIdle}}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := model{
				addr:        "127.0.0.1:8787",
				unreachable: tt.start,
				tree:        treeState{agents: []agentItem{{agent: agentstore.Agent{Name: "still-here", State: agentstore.StateIdle}}}},
				stage:       NewStage(&recordingTmux{}, config.DefaultLayout()),
			}

			updated, _ := m.Update(tt.msg)
			m, ok := updated.(model)
			require.True(t, ok)

			view := m.View()
			if !tt.wantTakeover {
				assert.NoError(t, m.unreachable)
				assert.NotContains(t, view, "Cannot reach the server")

				return
			}

			assert.Contains(t, view, "Cannot reach the server")
			assert.Contains(t, view, "127.0.0.1:8787", "the address to go looking at is on screen")
			assert.NotContains(t, view, "still-here", "the stale list is not shown as if it were current")
		})
	}
}
