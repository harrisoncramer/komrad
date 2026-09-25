package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"komrad/internal/caller"
	"komrad/internal/harness"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildCommand covers the command the pane is started with, stating the leading command rather
// than the whole line since every launch also carries the MCP config.
func TestBuildCommand(t *testing.T) {
	tests := []struct {
		name        string
		launch      harness.Launch
		wantCommand string
	}{
		{
			name:        "a resume pins the prior conversation by id",
			launch:      harness.Launch{SessionID: "sess-1", Resume: true},
			wantCommand: "KOMRAD_SESSION_ID='sess-1' claude --resume 'sess-1'",
		},
		{
			name:        "a fresh launch pins the id it was given",
			launch:      harness.Launch{SessionID: "sess-2"},
			wantCommand: "KOMRAD_SESSION_ID='sess-2' claude --session-id 'sess-2'",
		},
		{
			name:        "flags precede the session id",
			launch:      harness.Launch{Flags: "--model opus", SessionID: "sess-3"},
			wantCommand: "KOMRAD_SESSION_ID='sess-3' claude '--model' 'opus' --session-id 'sess-3'",
		},
		{
			name:        "the agent's own ids lead the line, so they reach claude's environment",
			launch:      harness.Launch{AgentID: "id-1", SessionID: "sess-4"},
			wantCommand: "KOMRAD_AGENT_ID='id-1' KOMRAD_SESSION_ID='sess-4' claude --session-id 'sess-4'",
		},
		{
			name: "and they survive a resume",
			launch: harness.Launch{
				AgentID:   "id-1",
				SessionID: "sess-5",
				Resume:    true,
			},
			wantCommand: "KOMRAD_AGENT_ID='id-1' KOMRAD_SESSION_ID='sess-5' claude --resume 'sess-5'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			launch := tt.launch
			launch.CacheDir = t.TempDir()

			command, err := NewHarness(NewHarnessParams{ConfigDir: t.TempDir()}).BuildCommand(launch)
			require.NoError(t, err)

			assert.True(t, strings.HasPrefix(stripGeneratedFlags(command), tt.wantCommand),
				"want %q to start with %q", stripGeneratedFlags(command), tt.wantCommand)
		})
	}
}

// TestBuildCommandFailsWhenThePromptCannotBeWritten covers a command line built without the
// prompt it was given, which loses the ask for good once the caller clears the interrupted turn.
func TestBuildCommandFailsWhenThePromptCannotBeWritten(t *testing.T) {
	tests := []struct {
		name   string
		launch harness.Launch
	}{
		{
			name: "on the way back into a conversation",
			launch: harness.Launch{
				SessionID: "sess-1",
				Prompt:    "carry on",
				Resume:    true,
			},
		},
		{
			name:   "and on a fresh one",
			launch: harness.Launch{SessionID: "sess-2", Prompt: "get started"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocked := filepath.Join(t.TempDir(), "blocked")
			require.NoError(t, os.Mkdir(blocked, 0o500))

			launch := tt.launch
			launch.CacheDir = filepath.Join(blocked, "cache")

			_, err := NewHarness(NewHarnessParams{ConfigDir: t.TempDir()}).BuildCommand(launch)
			require.Error(t, err)
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
			command, err := NewHarness(NewHarnessParams{ConfigDir: t.TempDir()}).BuildCommand(harness.Launch{
				Flags:     tt.flags,
				SessionID: "sess",
				CacheDir:  t.TempDir(),
			})
			require.NoError(t, err)

			assert.NotContains(t, command, tt.wantAbsent, "an unquoted flag would run in the pane")
			assert.Contains(t, command, tt.wantPresent, "the flag survives only as a literal argument")
		})
	}
}

