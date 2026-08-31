package codex

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"weave/internal/harness"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildCommand covers the command typed into the pane, stating the distinguishing part
// rather than the whole line since every launch carries the panel's MCP server.
func TestBuildCommand(t *testing.T) {
	tests := []struct {
		name        string
		launch      harness.Launch
		wantContain []string
		wantAbsent  []string
	}{
		{
			name:   "a resume reopens the prior conversation by id",
			launch: harness.Launch{SessionID: "019fb3e7-2cb6", Resume: true},
			wantContain: []string{
				"codex resume",
				"'019fb3e7-2cb6'",
			},

			wantAbsent: []string{"$(cat "},
		},
		{
			name:   "a resume carries an opening prompt after the id, so it is not read as one",
			launch: harness.Launch{SessionID: "019fb3e7-2cb6", Prompt: "carry on", Resume: true},
			wantContain: []string{
				"codex resume",
				"'019fb3e7-2cb6' -- \"$(cat ",
				"; rm ",
			},
		},
		{
			name:   "an adoption resumes the person's own rollout, since codex names its own conversations",
			launch: harness.Launch{ResumeFrom: "019fb3e7-2cb6", Prompt: "carry on"},
			wantContain: []string{
				"codex resume",
				"'019fb3e7-2cb6' -- \"$(cat ",
			},
		},
		{
			name:       "a fresh launch pins no id",
			launch:     harness.Launch{Prompt: "do the thing"},
			wantAbsent: []string{"resume", "--session-id"},
		},
		{
			name:        "flags reach the command line",
			launch:      harness.Launch{Flags: "-m gpt-5.6 -s read-only", Prompt: "go"},
			wantContain: []string{"'-m' 'gpt-5.6' '-s' 'read-only'"},
		},
		{
			name:        "the agent's own id leads the line, so it reaches codex's environment",
			launch:      harness.Launch{AgentID: "id-1", Prompt: "go"},
			wantContain: []string{"WEAVE_AGENT_ID='id-1' CODEX_INTERNAL_ORIGINATOR_OVERRIDE='weave-id-1' codex"},
		},
		{
			name:   "and it survives a resume",
			launch: harness.Launch{AgentID: "id-1", SessionID: "sess", Resume: true},
			wantContain: []string{
				"WEAVE_AGENT_ID='id-1' WEAVE_SESSION_ID='sess' CODEX_INTERNAL_ORIGINATOR_OVERRIDE='weave-id-1' codex resume",
			},
		},
		{
			name:       "a launch with no agent id stamps nothing",
			launch:     harness.Launch{Prompt: "go"},
			wantAbsent: []string{"CODEX_INTERNAL_ORIGINATOR_OVERRIDE"},
		},
		{
			name:        "the prompt is fenced off, since it could open with a subcommand's name",
			launch:      harness.Launch{Prompt: "resume the deploy"},
			wantContain: []string{` -- "$(cat `, "; rm "},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			launch := tt.launch
			launch.CacheDir = t.TempDir()

			command, err := NewHarness(NewHarnessParams{SessionsDir: t.TempDir()}).BuildCommand(launch)
			require.NoError(t, err)

			for _, want := range tt.wantContain {
				assert.Contains(t, command, want)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, command, absent)
			}
		})
	}
}

// TestBuildCommandRegistersMCPServer covers Codex taking MCP servers from its config rather
// than a flag, so the launch overrides that config and leaves the user's file untouched.
func TestBuildCommandRegistersMCPServer(t *testing.T) {
	tests := []struct {
		name   string
		resume bool
	}{
		{
			name:   "on a resume, since a resumed agent should keep the tools",
			resume: true,
		},
		{
			name: "on a fresh launch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command, err := NewHarness(NewHarnessParams{SessionsDir: t.TempDir()}).BuildCommand(harness.Launch{
				CacheDir:    t.TempDir(),
				SessionID:   "sess",
				MCPEndpoint: "127.0.0.1:9999",
				Resume:      tt.resume,
			})
			require.NoError(t, err)

			assert.Contains(t, command, `-c 'mcp_servers.weave.url="http://127.0.0.1:9999/mcp"'`)
			assert.Contains(t, command, `-c 'mcp_servers.weave.default_tools_approval_mode="approve"'`)
		})
	}
}

