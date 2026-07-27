package agents

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLaunchCommand covers the command typed into the pane. Every launch also
// carries the MCP config, and a fresh one appends a system prompt, so each case
// states the leading command rather than the whole line.
func TestLaunchCommand(t *testing.T) {
	tests := []struct {
		name        string
		params      launchClaudeParams
		wantCommand string
	}{
		{
			name: "a resume pins the prior conversation by id",
			params: launchClaudeParams{
				Pane:            "pane",
				ClaudeSessionID: "sess-1",
				Resume:          true,
			},
			wantCommand: "claude --resume 'sess-1'",
		},
		{
			name: "a fresh launch pins the id it was given",
			params: launchClaudeParams{
				Pane:            "pane",
				ClaudeSessionID: "sess-2",
			},
			wantCommand: "claude --session-id 'sess-2'",
		},
		{
			name: "flags precede the session id",
			params: launchClaudeParams{
				Pane:            "pane",
				Flags:           "--model opus",
				ClaudeSessionID: "sess-3",
			},
			wantCommand: "claude '--model' 'opus' --session-id 'sess-3'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := newFakeTmux()
			manager := newManager(tmux, newStore(t), "")

			require.NoError(t, manager.launcher.launch(tt.params))

			keys := tmux.lastKeys("pane")
			require.Len(t, keys, 2)
			assert.True(t, strings.HasPrefix(stripMCPConfig(keys[0]), tt.wantCommand),
				"want %q to start with %q", stripMCPConfig(keys[0]), tt.wantCommand)
			assert.Equal(t, "Enter", keys[1])
		})
	}
}

// TestLaunchPassesTextThroughTempFiles covers the prompt and the appended system
// prompt. Both are sourced from a file the shell cats and removes, so markdown
// carrying quotes and newlines survives the trip.
func TestLaunchPassesTextThroughTempFiles(t *testing.T) {
	tests := []struct {
		name        string
		params      launchClaudeParams
		wantContain []string
		wantAbsent  []string
	}{
		{
			name: "a resume appends no system prompt, since it would not persist",
			params: launchClaudeParams{
				Pane:            "pane",
				ClaudeSessionID: "sess",
				WorkflowContext: "shared notes",
				Resume:          true,
			},
			wantAbsent: []string{"--append-system-prompt"},
		},
		{
			name: "a multi-line prompt is fenced off from the variadic config flag",
			params: launchClaudeParams{
				Pane:            "pane",
				ClaudeSessionID: "sess",
				Prompt:          "line one\nline two",
			},
			wantContain: []string{"--session-id 'sess'", "$(cat ", "; rm ", ` -- "$(cat `},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := newFakeTmux()
			manager := newManager(tmux, newStore(t), "")

			require.NoError(t, manager.launcher.launch(tt.params))

			command := tmux.lastKeys("pane")[0]
			for _, want := range tt.wantContain {
				assert.Contains(t, command, want)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, command, absent)
			}
		})
	}
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
			name:        "an agent outside any workflow gets the briefing alone",
			wantContain: []string{"http://127.0.0.1:9999/mcp", "list_tables", "agent_transcripts"},
			wantAbsent:  []string{"shared notes"},
		},
		{
			name:        "an agent inside one gets the workflow's context after it",
			context:     "shared notes",
			wantContain: []string{"http://127.0.0.1:9999/mcp", "shared notes"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := newFakeTmux()
			manager := newAgentService(tmux, newStore(t), "", "127.0.0.1:9999")

			require.NoError(t, manager.launcher.launch(launchClaudeParams{
				Pane:            "pane",
				ClaudeSessionID: "sess",
				WorkflowContext: tt.context,
			}))

			prompt := appendedPrompt(t, tmux.lastKeys("pane")[0])
			for _, want := range tt.wantContain {
				assert.Contains(t, prompt, want)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, prompt, absent)
			}

			if tt.context != "" {
				assert.Less(t, strings.Index(prompt, "claude-agents"), strings.Index(prompt, tt.context),
					"the briefing comes first")
			}
		})
	}
}

// TestLaunchRegistersMCPServer covers the config file every launch points claude
// at. It is passed without --strict-mcp-config, so the agent gains this server
// on top of whatever the user has registered rather than trading them for it.
func TestLaunchRegistersMCPServer(t *testing.T) {
	tests := []struct {
		name   string
		resume bool
	}{
		{
			name:   "on a resume, since the briefing describing it does not persist",
			resume: true,
		},
		{
			name: "on a fresh launch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)

			tmux := newFakeTmux()
			manager := newAgentService(tmux, newStore(t), "", "127.0.0.1:9999")

			require.NoError(t, manager.launcher.launch(launchClaudeParams{
				Pane:            "pane",
				ClaudeSessionID: "sess",
				Resume:          tt.resume,
			}))

			command := tmux.lastKeys("pane")[0]
			assert.NotContains(t, command, "--strict-mcp-config", "the user's own servers survive")

			match := mcpConfigRef.FindStringSubmatch(command)
			require.Len(t, match, 2, "the launch carries a config path")
			assert.Equal(t, filepath.Join(home, ".claude", "claude-agents-mcp.json"), match[1])

			raw, err := os.ReadFile(match[1])
			require.NoError(t, err)

			var config struct {
				MCPServers map[string]struct {
					Type string `json:"type"`
					URL  string `json:"url"`
				} `json:"mcpServers"`
			}
			require.NoError(t, json.Unmarshal(raw, &config))

			server, ok := config.MCPServers["claude-agents"]
			require.True(t, ok, "the panel's own server is registered")
			assert.Equal(t, "http", server.Type)
			assert.Equal(t, "http://127.0.0.1:9999/mcp", server.URL)
		})
	}
}

// mcpConfigRef matches the --mcp-config flag and the path it points at.
var mcpConfigRef = regexp.MustCompile(` --mcp-config '([^']+)'`)

// stripMCPConfig removes the MCP config flag from a launch command, so the
// assertions about the rest of the line are not written around a path that
// varies by machine.
func stripMCPConfig(command string) string {
	return mcpConfigRef.ReplaceAllString(command, "")
}

// appendedPromptRef matches the temp file the launch command sources its
// appended system prompt from.
var appendedPromptRef = regexp.MustCompile(`--append-system-prompt "\$\(cat '([^']+)'`)

// appendedPrompt returns the system prompt text the command appends. The shell
// removes the file as the command runs, but a test never runs it, so the file is
// still on disk to read.
func appendedPrompt(t *testing.T, command string) string {
	t.Helper()

	match := appendedPromptRef.FindStringSubmatch(command)
	require.Len(t, match, 2, "command should append a system prompt: %s", command)

	text, err := os.ReadFile(match[1])
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(match[1]) })

	return string(text)
}
