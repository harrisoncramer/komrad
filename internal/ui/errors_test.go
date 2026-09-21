package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"weave/config"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/layout"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"

	"connectrpc.com/connect"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

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

func (r refusingAgents) HoldAgent(context.Context, *connect.Request[agentv1.HoldAgentRequest]) (*connect.Response[agentv1.HoldAgentResponse], error) {
	return nil, r.err
}

func (r refusingAgents) ReleaseAgent(context.Context, *connect.Request[agentv1.ReleaseAgentRequest]) (*connect.Response[agentv1.ReleaseAgentResponse], error) {
	return nil, r.err
}

func (r refusingAgents) ListAgents(context.Context, *connect.Request[agentv1.ListAgentsRequest]) (*connect.Response[agentv1.ListAgentsResponse], error) {
	return connect.NewResponse(&agentv1.ListAgentsResponse{}), nil
}

// TestRefusedWriteIsReported covers a refused write, which the reloading list would otherwise
// make look exactly like a keypress that never registered.
func TestRefusedWriteIsReported(t *testing.T) {
	agent := agentstore.Agent{
		Name:       "api-refactor",
		State:      agentstore.StateIdle,
		WorkflowID: "w1",
	}

	m := model{
		api: api{
			agents:    refusingAgents{err: connect.NewError(connect.CodeFailedPrecondition, errors.New("already archived"))},
			workflows: emptyLists{},
		},
		tmux:   &recordingTmux{},
		frame:  layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		screen: screenAgents,
		listing: listing{
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "backend"}},
			agents:    []agentItem{{agent: agent, alive: false}},
		},
	}
	m.tree.cursor = firstAgentRowIndex(m.agentRows())

	opened, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	pressed, ok := opened.(model)
	require.True(t, ok)

	pressed.form.form.State = huh.StateCompleted
	updated, cmd := pressed.form.update(pressed, keyMsg("enter"))
	pressed, ok = updated.(model)
	require.True(t, ok)

	m = settle(t, pressed, cmd)

	require.NotEmpty(t, m.messages.items, "a refused archive is reported rather than swallowed")
	assert.Equal(t, `Could not archive agent "api-refactor"`, newestMessage(m).summary)
	assert.Equal(t, "already archived", newestMessage(m).detail,
		"the service's message is shown rather than the connect code in front of it")
}

// TestRefusedResumeIsReported covers a refused resume, which leaves the row dormant and so
// changes nothing on screen without the banner.
func TestRefusedResumeIsReported(t *testing.T) {
	m := model{
		api: api{
			agents:    refusingAgents{err: connect.NewError(connect.CodeNotFound, errors.New("no such agent"))},
			workflows: emptyLists{},
		},
		tmux:   &recordingTmux{},
		frame:  layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		screen: screenAgents,
		listing: listing{
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "backend"}},
			agents: []agentItem{{agent: agentstore.Agent{
				Name:       "api-refactor",
				State:      agentstore.StateIdle,
				WorkflowID: "w1",
			}, alive: false}},
		},
	}
	m.tree.cursor = firstAgentRowIndex(m.agentRows())

	updated, cmd := m.openAgent()
	pressed, ok := updated.(model)
	require.True(t, ok)

	got := settle(t, pressed, cmd)

	require.NotEmpty(t, got.messages.items)
	assert.Equal(t, `Could not resume agent "api-refactor"`, newestMessage(got).summary)
	assert.Equal(t, "no such agent", newestMessage(got).detail,
		"the wrapper the panel put around the call is not what the reader is shown")
}

// TestMessageSurvivesTheFormItWasReportedFrom covers a failure reported by a form.
func TestMessageSurvivesTheFormItWasReportedFrom(t *testing.T) {
	m := model{
		messages: oneMessage(`Could not rename agent "api-refactor"`, "name is taken"),
		screen:   screenAgents,
		keys:     config.DefaultKeybindings(),
		frame:    layout.New(&recordingTmux{}, config.DefaultLayout()),
	}
	opened, _ := m.openRename(agentstore.Agent{ID: "a1", Name: "api-refactor"})
	m = asModel(t, opened)

	assert.Contains(t, m.View(), "Rename", "the form is on screen")
	require.Len(t, m.messages.items, 1, "the refusal is still waiting to be read")
	assert.Equal(t, "name is taken", newestMessage(m).detail)
}

