package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"weave/internal/caller"
	"weave/internal/harness"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildCommand covers the command typed into the pane. Every launch also carries
// the MCP config, and a fresh one appends a system prompt, so each case states the
// leading command rather than the whole line.
func TestBuildCommand(t *testing.T) {
	tests := []struct {
		name        string
		launch      harness.Launch
		wantCommand string
	}{
		{
			name:        "a resume pins the prior conversation by id",
			launch:      harness.Launch{SessionID: "sess-1", Resume: true},
			wantCommand: "WEAVE_SESSION_ID='sess-1' claude --resume 'sess-1'",
		},
		{
			name:        "a fresh launch pins the id it was given",
			launch:      harness.Launch{SessionID: "sess-2"},
			wantCommand: "WEAVE_SESSION_ID='sess-2' claude --session-id 'sess-2'",
		},
		{
			name:        "flags precede the session id",
			launch:      harness.Launch{Flags: "--model opus", SessionID: "sess-3"},
			wantCommand: "WEAVE_SESSION_ID='sess-3' claude '--model' 'opus' --session-id 'sess-3'",
		},
		{
			name:        "the agent's own ids lead the line, so they reach claude's environment",
			launch:      harness.Launch{AgentID: "id-1", SessionID: "sess-4"},
			wantCommand: "WEAVE_AGENT_ID='id-1' WEAVE_SESSION_ID='sess-4' claude --session-id 'sess-4'",
		},
		{
			// The appended system prompt carrying it would not, which is the whole reason
			// it rides on the environment instead.
			name:        "and they survive a resume",
			launch:      harness.Launch{AgentID: "id-1", SessionID: "sess-5", Resume: true},
			wantCommand: "WEAVE_AGENT_ID='id-1' WEAVE_SESSION_ID='sess-5' claude --resume 'sess-5'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			launch := tt.launch
			launch.CacheDir = t.TempDir()

			command, err := NewHarness(NewHarnessParams{ProjectsDir: t.TempDir()}).BuildCommand(launch)
			require.NoError(t, err)

			assert.True(t, strings.HasPrefix(stripMCPConfig(command), tt.wantCommand),
				"want %q to start with %q", stripMCPConfig(command), tt.wantCommand)
		})
	}
}

// TestBuildCommandQuotesFlags confirms the quoting is actually applied on the path that
// types into the pane, not just available to it.
func TestBuildCommandQuotesFlags(t *testing.T) {
	tests := []struct {
		name        string
		flags       string
		wantAbsent  string
		wantPresent string
	}{
		{
			name:        "a command substitution reaches the pane as a literal",
			flags:       "--model $(touch /tmp/pwned)",
			wantAbsent:  "claude --model $(",
			wantPresent: `'$(touch'`,
		},
		{
			name:        "a chained command reaches the pane as a literal",
			flags:       "--model opus; touch /tmp/pwned",
			wantAbsent:  "; touch /tmp/pwned",
			wantPresent: `'opus;'`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command, err := NewHarness(NewHarnessParams{ProjectsDir: t.TempDir()}).BuildCommand(harness.Launch{Flags: tt.flags, SessionID: "sess", CacheDir: t.TempDir()})
			require.NoError(t, err)

			assert.NotContains(t, command, tt.wantAbsent, "an unquoted flag would run in the pane")
			assert.Contains(t, command, tt.wantPresent, "the flag survives only as a literal argument")
		})
	}
}

// TestBuildCommandPassesTextThroughTempFiles covers the prompt and the appended system
// prompt. Both are sourced from a file the shell cats and removes, so markdown
// carrying quotes and newlines survives the trip.
func TestBuildCommandPassesTextThroughTempFiles(t *testing.T) {
	tests := []struct {
		name        string
		launch      harness.Launch
		wantContain []string
		wantAbsent  []string
	}{
		{
			name:       "a resume appends no system prompt, since it would not persist",
			launch:     harness.Launch{SessionID: "sess", Context: "the buildBriefing", Resume: true},
			wantAbsent: []string{"--append-system-prompt"},
		},
		{
			name:        "a multi-line prompt is fenced off from the variadic config flag",
			launch:      harness.Launch{SessionID: "sess", Prompt: "line one\nline two"},
			wantContain: []string{"--session-id 'sess'", "$(cat ", "; rm ", ` -- "$(cat `},
		},
		{
			name:        "the context the panel handed over is what gets appended",
			launch:      harness.Launch{SessionID: "sess", Context: "the buildBriefing"},
			wantContain: []string{"--append-system-prompt"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			launch := tt.launch
			launch.CacheDir = t.TempDir()

			command, err := NewHarness(NewHarnessParams{ProjectsDir: t.TempDir()}).BuildCommand(launch)
			require.NoError(t, err)

			for _, want := range tt.wantContain {
				assert.Contains(t, command, want)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, command, absent)
			}

			if tt.launch.Context != "" && !tt.launch.Resume {
				assert.Equal(t, tt.launch.Context, appendedPrompt(t, command),
					"the text reaches the file the command sources")
			}
		})
	}
}

