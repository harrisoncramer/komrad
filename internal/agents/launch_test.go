package agents

import (
	"errors"
	"os"
	"path/filepath"
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
			launcher := NewLauncher(LauncherParams{Tmux: tmux, Addr: "127.0.0.1:9999"})

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
	launcher := NewLauncher(LauncherParams{Tmux: tmux, Addr: "127.0.0.1:9999"})

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
	// The buildBriefing is handed over on a resume too. A harness that can only append a
	// system prompt drops it, but one that can re-inject it should.
	assert.Contains(t, launch.Context, "http://127.0.0.1:9999/mcp")
}

// briefedLogPath stands in for the service's log, which the launcher is handed rather than
// finding it for itself.
const briefedLogPath = "/var/cache/weave/weave.log"

// TestAppendedSystemPrompt covers what a spawned agent is told. The buildBriefing
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
			name: "an agent outside any workflow gets the buildBriefing alone",
			wantContain: []string{
				"http://127.0.0.1:9999/mcp",
				"list_tables",
				"agent_transcripts",
				// The buildBriefing has to name both halves or the agent has an id it never uses
				// and a tool it does not know to reach for.
				"WEAVE_AGENT_ID",
				"set_agent_metadata",
				// An agent told to read the service's log is told the file this install
				// writes, rather than a path it would have to work out for itself.
				briefedLogPath,
			},
			// A placeholder that reached an agent would send it looking for a path or an
			// endpoint that does not exist, which is worse than saying nothing.
			wantAbsent: []string{"shared notes", mcpURLPlaceholder, logPathPlaceholder},
		},
		{
			name:        "an agent inside one gets the workflow's context after it",
			context:     "shared notes",
			wantContain: []string{"http://127.0.0.1:9999/mcp", "shared notes"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			launcher := NewLauncher(LauncherParams{
				Tmux:    newFakeTmux(),
				Addr:    "127.0.0.1:9999",
				LogPath: briefedLogPath,
			})

			prompt := launcher.appendedSystemPrompt(tt.context, "", "")

			for _, want := range tt.wantContain {
				assert.Contains(t, prompt, want)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, prompt, absent)
			}

			if tt.context != "" {
				assert.Less(t, strings.Index(prompt, "weave"), strings.Index(prompt, tt.context),
					"the buildBriefing comes first")
			}
		})
	}
}

// TestAppendedSystemPromptDelegateBriefing covers what a spawned agent is told about
// whatever agent started it. weave writes this itself so every delegate gets the same
// wording whether or not the agent that spawned it thought to say so in the prompt.
func TestAppendedSystemPromptDelegateBriefing(t *testing.T) {
	tests := []struct {
		name             string
		spawnedByAgentID string
		spawnedByName    string
		wantContain      []string
		wantAbsent       []string
	}{
		{
			name:       "an agent a person started carries no delegate briefing",
			wantAbsent: []string{"spawned by"},
		},
		{
			name:             "a spawned agent is told who spawned it and to report back",
			spawnedByAgentID: "parent-id-1",
			spawnedByName:    "reviewer",
			wantContain:      []string{"spawned by agent reviewer (parent-id-1)", "Answer its messages", "tell it when you are done"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			launcher := NewLauncher(LauncherParams{
				Tmux:    newFakeTmux(),
				Addr:    "127.0.0.1:9999",
				LogPath: briefedLogPath,
			})

			prompt := launcher.appendedSystemPrompt("", tt.spawnedByAgentID, tt.spawnedByName)

			for _, want := range tt.wantContain {
				assert.Contains(t, prompt, want)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, prompt, absent)
			}
		})
	}
}

// TestAppendedSystemPromptGlobalContext covers the standing text the user keeps for every
// agent. It sits between the buildBriefing and the workflow's own, so the three read general to
// specific, and a file that is not there is the ordinary case rather than a failed launch.
func TestAppendedSystemPromptGlobalContext(t *testing.T) {
	tests := []struct {
		name string
		// global is written to the context file, or the file is left absent when empty.
		global string
		// missing points the launcher at a path nothing was written to.
		missing         bool
		workflowContext string
		wantContain     []string
		wantAbsent      []string
	}{
		{
			name:            "a path with no file behind it is not an error",
			missing:         true,
			workflowContext: "shared notes",
			wantContain:     []string{"shared notes"},
		},
		{
			name:            "a file of only whitespace adds nothing",
			global:          "   \n\t\n",
			workflowContext: "shared notes",
			wantContain:     []string{"shared notes"},
		},
		{
			name:        "the standing text reaches an agent in no workflow",
			global:      "always use tabs",
			wantContain: []string{"always use tabs"},
		},
		{
			name:            "both are given, the workflow's last",
			global:          "always use tabs",
			workflowContext: "shared notes",
			wantContain:     []string{"always use tabs", "shared notes"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "context.md")
			if !tt.missing {
				require.NoError(t, os.WriteFile(path, []byte(tt.global), 0o600))
			}

			launcher := NewLauncher(LauncherParams{
				Tmux:        newFakeTmux(),
				Addr:        "127.0.0.1:9999",
				LogPath:     briefedLogPath,
				ContextPath: path,
			})

			prompt := launcher.appendedSystemPrompt(tt.workflowContext, "", "")

			for _, want := range tt.wantContain {
				assert.Contains(t, prompt, want)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, prompt, absent)
			}

			// General to specific, which is what makes a workflow the last word on
			// anything the standing text also speaks to.
			if tt.global != "" && tt.workflowContext != "" {
				assert.Less(t, strings.Index(prompt, tt.global), strings.Index(prompt, tt.workflowContext))
			}
		})
	}
}

// TestGlobalContextIsReadPerLaunch covers editing the file while the service runs. The
// path is fixed at startup, so a launcher that read the text once would hand every later
// agent whatever was there when the service came up.
func TestGlobalContextIsReadPerLaunch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "context.md")
	require.NoError(t, os.WriteFile(path, []byte("before-the-edit"), 0o600))

	launcher := NewLauncher(LauncherParams{
		Tmux:        newFakeTmux(),
		Addr:        "127.0.0.1:9999",
		LogPath:     briefedLogPath,
		ContextPath: path,
	})
	require.Contains(t, launcher.appendedSystemPrompt("", "", ""), "before-the-edit")

	require.NoError(t, os.WriteFile(path, []byte("after-the-edit"), 0o600))

	prompt := launcher.appendedSystemPrompt("", "", "")
	assert.Contains(t, prompt, "after-the-edit")
	assert.NotContains(t, prompt, "before-the-edit")
}