// TestBuildCommandQuotesFlags covers flags reaching the pane through send-keys, where anything
// a shell would act on has to arrive as a literal argument.
func TestBuildCommandQuotesFlags(t *testing.T) {
	tests := []struct {
		name        string
		flags       string
		wantAbsent  string
		wantPresent string
	}{
		{
			name:        "a command substitution reaches the pane as a literal",
			flags:       "-m $(touch /tmp/pwned)",
			wantAbsent:  "codex -m $(",
			wantPresent: `'$(touch'`,
		},
		{
			name:        "a chained command reaches the pane as a literal",
			flags:       "-m gpt; touch /tmp/pwned",
			wantAbsent:  "; touch /tmp/pwned",
			wantPresent: `'gpt;'`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command, err := NewHarness(NewHarnessParams{SessionsDir: t.TempDir()}).BuildCommand(harness.Launch{Flags: tt.flags, Prompt: "go", CacheDir: t.TempDir()})
			require.NoError(t, err)

			assert.NotContains(t, command, tt.wantAbsent, "an unquoted flag would run in the pane")
			assert.Contains(t, command, tt.wantPresent, "the flag survives only as a literal argument")
		})
	}
}

// TestOpeningPrompt covers the briefing arriving ahead of the prompt the agent was created
// with, since Codex has no way to append to its system prompt.
func TestOpeningPrompt(t *testing.T) {
	tests := []struct {
		name   string
		launch harness.Launch
		want   string
	}{
		{
			name:   "an agent created with no prompt is left unbriefed",
			launch: harness.Launch{Context: "the buildBriefing"},
			want:   "",
		},
		{
			name:   "a prompt with no buildBriefing is sent as it stands",
			launch: harness.Launch{Prompt: "do the thing"},
			want:   "do the thing",
		},
		{
			name:   "the buildBriefing comes first, fenced, so the prompt reads as the task",
			launch: harness.Launch{Context: "the buildBriefing", Prompt: "do the thing"},
			want:   "<panel_briefing>\nthe buildBriefing\n</panel_briefing>\n\ndo the thing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harnessUnderTest := NewHarness(NewHarnessParams{SessionsDir: t.TempDir()})
			assert.Equal(t, tt.want, harnessUnderTest.openingPrompt(tt.launch))

			launch := tt.launch
			launch.CacheDir = t.TempDir()

			command, err := harnessUnderTest.BuildCommand(launch)
			require.NoError(t, err)

			if tt.want == "" {
				assert.NotContains(t, command, "$(cat ", "nothing is sourced when there is nothing to say")

				return
			}

			assert.Equal(t, tt.want, promptText(t, command), "the text reaches the file the command sources")
		})
	}
}

var promptRef = regexp.MustCompile(`"\$\(cat '([^']+)'`)

func promptText(t *testing.T, command string) string {
	t.Helper()

	match := promptRef.FindStringSubmatch(command)
	require.Len(t, match, 2, "command should source a prompt: %s", command)

	text, err := os.ReadFile(match[1])
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(match[1]) })

	return string(text)
}

// TestTOMLString covers rendering a value for a -c override, whose value is parsed as
// TOML rather than taken as a string.
func TestTOMLString(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{
			name:  "a plain path is quoted",
			value: "/repo/worktree",
			want:  `"/repo/worktree"`,
		},
		{
			name:  "an embedded quote is escaped",
			value: `we"ird`,
			want:  `"we\"ird"`,
		},
		{
			name:  "a backslash is escaped first, so an escaped quote stays escaped",
			value: `back\"slash`,
			want:  `"back\\\"slash"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tomlString(tt.value))
			assert.False(t, strings.HasSuffix(strings.TrimSuffix(tt.want, `"`), `\`),
				"a value must not end mid-escape, which would swallow the closing quote")
		})
	}
}

// TestMCPHeadersNameTheEnvironment covers Codex identifying the agent through env_http_headers,
// which keeps the override the same for every agent while the values differ.
func TestMCPHeadersNameTheEnvironment(t *testing.T) {
	command, err := NewHarness(NewHarnessParams{}).BuildCommand(harness.Launch{
		CacheDir:    t.TempDir(),
		AgentID:     "agent-1",
		SessionID:   "sess-1",
		MCPEndpoint: "127.0.0.1:9999",
	})
	require.NoError(t, err)

	assert.Contains(t, command,
		`-c 'mcp_servers.weave.env_http_headers={"X-Weave-Agent-Id"="WEAVE_AGENT_ID","X-Weave-Session-Id"="WEAVE_SESSION_ID"}'`)

	assert.Contains(t, command, "WEAVE_AGENT_ID='agent-1'")
	assert.Contains(t, command, "WEAVE_SESSION_ID='sess-1'")
}

// TestMCPHeadersOnlyWithAnEndpoint covers a launch with no server to register, where no
// override is written rather than one pointing at nothing.
func TestMCPHeadersOnlyWithAnEndpoint(t *testing.T) {
	command, err := NewHarness(NewHarnessParams{}).BuildCommand(harness.Launch{AgentID: "agent-1"})
	require.NoError(t, err)

	assert.NotContains(t, command, "env_http_headers")
}
