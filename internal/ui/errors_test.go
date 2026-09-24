package ui_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"weave/internal/engine"
	"weave/internal/ui"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"

	tea "charm.land/bubbletea/v2"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

const serverAddr = "127.0.0.1:8787"

// TestErrorsRefusedWrites covers a refused write being said in the message region.
func TestErrorsRefusedWrites(t *testing.T) {
	tests := []struct {
		name        string
		agent       *agentv1.Agent
		refused     string
		reason      string
		press       []string
		wantSummary string
	}{
		{
			name:        "a refused archive",
			agent:       newDormant("api-refactor"),
			refused:     agentv1connect.AgentServiceArchiveAgentsProcedure,
			reason:      "already archived",
			press:       []string{"j", "z", "ctrl+s"},
			wantSummary: `Could not archive agent "api-refactor"`,
		},
		{
			name:        "a refused resume",
			agent:       newDormant("api-refactor"),
			refused:     agentv1connect.AgentServiceResumeAgentProcedure,
			reason:      "no such agent",
			press:       []string{"j", "enter"},
			wantSummary: `Could not resume agent "api-refactor"`,
		},
		{
			name:        "a refused pause",
			agent:       newPausable(""),
			refused:     agentv1connect.AgentServiceHoldAgentProcedure,
			reason:      `"telescope" would not stop`,
			press:       []string{"j", "p"},
			wantSummary: `Could not pause agent "telescope"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openAgents(t, newLiveWorkflow("w1", "backend"), tt.agent)
			harness.refuse(tt.refused, connect.NewError(connect.CodeFailedPrecondition, errors.New(tt.reason)))

			harness.press(append(tt.press, widenListPresses...)...)

			frame := harness.readFrame()
			assert.Contains(t, frame, tt.wantSummary)
			assert.Contains(t, frame, tt.reason, "the service's reason is shown rather than the connect code in front of it")
			assert.NotEmpty(t, readHoveredRow(frame), "the list is still on screen above the region")
		})
	}

	t.Run("a write that lands says nothing and takes nothing away", func(t *testing.T) {
		harness := openAgents(t, newLiveWorkflow("w1", "backend"), newDormant("api-refactor"))
		harness.refuse(agentv1connect.AgentServiceResumeAgentProcedure, connect.NewError(connect.CodeNotFound, errors.New("no such agent")))

		harness.press("j", "enter", "z", "ctrl+s")
		harness.press(widenListPresses...)

		frame := harness.readFrame()
		assert.Contains(t, frame, `Could not resume agent "api-refactor"`)
		assert.NotContains(t, frame, "+1", "the archive that landed pushed nothing onto the stack")
	})

	t.Run("a message outlives the form opened over it", func(t *testing.T) {
		harness := openAgents(t, newLiveWorkflow("w1", "backend"), newDormant("api-refactor"))
		harness.refuse(agentv1connect.AgentServiceResumeAgentProcedure, connect.NewError(connect.CodeNotFound, errors.New("no such agent")))

		harness.press("j", "enter", "r")
		require.Contains(t, harness.readFrame(), "Rename")

		harness.press("esc")

		assert.Contains(t, harness.readFrame(), "no such agent")
	})
}

// TestErrorsRegion covers the keys of the message region.
func TestErrorsRegion(t *testing.T) {
	harness := openAgents(t, newLiveWorkflow("w1", "backend"), newDormant("api-refactor"))
	harness.refuse(agentv1connect.AgentServiceResumeAgentProcedure, connect.NewError(connect.CodeNotFound, errors.New("no such agent")))
	harness.press("j", "enter")

	assert.Contains(t, harness.readFrame(), "to read", "the way into the whole message is said")

	harness.press("esc")
	assert.Contains(t, harness.readFrame(), "no such agent", "the list's own esc leaves the region alone")

	harness.press("ctrl+g", "esc")
	assert.Contains(t, harness.readFrame(), "No messages", "the region's esc dismisses what it is showing")
}

// TestErrorsUnreachable covers the server going away being said once per time it goes.
func TestErrorsUnreachable(t *testing.T) {
	t.Run("the tree already drawn stays drawn", func(t *testing.T) {
		harness := openUnreachable(t, engine.Remote{})

		frame := harness.readFrame()
		assert.Contains(t, frame, "Cannot reach the")
		assert.Equal(t, []string{"backend"}, readListRows(frame))
	})

	t.Run("a stream lost twice in a row is said once", func(t *testing.T) {
		harness := openUnreachable(t, engine.Remote{})

		harness.send(ui.NewWatchLost(errors.New("connection refused")))

		frame := harness.readFrame()
		assert.Contains(t, frame, "Cannot reach the")
		assert.NotContains(t, frame, "+1", "a server still down says nothing more")
		assert.NotContains(t, frame, "The panel could not list your agents", "and no screen is taken over")
	})

	t.Run("a stream that came back and went again is said again", func(t *testing.T) {
		harness := openUnreachable(t, engine.Remote{})

		harness.watch(buildSnapshot(newLiveWorkflow("w1", "backend")))
		harness.send(ui.NewWatchLost(errors.New("connection refused")))

		assert.Contains(t, harness.readFrame(), "+1")
	})

	tests := []struct {
		name   string
		remote engine.Remote
		want   string
	}{
		{
			name: "the hint on this machine says how to start a server",
			want: "Start it with: weave serve",
		},
		{
			name:   "the hint for a remote engine says the tunnel is dialled again",
			remote: engine.Remote{Destination: "weave@host"},
			want:   "The tunnel to the engine is dialled again until it answers",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openUnreachable(t, tt.remote)

			harness.press("ctrl+g")

			frame := strings.Join(strings.Fields(harness.readFrame()), " ")
			assert.Contains(t, frame, serverAddr)
			assert.Contains(t, frame, tt.want)
		})
	}
}

// TestErrorsFatalScreens covers the screens that take the whole panel when it cannot start.
func TestErrorsFatalScreens(t *testing.T) {
	configErr := errors.New("failed to parse config /home/u/.config/weave/config.toml: keybindings.list is not a setting")
	startupErr := errors.New("the agent server stopped during startup: failed to reach db: connection refused")

	tests := []struct {
		name        string
		configErr   error
		startupErr  error
		wantContain []string
		wantAbsent  []string
	}{
		{
			name:        "a config error names the file to fix",
			configErr:   configErr,
			wantContain: []string{"Your configuration could not be read", "keybindings.list is not a setting", "config.toml"},
			wantAbsent:  []string{"Cannot reach the server"},
		},
		{
			name:        "a startup failure carries the reason the server gave",
			startupErr:  startupErr,
			wantContain: []string{"The agent server could not be started", "connection refused"},
			wantAbsent:  []string{"Cannot reach the server"},
		},
		{
			name:        "a config error outranks a startup failure, having happened first",
			configErr:   configErr,
			startupErr:  startupErr,
			wantContain: []string{"Your configuration could not be read"},
			wantAbsent:  []string{"The agent server could not be started"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := newPanelHarness(t, panelHarnessParams{configErr: tt.configErr, startupErr: tt.startupErr})
			require.Nil(t, harness.panel.Init(), "nothing runs behind the screen")

			harness.watch(buildSnapshot(newLiveWorkflow("w1", "should-not-render")))
			harness.send(ui.NewWatchLost(errors.New("connection refused")))

			frame := strings.Join(strings.Fields(harness.readFrame()), " ")
			for _, want := range tt.wantContain {
				assert.Contains(t, frame, want)
			}
			for _, absent := range append(tt.wantAbsent, "should-not-render") {
				assert.NotContains(t, frame, absent)
			}

			assert.True(t, doesQuit(harness.panel.Update(ui.NewKeyPress("x"))), "any key dismisses the screen and quits")
		})
	}
}

func openUnreachable(t *testing.T, remote engine.Remote) *panelHarness {
	t.Helper()

	harness := newPanelHarness(t, panelHarnessParams{
		settings: ui.PanelSettings{Addr: serverAddr},
		engine:   remote,
	})
	harness.watch(buildSnapshot(newLiveWorkflow("w1", "backend")))
	harness.send(ui.NewWatchLost(errors.New("connection refused")))

	return harness
}

func doesQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}

	switch msg := cmd().(type) {
	case tea.QuitMsg:
		return true
	case tea.BatchMsg:
		return slices.ContainsFunc(msg, doesQuit)
	default:
		return false
	}
}

func newDormant(name string) *agentv1.Agent {
	return &agentv1.Agent{
		Id:         "a1",
		Name:       name,
		WorkflowId: "w1",
		State:      agentv1.AgentState_AGENT_STATE_IDLE,
		Alive:      proto.Bool(false),
	}
}