type acceptingAgents struct {
	agentv1connect.AgentServiceClient
}

func (acceptingAgents) ArchiveAgents(context.Context, *connect.Request[agentv1.ArchiveAgentsRequest]) (*connect.Response[emptypb.Empty], error) {
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (acceptingAgents) ListAgents(context.Context, *connect.Request[agentv1.ListAgentsRequest]) (*connect.Response[agentv1.ListAgentsResponse], error) {
	return connect.NewResponse(&agentv1.ListAgentsResponse{}), nil
}

// TestSuccessfulWriteReportsNothing covers a write that worked saying nothing of its own, and
// leaving whatever was reported before it in the stack rather than taking it away unread.
func TestSuccessfulWriteReportsNothing(t *testing.T) {
	m := model{
		api:    api{agents: acceptingAgents{}, workflows: emptyLists{}},
		tmux:   &recordingTmux{},
		frame:  layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		screen: screenAgents,
		listing: listing{
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "backend"}},
			agents: []agentItem{
				{
					agent: agentstore.Agent{
						Name:       "api-refactor",
						State:      agentstore.StateIdle,
						WorkflowID: "w1",
					},
				},
			},
		},
		messages: oneMessage("Could not archive agent \"gone\"", "stale"),
	}
	m.tree.cursor = firstAgentRowIndex(m.agentRows())

	opened, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	pressed, ok := opened.(model)
	require.True(t, ok)

	pressed.form.form.State = huh.StateCompleted
	updated, cmd := pressed.form.update(pressed, keyMsg("enter"))
	pressed, ok = updated.(model)
	require.True(t, ok)

	got := settle(t, pressed, cmd)

	assert.Len(t, got.messages.items, 1, "a write that succeeded says nothing and takes nothing away")
}

// TestMessageDrawsInTheRegion covers a refused write reporting without taking the screen.
func TestMessageDrawsInTheRegion(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.height, m.width = 40, 60
	m.messages = oneMessage(`Could not archive agent "api-refactor"`, "already archived")

	view := ansi.Strip(m.View())

	assert.Contains(t, view, "Could not archive agent")
	assert.Contains(t, view, "already archived")
	assert.Contains(t, view, "to read", "the way into the whole message is said")
	assert.Contains(t, view, m.listing.agents[0].agent.Name, "the list is still on screen above the region")
}

// TestMessagesYieldToTakeoverScreens pins a takeover screen coming before the message region.
func TestMessagesYieldToTakeoverScreens(t *testing.T) {
	tests := []struct {
		name string
		m    model
		want string
	}{
		{
			name: "a startup failure takes the screen from the region",
			m:    model{messages: oneMessage("Could not delete", "nope"), startupErr: errors.New("refused")},
			want: "The agent server could not be started",
		},
		{
			name: "and so does a config error",
			m:    model{messages: oneMessage("Could not delete", "nope"), configErr: errors.New("bad")},
			want: "Your configuration could not be read",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			view := tt.m.View()

			assert.Contains(t, view, tt.want)
			assert.NotContains(t, view, "Could not delete")
		})
	}
}

// TestUnreachableReportsOnce covers the server going away being said in the region, on the pass
// that finds it gone and not on every refresh after it.
func TestUnreachableReportsOnce(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.height, m.width = 40, 60
	m.addr = "127.0.0.1:8787"

	m.applyRefresh(refreshResult{agentsErr: errors.New("connection refused")})
	require.Len(t, m.messages.items, 1)
	assert.Contains(t, m.messages.items[0].summary, "Cannot reach the server")
	assert.Contains(t, m.messages.items[0].detail, "127.0.0.1:8787")

	m.applyRefresh(refreshResult{agentsErr: errors.New("connection refused")})
	assert.Len(t, m.messages.items, 1, "a server still down says nothing more")

	assert.Contains(t, ansi.Strip(m.View()), "Cannot reach the server")
}

// TestMessagesKeepEscOffTheList covers esc staying the list's own key until the region holds
// the keys.
func TestMessagesKeepEscOffTheList(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.height, m.width = 40, 60
	m.messages = oneMessage("Could not delete", "nope")

	onList := send(t, m, "esc")
	assert.NotEmpty(t, onList.messages.items, "the list's own esc leaves the region alone")

	inRegion := send(t, send(t, m, messagesFocusKey), "esc")
	assert.Empty(t, inRegion.messages.items, "the region's esc dismisses what it is showing")
}