// TestBuildCommandStagesTextUnderTheCacheDir covers where the files a launch passes text
// through are written. They hold the agent's prompt and are removed by the command that
// reads them, so they belong under the directory weave was told to keep what it generates
// in rather than wherever the system temp directory happens to be.
func TestBuildCommandStagesTextUnderTheCacheDir(t *testing.T) {
	cache := t.TempDir()

	_, err := NewHarness(NewHarnessParams{ProjectsDir: t.TempDir()}).BuildCommand(harness.Launch{
		SessionID: "sess",
		Context:   "the buildBriefing",
		Prompt:    "do the thing",
		CacheDir:  cache,
	})
	require.NoError(t, err)

	staged, err := filepath.Glob(filepath.Join(cache, "claude-*.md"))
	require.NoError(t, err)
	assert.Len(t, staged, 2, "one file for the appended system prompt and one for the opening prompt")
}

// TestBuildCommandRegistersMCPServer covers the config file every launch points claude
// at. It is passed without --strict-mcp-config, so the agent gains this server on
// top of whatever the user has registered rather than trading them for it.
func TestBuildCommandRegistersMCPServer(t *testing.T) {
	tests := []struct {
		name   string
		resume bool
	}{
		{
			name:   "on a resume, since the buildBriefing describing it does not persist",
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

			command, err := NewHarness(NewHarnessParams{ProjectsDir: t.TempDir()}).BuildCommand(harness.Launch{
				CacheDir:    t.TempDir(),
				SessionID:   "sess",
				MCPEndpoint: "127.0.0.1:9999",
				Resume:      tt.resume,
			})
			require.NoError(t, err)

			assert.NotContains(t, command, "--strict-mcp-config", "the user's own servers survive")

			match := mcpConfigRef.FindStringSubmatch(command)
			require.Len(t, match, 2, "the launch carries a config path")
			assert.Equal(t, filepath.Join(home, ".claude", "weave-mcp.json"), match[1])

			raw, err := os.ReadFile(match[1])
			require.NoError(t, err)

			var config struct {
				MCPServers map[string]struct {
					Type string `json:"type"`
					URL  string `json:"url"`
				} `json:"mcpServers"`
			}
			require.NoError(t, json.Unmarshal(raw, &config))

			server, ok := config.MCPServers["weave"]
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

// TestMCPConfigCarriesTheAgentsIdentity covers the headers every call an agent makes back
// to the panel arrives with, which is how the service says which agent asked for something
// rather than only that something on loopback did.
//
// They are written as variables rather than as this agent's ids, which is what lets every
// agent share one config file: Claude expands them out of the environment the panel
// launched that agent with, so each one calls back as itself.
func TestMCPConfigCarriesTheAgentsIdentity(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	command, err := NewHarness(NewHarnessParams{ProjectsDir: t.TempDir()}).BuildCommand(harness.Launch{
		CacheDir:    t.TempDir(),
		AgentID:     "agent-1",
		SessionID:   "sess-1",
		MCPEndpoint: "127.0.0.1:9999",
	})
	require.NoError(t, err)

	match := mcpConfigRef.FindStringSubmatch(command)
	require.Len(t, match, 2)
	assert.Equal(t, filepath.Join(home, ".claude", "weave-mcp.json"), match[1],
		"one file for every agent, since the headers name no agent")

	raw, err := os.ReadFile(match[1])
	require.NoError(t, err)

	var config struct {
		MCPServers map[string]struct {
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	require.NoError(t, json.Unmarshal(raw, &config))

	headers := config.MCPServers["weave"].Headers
	assert.Equal(t, "${WEAVE_AGENT_ID:-}", headers[caller.AgentHeader])
	assert.Equal(t, "${WEAVE_SESSION_ID:-}", headers[caller.SessionHeader])

	// The variables the headers name are the ones the launch exports, or every call an
	// agent makes arrives anonymous.
	assert.Contains(t, command, "WEAVE_AGENT_ID='agent-1'")
	assert.Contains(t, command, "WEAVE_SESSION_ID='sess-1'")
}
