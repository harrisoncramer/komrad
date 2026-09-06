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

// TestLaunchTypesTheHarnessCommand covers the launcher's whole job, which is putting the
// command the harness built in a file and sourcing that file in the agent's pane.
func TestLaunchTypesTheHarnessCommand(t *testing.T) {
	tests := []struct {
		name        string
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
			cache := t.TempDir()
			tmux := newFakeTmux()
			tmux.sendKeysErr = tt.sendKeysErr
			h := &fakeHarness{commandErr: tt.commandErr}
			launcher := NewLauncher(LauncherParams{Tmux: tmux, Addr: "127.0.0.1:9999", CacheDir: cache})

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

			keys := tmux.lastKeys("pane")
			require.Len(t, keys, 2)
			assert.Equal(t, "Enter", keys[1])

			path, ok := strings.CutPrefix(keys[0], ". ")
			require.True(t, ok, "the pane is told to source a file, not typed the command")
			path = strings.Trim(path, "'")
			assert.Equal(t, cache, filepath.Dir(path), "the script sits in the cache dir")

			body, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, "agent-cli --session sess\n", string(body))
		})
	}
}

// TestLaunchPassesWhatThePanelKnows covers what the launcher hands the harness, which is
// what the agent is told and which conversation it is.
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

	assert.Contains(t, launch.Context, "http://127.0.0.1:9999/mcp")
}

const briefedLogPath = "/var/cache/weave/weave.log"

// TestAppendedSystemPrompt pins the buildBriefing coming first, so a workflow's own context
// reads as an addition to it rather than being buried inside it.
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

				"WEAVE_AGENT_ID",
				"set_agent_metadata",

				briefedLogPath,
			},
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

			prompt := launcher.appendedSystemPrompt(launchParams{WorkflowContext: tt.context})

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

// TestAppendedSystemPromptDelegateBriefing pins that weave writes the delegate briefing
// itself, so the wording is the same whether or not the spawning agent thought to say it.
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
			wantAbsent: []string{"You were spawned by"},
		},
		{
			name:             "a spawned agent is told who spawned it and to report back",
			spawnedByAgentID: "parent-id-1",
			spawnedByName:    "reviewer",
			wantContain:      []string{"spawned by agent reviewer (parent-id-1)", "Answer its mail", "tell it when you are done"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			launcher := NewLauncher(LauncherParams{
				Tmux:    newFakeTmux(),
				Addr:    "127.0.0.1:9999",
				LogPath: briefedLogPath,
			})

			prompt := launcher.appendedSystemPrompt(launchParams{SpawnedByAgentID: tt.spawnedByAgentID, SpawnedByName: tt.spawnedByName})

			for _, want := range tt.wantContain {
				assert.Contains(t, prompt, want)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, prompt, absent)
			}
		})
	}
}

// TestAppendedSystemPromptGlobalContext pins the user's standing text sitting between the
// buildBriefing and the workflow's own, with a missing file treated as ordinary.
func TestAppendedSystemPromptGlobalContext(t *testing.T) {
	tests := []struct {
		name            string
		global          string
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

			prompt := launcher.appendedSystemPrompt(launchParams{WorkflowContext: tt.workflowContext})

			for _, want := range tt.wantContain {
				assert.Contains(t, prompt, want)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, prompt, absent)
			}

			if tt.global != "" && tt.workflowContext != "" {
				assert.Less(t, strings.Index(prompt, tt.global), strings.Index(prompt, tt.workflowContext))
			}
		})
	}
}

// TestGlobalContextIsReadPerLaunch pins the file being read at each launch, since one read
// at startup would hand every later agent stale text.
func TestGlobalContextIsReadPerLaunch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "context.md")
	require.NoError(t, os.WriteFile(path, []byte("before-the-edit"), 0o600))

	launcher := NewLauncher(LauncherParams{
		Tmux:        newFakeTmux(),
		Addr:        "127.0.0.1:9999",
		LogPath:     briefedLogPath,
		ContextPath: path,
	})
	require.Contains(t, launcher.appendedSystemPrompt(launchParams{}), "before-the-edit")

	require.NoError(t, os.WriteFile(path, []byte("after-the-edit"), 0o600))

	prompt := launcher.appendedSystemPrompt(launchParams{})
	assert.Contains(t, prompt, "after-the-edit")
	assert.NotContains(t, prompt, "before-the-edit")
}

// TestBriefingCarriesTheReviewRules pins the review surface being part of what weave ships rather
// than something a person configures, since an agent that reaches for gh or mails its findings has
// given a review nobody can settle and the panel cannot show.
func TestBriefingCarriesTheReviewRules(t *testing.T) {
	launcher := NewLauncher(LauncherParams{Addr: "127.0.0.1:8787"})

	// No context file and no workflow, which is the least an install can give an agent.
	prompt := launcher.appendedSystemPrompt(launchParams{})

	for _, want := range []string{
		"open_review_round",
		"create_review_thread",
		"release_review",
		"add_review_message",
		"quotedLines",
	} {
		assert.Contains(t, prompt, want, "an agent is told the call by name")
	}

	assert.Contains(t, prompt, "`gh`", "and told plainly not to reach for the forge")
	assert.Contains(t, prompt, "the agent you are a child of",
		"and who it may review, since the rule is refused at the server either way")
}