// TestBuildCommandPassesTextThroughTempFiles covers markdown carrying quotes and newlines
// surviving the trip through a file the shell cats and removes.
func TestBuildCommandPassesTextThroughTempFiles(t *testing.T) {
	tests := []struct {
		name        string
		launch      harness.Launch
		wantContain []string
		wantAbsent  []string
	}{
		{
			name: "a resume appends no system prompt, since it would not persist",
			launch: harness.Launch{
				SessionID: "sess",
				Context:   "the buildBriefing",
				Resume:    true,
			},
			wantAbsent: []string{"--append-system-prompt"},
		},
		{
			name: "a resume carries an opening prompt, which is how an unfinished turn is asked for again",
			launch: harness.Launch{
				SessionID: "sess",
				Prompt:    "carry on",
				Resume:    true,
			},
			wantContain: []string{"--resume 'sess'", ` -- "$(cat `, "; rm "},
			wantAbsent:  []string{"--session-id"},
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

			command, err := NewHarness(NewHarnessParams{ConfigDir: t.TempDir()}).BuildCommand(launch)
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

// TestBuildCommandStagesTextUnderTheCacheDir covers the prompt files landing under the directory
// komrad was told to keep what it generates in rather than the system temp directory.
func TestBuildCommandStagesTextUnderTheCacheDir(t *testing.T) {
	cache := t.TempDir()

	_, err := NewHarness(NewHarnessParams{ConfigDir: t.TempDir()}).BuildCommand(harness.Launch{
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

// TestBuildCommandRegistersMCPServer covers the config file passed without --strict-mcp-config,
// so an agent gains this server on top of the user's rather than trading them for it.
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
			configDir := t.TempDir()

			command, err := NewHarness(NewHarnessParams{ConfigDir: configDir}).BuildCommand(harness.Launch{
				CacheDir:    t.TempDir(),
				SessionID:   "sess",
				MCPEndpoint: "127.0.0.1:9999",
				Resume:      tt.resume,
			})
			require.NoError(t, err)

			assert.NotContains(t, command, "--strict-mcp-config", "the user's own servers survive")

			match := mcpConfigRef.FindStringSubmatch(command)
			require.Len(t, match, 2, "the launch carries a config path")
			assert.Equal(t, filepath.Join(configDir, "komrad-mcp.json"), match[1])

			raw, err := os.ReadFile(match[1])
			require.NoError(t, err)

			var config struct {
				MCPServers map[string]struct {
					Type       string `json:"type"`
					URL        string `json:"url"`
					AlwaysLoad bool   `json:"alwaysLoad"`
				} `json:"mcpServers"`
			}
			require.NoError(t, json.Unmarshal(raw, &config))

			server, ok := config.MCPServers["komrad"]
			require.True(t, ok, "the panel's own server is registered")
			assert.Equal(t, "http", server.Type)
			assert.Equal(t, "http://127.0.0.1:9999/mcp", server.URL)
			assert.True(t, server.AlwaysLoad, "the panel's tools sit in context rather than behind a tool search")
		})
	}
}

var mcpConfigRef = regexp.MustCompile(` --mcp-config '([^']+)'`)

var configDirRef = regexp.MustCompile(`CLAUDE_CONFIG_DIR='([^']+)' `)

func stripGeneratedFlags(command string) string {
	command = configDirRef.ReplaceAllString(command, "")

	return settingsRef.ReplaceAllString(mcpConfigRef.ReplaceAllString(command, ""), "")
}

// TestBuildCommandPinsTheConfigDir covers the launch carrying the directory komrad reads its log from.
func TestBuildCommandPinsTheConfigDir(t *testing.T) {
	tests := []struct {
		name      string
		configDir string
		launch    harness.Launch
		wantPin   bool
	}{
		{
			name:      "on a fresh launch",
			configDir: "work login",
			launch:    harness.Launch{SessionID: "sess"},
			wantPin:   true,
		},
		{
			name:      "and on a resume",
			configDir: "work login",
			launch:    harness.Launch{SessionID: "sess", Resume: true},
			wantPin:   true,
		},
		{
			name:   "not at all when the CLI is left to its default",
			launch: harness.Launch{SessionID: "sess"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())

			configDir := ""
			if tt.configDir != "" {
				configDir = filepath.Join(t.TempDir(), tt.configDir)
			}

			launch := tt.launch
			launch.CacheDir = t.TempDir()

			command, err := NewHarness(NewHarnessParams{ConfigDir: configDir}).BuildCommand(launch)
			require.NoError(t, err)

			if !tt.wantPin {
				assert.NotContains(t, command, configDirEnv+"=")

				return
			}

			match := configDirRef.FindStringSubmatch(command)
			require.Len(t, match, 2, "the launch carries the config dir")
			assert.Equal(t, configDir, match[1])
			assert.Less(t, strings.Index(command, configDirEnv+"="), strings.Index(command, " claude"),
				"the assignment leads the command so it reaches claude's environment")
		})
	}
}

var appendedPromptRef = regexp.MustCompile(`--append-system-prompt "\$\(cat '([^']+)'`)

func appendedPrompt(t *testing.T, command string) string {
	t.Helper()

	match := appendedPromptRef.FindStringSubmatch(command)
	require.Len(t, match, 2, "command should append a system prompt: %s", command)

	text, err := os.ReadFile(match[1])
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(match[1]) })

	return string(text)
}

