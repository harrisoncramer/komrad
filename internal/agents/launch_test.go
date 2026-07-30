package agents

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLaunchTypesTheHarnessCommand covers the launcher's whole job: the command the
// harness built is typed into the agent's pane and run. What that command says is
// the harness's, and is covered where it is assembled.
func TestLaunchTypesTheHarnessCommand(t *testing.T) {
	tests := []struct {
		name string
		// commandErr makes the harness refuse to build a command line, and
		// sendKeysErr makes tmux refuse to type it.
		commandErr  error
		sendKeysErr error
		wantErr     string
	}{
		{
			name: "the command reaches the pane, followed by Enter",
		},
		{
			name:       "a harness that cannot build a command line launches nothing",
			commandErr: errors.New("no command for you"),
			wantErr:    "failed to build the launch command",
		},
		{
			name:        "a pane that refuses the keys fails the launch",
			sendKeysErr: errors.New("no such pane"),
			wantErr:     "failed to type the launch command",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := newFakeTmux()
			tmux.sendKeysErr = tt.sendKeysErr
			h := &fakeHarness{commandErr: tt.commandErr}
			launcher := NewLauncher(tmux, "127.0.0.1:9999")

			err := launcher.launch(h, launchParams{Pane: "pane", SessionID: "sess"})

			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				if tt.commandErr != nil {
					assert.Empty(t, tmux.sent, "a command that was never built is never typed")
				}

				return
			}

			require.NoError(t, err)
			require.Len(t, h.launches, 1)
			assert.Equal(t, []string{"agent-cli --session sess", "Enter"}, tmux.lastKeys("pane"))
		})
	}
}

// TestLaunchPassesWhatThePanelKnows covers what the launcher hands the harness.
// The panel decides what an agent is told and which conversation it is; how any of
// it reaches a command line is the harness's own business.
func TestLaunchPassesWhatThePanelKnows(t *testing.T) {
	tmux := newFakeTmux()
	h := &fakeHarness{}
	launcher := NewLauncher(tmux, "127.0.0.1:9999")

	require.NoError(t, launcher.launch(h, launchParams{
		Pane:      "pane",
		Flags:     "--model opus",
		Prompt:    "do the thing",
		SessionID: "sess",
		AgentID:   "id-1",
		Resume:    true,
	}))

	require.Len(t, h.launches, 1)
	launch := h.launches[0]
	assert.Equal(t, "--model opus", launch.Flags)
	assert.Equal(t, "do the thing", launch.Prompt)
	assert.Equal(t, "sess", launch.SessionID)
	assert.Equal(t, "id-1", launch.AgentID)
	assert.Equal(t, "127.0.0.1:9999", launch.MCPEndpoint, "the agent is told where the panel's own server is")
	assert.True(t, launch.Resume)
	// The briefing is handed over on a resume too. A harness that can only append a
	// system prompt drops it, but one that can re-inject it should.
	assert.Contains(t, launch.Context, "http://127.0.0.1:9999/mcp")
}

// TestAppendedSystemPrompt covers what a spawned agent is told. The briefing
// comes first so a workflow's own context reads as an addition to it rather than
// being buried inside it.
func TestAppendedSystemPrompt(t *testing.T) {
	tests := []struct {
		name        string
		context     string
		wantContain []string
		wantAbsent  []string
	}{
		{
			name: "an agent outside any workflow gets the briefing alone",
			wantContain: []string{
				"http://127.0.0.1:9999/mcp",
				"list_tables",
				"agent_transcripts",
				// The briefing has to name both halves or the agent has an id it never uses
				// and a tool it does not know to reach for.
				"CLAUDE_AGENTS_AGENT_ID",
				"set_agent_metadata",
			},
			wantAbsent: []string{"shared notes"},
		},
		{
			name:        "an agent inside one gets the workflow's context after it",
			context:     "shared notes",
			wantContain: []string{"http://127.0.0.1:9999/mcp", "shared notes"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			launcher := NewLauncher(newFakeTmux(), "127.0.0.1:9999")

			prompt := launcher.appendedSystemPrompt(tt.context)
			for _, want := range tt.wantContain {
				assert.Contains(t, prompt, want)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, prompt, absent)
			}

			if tt.context != "" {
				assert.Less(t, strings.Index(prompt, "weave"), strings.Index(prompt, tt.context),
					"the briefing comes first")
			}
		})
	}
}
