package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"weave/internal/appconfig"
	"weave/internal/errui"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"

	"connectrpc.com/connect"
	tea "github.com/charmbracelet/bubbletea"
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
		stage:  NewStage(&recordingTmux{}, appconfig.DefaultSplit),
		keys:   appconfig.DefaultKeybindings(),
		screen: screenAgents,
		tree: treeState{
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "backend"}},
			agents:    []agentItem{{agent: agent, alive: false}},
		},
	}
	m.tree.cursor = firstAgentRowIndex(m.agentRows())

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	pressed, ok := updated.(model)
	require.True(t, ok)

	m = settle(t, pressed, cmd)

	require.NotNil(t, m.banner, "a refused archive is reported rather than swallowed")
	assert.Equal(t, `Could not archive agent "api-refactor"`, m.banner.Summary)
	assert.Equal(t, "already archived", m.banner.Detail,
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
		stage:  NewStage(&recordingTmux{}, appconfig.DefaultSplit),
		keys:   appconfig.DefaultKeybindings(),
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

	require.NotNil(t, got.banner)
	assert.Equal(t, `Could not resume agent "api-refactor"`, got.banner.Summary)
	assert.Equal(t, "no such agent", got.banner.Detail,
		"the wrapper the panel put around the call is not what the reader is shown")
}

// TestBannerSurvivesTheFormItWasReportedFrom covers a failure reported by a form,
// which returns to the agents screen as it closes. The banner is chrome above
// whatever screen the panel lands on, so it outlives the form that caused it.
func TestBannerSurvivesTheFormItWasReportedFrom(t *testing.T) {
	m := model{
		banner: &errui.Banner{Summary: `Could not rename agent "api-refactor"`, Detail: "name is taken"},
		screen: screenRename,
		keys:   appconfig.DefaultKeybindings(),
	}
	m.form, m.values.rename = renameForm("api-refactor", "", m.keys.Form)
	m.form.Init()

	view := m.View()

	assert.Contains(t, view, "Could not rename agent")
	assert.Contains(t, view, "name is taken")
	assert.Contains(t, view, "Rename", "the form is still on screen under the banner")
}

// acceptingAgents accepts writes, for the case where a banner should clear.
type acceptingAgents struct {
	agentv1connect.AgentServiceClient
}

func (acceptingAgents) ArchiveAgents(context.Context, *connect.Request[agentv1.ArchiveAgentsRequest]) (*connect.Response[emptypb.Empty], error) {
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (acceptingAgents) ListAgents(context.Context, *connect.Request[agentv1.ListAgentsRequest]) (*connect.Response[agentv1.ListAgentsResponse], error) {
	return connect.NewResponse(&agentv1.ListAgentsResponse{}), nil
}

// TestSuccessfulWriteClearsTheBanner covers the other half of report. Clearing is
// folded into the same call that sets, so a write cannot succeed and leave the
// previous failure sitting on screen.
func TestSuccessfulWriteClearsTheBanner(t *testing.T) {
	m := model{
		api:    api{agents: acceptingAgents{}, workflows: emptyLists{}},
		tmux:   &recordingTmux{},
		stage:  NewStage(&recordingTmux{}, appconfig.DefaultSplit),
		keys:   appconfig.DefaultKeybindings(),
		screen: screenAgents,
		tree: treeState{
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "backend"}},
			agents: []agentItem{
				{agent: agentstore.Agent{Name: "api-refactor", State: agentstore.StateIdle, WorkflowID: "w1"}},
			},
		},
		banner: &errui.Banner{Summary: "Could not archive agent \"gone\"", Detail: "stale"},
	}
	m.tree.cursor = firstAgentRowIndex(m.agentRows())

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	pressed, ok := updated.(model)
	require.True(t, ok)

	got := settle(t, pressed, cmd)

	assert.Nil(t, got.banner, "a write that succeeded clears the previous failure")
}

// TestBannerRendersOverTheList covers that a refused write reports without
// taking the screen, unlike the config and unreachable screens. The row the error
// is about is in the list underneath, so hiding the list would lose the context.
func TestBannerRendersOverTheList(t *testing.T) {
	m := model{
		banner: &errui.Banner{Summary: `Could not archive agent "api-refactor"`, Detail: "already archived"},
		screen: screenAgents,
		keys:   appconfig.DefaultKeybindings(),
		tree: treeState{
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "backend"}},
			agents: []agentItem{
				{agent: agentstore.Agent{Name: "api-refactor", State: agentstore.StateIdle, WorkflowID: "w1"}},
			},
		},
	}

	view := m.View()

	assert.Contains(t, view, "Could not archive agent")
	assert.Contains(t, view, "already archived")
	assert.Contains(t, view, "esc to dismiss")
	assert.Contains(t, view, "api-refactor", "the list is still on screen under the banner")
}

// TestBannerYieldsToTakeoverScreens pins the precedence. A banner over a list
// that cannot be trusted would be reporting one failed write while every row behind
// it was stale.
func TestBannerYieldsToTakeoverScreens(t *testing.T) {
	tests := []struct {
		name string
		m    model
		want string
	}{
		{
			name: "an unreachable service takes the screen from the banner",
			m:    model{banner: &errui.Banner{Summary: "Could not delete"}, unreachable: errors.New("refused")},
			want: "Cannot reach the server",
		},
		{
			name: "and so does a config error",
			m:    model{banner: &errui.Banner{Summary: "Could not delete"}, configErr: errors.New("bad")},
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

// TestBannerDismissal covers what clears the banner. It is chrome rather than
// a screen, so esc dismisses it, but a form owns its own esc and has to keep it.
func TestBannerDismissal(t *testing.T) {
	tests := []struct {
		name      string
		screen    screen
		key       tea.KeyMsg
		wantClear bool
	}{
		{
			name:      "esc clears the banner",
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
				banner: &errui.Banner{Summary: "Could not delete", Detail: "nope"},
				screen: tt.screen,
				keys:   appconfig.DefaultKeybindings(),
				tmux:   &recordingTmux{},
				stage:  NewStage(&recordingTmux{}, appconfig.DefaultSplit),
			}
			if tt.screen == screenRename {
				m.form, m.values.rename = renameForm("api-refactor", "", m.keys.Form)
			}

			updated, _ := m.Update(tt.key)
			got, ok := updated.(model)
			require.True(t, ok)

			if tt.wantClear {
				assert.Nil(t, got.banner)
			} else {
				assert.NotNil(t, got.banner)
			}
		})
	}
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
				"invalid keys: list",
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
				configErr:   errors.New("failed to parse config /home/u/.config/weave/config.toml: 'keybindings' has invalid keys: list"),
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
				stage:       NewStage(&recordingTmux{}, appconfig.DefaultSplit),
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