// TestMCPConfigCarriesTheAgentsIdentity covers the headers naming the agent behind a call back
// to the panel, written as variables Claude expands so every agent can share one config file.
func TestMCPConfigCarriesTheAgentsIdentity(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	command, err := NewHarness(NewHarnessParams{}).BuildCommand(harness.Launch{
		CacheDir:    t.TempDir(),
		AgentID:     "agent-1",
		SessionID:   "sess-1",
		MCPEndpoint: "127.0.0.1:9999",
	})
	require.NoError(t, err)

	match := mcpConfigRef.FindStringSubmatch(command)
	require.Len(t, match, 2)
	assert.Equal(t, filepath.Join(home, ".claude", "komrad-mcp.json"), match[1],
		"one file for every agent, since the headers name no agent")

	raw, err := os.ReadFile(match[1])
	require.NoError(t, err)

	var config struct {
		MCPServers map[string]struct {
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	require.NoError(t, json.Unmarshal(raw, &config))

	headers := config.MCPServers["komrad"].Headers
	assert.Equal(t, "${KOMRAD_AGENT_ID:-}", headers[caller.AgentHeader])
	assert.Equal(t, "${KOMRAD_SESSION_ID:-}", headers[caller.SessionHeader])

	assert.Contains(t, command, "KOMRAD_AGENT_ID='agent-1'")
	assert.Contains(t, command, "KOMRAD_SESSION_ID='sess-1'")
}

// TestBuildCommandRegistersTheStopHook pins when the launch command carries the stop hook,
// which is what lets komrad hear an agent finish a turn.
func TestBuildCommandRegistersTheStopHook(t *testing.T) {
	tests := []struct {
		name        string
		hookCommand string
		resume      bool
		wantHook    bool
	}{
		{
			name:     "a build that could not name itself registers no hook",
			wantHook: false,
		},
		{
			name:        "a fresh launch",
			hookCommand: "/usr/local/bin/komrad",
			wantHook:    true,
		},
		{
			name:        "a resume, since mail arrives whether or not the agent was just started",
			hookCommand: "/usr/local/bin/komrad",
			resume:      true,
			wantHook:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configDir := t.TempDir()

			command, err := NewHarness(NewHarnessParams{ConfigDir: configDir}).BuildCommand(harness.Launch{
				CacheDir:    t.TempDir(),
				SessionID:   "sess",
				HookCommand: tt.hookCommand,
				Resume:      tt.resume,
			})
			require.NoError(t, err)

			settings := readSettings(t, command)
			assert.Equal(t, filepath.Join(configDir, "komrad-settings.json"), settingsPath(t, command))

			if !tt.wantHook {
				assert.Empty(t, settings.Hooks, "no hook is registered")

				return
			}

			require.Len(t, settings.Hooks, len(InstalledHooks), "every event komrad answers is written")
			for event, install := range InstalledHooks {
				group, ok := settings.Hooks[string(event)]
				require.True(t, ok, "the %s event is hooked", event)
				require.Len(t, group, 1)
				assert.Equal(t, install.Matcher, group[0].Matcher)
				require.Len(t, group[0].Hooks, 1)
				assert.Equal(t, "command", group[0].Hooks[0].Type)
				assert.Equal(t, tt.hookCommand+" hook "+install.Subcommand, group[0].Hooks[0].Command)
			}
		})
	}
}

// TestBuildCommandSetsTheCompactionPoint pins the point reaching claude's own settings.
func TestBuildCommandSetsTheCompactionPoint(t *testing.T) {
	configDir := t.TempDir()
	hn := NewHarness(NewHarnessParams{ConfigDir: configDir})

	build := func(role string, autoCompaction int) string {
		command, err := hn.BuildCommand(harness.Launch{
			CacheDir:       t.TempDir(),
			SessionID:      "sess",
			HookCommand:    "/usr/local/bin/komrad",
			Role:           role,
			AutoCompaction: autoCompaction,
		})
		require.NoError(t, err)

		return command
	}

	implementer := build("implementer", 250000)
	orchestrator := build("orchestrator", 150000)
	unset := build("", 0)
	belowFloor := build("reviewer", 40000)

	assert.Equal(t, 250000, readSettings(t, implementer).AutoCompactWindow, "a launch just after it did not overwrite its settings")
	assert.Equal(t, 150000, readSettings(t, orchestrator).AutoCompactWindow)
	assert.Zero(t, readSettings(t, unset).AutoCompactWindow, "no point leaves claude its own")
	assert.Equal(t, minAutoCompactWindow, readSettings(t, belowFloor).AutoCompactWindow, "claude ignores a window under its floor, so it is raised to it")
	assert.Equal(t, filepath.Join(configDir, "komrad-settings.json"), settingsPath(t, unset))
	assert.Equal(t, filepath.Join(configDir, "komrad-settings-implementer.json"), settingsPath(t, implementer))
	assert.Equal(t, filepath.Join(configDir, "komrad-settings-orchestrator.json"), settingsPath(t, orchestrator))
}

// TestBuildCommandDeniesSubagents covers the deny that leaves create_agent as the only way an
// agent spawns anything.
func TestBuildCommandDeniesSubagents(t *testing.T) {
	tests := []struct {
		name        string
		hookCommand string
		resume      bool
	}{
		{
			name:        "a fresh launch",
			hookCommand: "/usr/local/bin/komrad",
		},
		{
			name:   "a launch that registered no hook",
			resume: false,
		},
		{
			name:        "a resume",
			hookCommand: "/usr/local/bin/komrad",
			resume:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command, err := NewHarness(NewHarnessParams{ConfigDir: t.TempDir()}).BuildCommand(harness.Launch{
				CacheDir:    t.TempDir(),
				SessionID:   "sess",
				HookCommand: tt.hookCommand,
				Resume:      tt.resume,
			})
			require.NoError(t, err)

			deny := readSettings(t, command).Permissions.Deny
			assert.Contains(t, deny, "Agent(general-purpose)")
			assert.Contains(t, deny, "Agent(claude)")
			assert.NotContains(t, deny, "Agent(Explore)", "searching a codebase is not spawning an agent")
		})
	}
}

type claudeSettings struct {
	AutoCompactWindow int `json:"autoCompactWindow"`
	Hooks             map[string][]struct {
		Matcher string `json:"matcher"`
		Hooks   []struct {
			Type    string `json:"type"`
			Command string `json:"command"`
		} `json:"hooks"`
	} `json:"hooks"`
	Permissions struct {
		Deny []string `json:"deny"`
	} `json:"permissions"`
}

func settingsPath(t *testing.T, command string) string {
	t.Helper()

	match := settingsRef.FindStringSubmatch(command)
	require.Len(t, match, 2, "the launch carries a settings path")

	return match[1]
}

func readSettings(t *testing.T, command string) claudeSettings {
	t.Helper()

	raw, err := os.ReadFile(settingsPath(t, command))
	require.NoError(t, err)

	var settings claudeSettings
	require.NoError(t, json.Unmarshal(raw, &settings))

	return settings
}

var settingsRef = regexp.MustCompile(` --settings '([^']+)'`)