// TestConfigErrorView covers the screen shown when the config file will not parse, which is
// all the reader gets and so has to carry the error and name the file to fix.
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
				configErr:   errors.New("failed to parse config /home/u/.config/weave/config.toml: keybindings.list is not a setting; [keybindings] takes agents, archive, filter, form, help, nav, schedules, selection, workflows"),
				unreachable: tt.unreachable,
				addr:        "127.0.0.1:8787",
				listing:     listing{agents: []agentItem{{agent: agentstore.Agent{Name: "should-not-render", State: agentstore.StateIdle}}}},
			}

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

// TestConfigErrorScreenIsTerminal covers nothing running behind the screen.
func TestConfigErrorScreenIsTerminal(t *testing.T) {
	m := model{configErr: errors.New("failed to parse config: bad")}

	assert.Nil(t, m.Init(), "no poll is started behind the error screen")

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	require.NotNil(t, cmd, "any key dismisses the screen")
	assert.Equal(t, tea.Quit(), cmd(), "and dismissing quits")
}

// TestStartupErrorView covers the screen shown when the server could not be started, which
// is the only thing on screen and so is the only place the failure can be read.
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
				listing:     listing{agents: []agentItem{{agent: agentstore.Agent{Name: "should-not-render", State: agentstore.StateIdle}}}},
			}

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

// TestStartupErrorScreenIsTerminal covers nothing running behind the screen.
func TestStartupErrorScreenIsTerminal(t *testing.T) {
	m := model{startupErr: errors.New("the agent server stopped during startup")}

	assert.Nil(t, m.Init(), "no poll is started behind the error screen")

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	require.NotNil(t, cmd, "any key dismisses the screen")
	assert.Equal(t, tea.Quit(), cmd(), "and dismissing quits")
}

// TestUnreachableView covers the service no longer answering.
func TestUnreachableView(t *testing.T) {
	m := detailsModel(&recordingTmux{})
	m.height, m.width = 40, 80
	m.addr = "127.0.0.1:8787"

	down, _ := m.Update(refreshResult{agentsErr: errors.New("dial tcp 127.0.0.1:8787: connect: connection refused")})
	m, ok := down.(model)
	require.True(t, ok)

	assert.Contains(t, ansi.Strip(m.View()), "Cannot reach", "the failure is reported in the region")
	assert.NotContains(t, ansi.Strip(m.View()), "The panel could not list your agents", "and no screen is taken over")

	back, _ := m.Update(refreshResult{agents: []agentItem{{agent: agentstore.Agent{Name: "back", State: agentstore.StateIdle}}}})
	m, ok = back.(model)
	require.True(t, ok)

	require.NoError(t, m.unreachable)
	assert.Len(t, m.messages.items, 1, "the message stays until it is dismissed")

	again, _ := m.Update(refreshResult{agentsErr: errors.New("connection refused")})
	m, ok = again.(model)
	require.True(t, ok)

	assert.Len(t, m.messages.items, 2, "the server going away a second time is said again")
}

// TestRefusedPauseIsReported covers the pause weave would not claim, which leaves the agent
// running and so changes nothing on screen without the message.
func TestRefusedPauseIsReported(t *testing.T) {
	m := model{
		api: api{
			agents: refusingAgents{err: connect.NewError(connect.CodeFailedPrecondition,
				errors.New(`"telescope" would not stop`))},
			workflows: emptyLists{},
		},
		tmux:   &recordingTmux{},
		frame:  layout.New(&recordingTmux{}, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		screen: screenAgents,
		listing: listing{
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "backend"}},
			agents: []agentItem{{agent: agentstore.Agent{
				Name:       "telescope",
				State:      agentstore.StateThinking,
				WorkflowID: "w1",
			}, alive: true}},
		},
	}
	m.tree.cursor = firstAgentRowIndex(m.agentRows())

	pressed, cmd := m.Update(keyMsg("p"))
	written, ok := pressed.(model)
	require.True(t, ok)

	written = settle(t, written, cmd)

	require.NotEmpty(t, written.messages.items, "a refused hold is reported rather than swallowed")
	assert.Equal(t, `Could not pause agent "telescope"`, newestMessage(written).summary)
	assert.Equal(t, `"telescope" would not stop`, newestMessage(written).detail)
}
