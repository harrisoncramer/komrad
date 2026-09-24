package agents

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"weave/internal/harness"
	"weave/internal/harnesstest"
	agentstore "weave/internal/store/agent"
	"weave/internal/tmuxtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLaunchStartsTheHarnessCommand covers the launcher's whole job.
func TestLaunchStartsTheHarnessCommand(t *testing.T) {
	tests := []struct {
		name       string
		commandErr error
		respawnErr error
		wantErr    string
	}{
		{
			name: "the command becomes the pane's process",
		},
		{
			name:       "a harness that cannot build a command line launches nothing",
			commandErr: errors.New("no command for you"),
			wantErr:    "failed to build the launch command",
		},
		{
			name:       "a pane that refuses the command fails the launch",
			respawnErr: errors.New("no such pane"),
			wantErr:    "failed to start the agent in its pane",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := t.TempDir()
			tmux := tmuxtest.NewFake()
			tmux.RespawnErr = tt.respawnErr
			tmux.Shell = "/bin/zsh"
			fakeHarness := &harnesstest.Fake{CommandErr: tt.commandErr}
			launcher := NewLauncher(NewLauncherParams{
				Tmux:     tmux,
				Addr:     "127.0.0.1:9999",
				CacheDir: cache,
			})

			err := launcher.launch(fakeHarness, launchParams{
				Pane:      "pane",
				Dir:       "/work",
				SessionID: "sess",
			})

			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				if tt.commandErr != nil {
					assert.Empty(t, tmux.LastRespawn("pane").Argv, "a command that was never built is never run")
				}

				return
			}

			require.NoError(t, err)
			require.Len(t, fakeHarness.Launches, 1)
			assert.Empty(t, tmux.Sent, "nothing is typed at the pane's shell, so nothing reaches its history")

			run := tmux.LastRespawn("pane")
			assert.Equal(t, "/work", run.Dir)
			require.Len(t, run.Argv, 5)
			assert.Equal(t, []string{"/bin/zsh", "-l", "-i", "-c"}, run.Argv[:4], "the agent's shell is a login shell, as it was when tmux started it")

			script := run.Argv[4]
			rest, ok := strings.CutSuffix(script, "; exec '/bin/zsh' -l -i")
			require.True(t, ok, "the pane is left holding a shell once the agent exits")

			path, ok := strings.CutPrefix(rest, ". ")
			require.True(t, ok, "the pane sources a file, rather than being handed the command")
			path = strings.Trim(path, "'")
			assert.Equal(t, cache, filepath.Dir(path), "the script sits in the cache dir")

			body, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, "agent-cli --session sess\n", string(body))
		})
	}
}

// TestLaunchFallsBackToAPOSIXShell covers a tmux server naming no default shell.
func TestLaunchFallsBackToAPOSIXShell(t *testing.T) {
	tmux := tmuxtest.NewFake()
	tmux.Shell = ""
	launcher := NewLauncher(NewLauncherParams{
		Tmux:     tmux,
		Addr:     "127.0.0.1:9999",
		CacheDir: t.TempDir(),
	})

	require.NoError(t, launcher.launch(&harnesstest.Fake{}, launchParams{Pane: "pane"}))

	assert.Equal(t, fallbackShell, tmux.LastRespawn("pane").Argv[0])
}

// TestLaunchPassesWhatThePanelKnows covers what the launcher hands the harness, which is
// what the agent is told and which conversation it is.
func TestLaunchPassesWhatThePanelKnows(t *testing.T) {
	tmux := tmuxtest.NewFake()
	fakeHarness := &harnesstest.Fake{}
	launcher := NewLauncher(NewLauncherParams{Tmux: tmux, Addr: "127.0.0.1:9999"})

	require.NoError(t, launcher.launch(fakeHarness, launchParams{
		Pane:      "pane",
		Flags:     "--model opus",
		Prompt:    "do the thing",
		SessionID: "sess",
		AgentID:   "id-1",
		Resume:    true,
	}))

	require.Len(t, fakeHarness.Launches, 1)
	launch := fakeHarness.Launches[0]
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
				"agent_transcript_chunks",

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
			launcher := NewLauncher(NewLauncherParams{
				Tmux:    tmuxtest.NewFake(),
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
			launcher := NewLauncher(NewLauncherParams{
				Tmux:    tmuxtest.NewFake(),
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

			launcher := NewLauncher(NewLauncherParams{
				Tmux:        tmuxtest.NewFake(),
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

// TestGlobalContextIsReadPerLaunch pins the file being read at each launch.
func TestGlobalContextIsReadPerLaunch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "context.md")
	require.NoError(t, os.WriteFile(path, []byte("before-the-edit"), 0o600))

	launcher := NewLauncher(NewLauncherParams{
		Tmux:        tmuxtest.NewFake(),
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

// TestBriefingCarriesTheReviewRules pins the review surface being part of what weave ships
// rather than something a person configures.
func TestBriefingCarriesTheReviewRules(t *testing.T) {
	launcher := NewLauncher(NewLauncherParams{Addr: "127.0.0.1:8787"})

	// No context file and no workflow.
	base := launcher.appendedSystemPrompt(launchParams{})
	reviewer := launcher.appendedSystemPrompt(launchParams{Role: agentstore.RoleReviewer})

	for _, want := range []string{
		"open_review_round",
		"create_review_thread",
		"release_review",
		"quotedLines",
	} {
		assert.Contains(t, reviewer, want, "an agent giving a review is told the call by name")
	}

	assert.Contains(t, base, "add_review_message",
		"answering one reaches every agent, since a person may review any of them from the panel")
	assert.Contains(t, base, "`gh`", "and every agent is told plainly not to reach for the forge")
	assert.Contains(t, reviewer, "the agent you are a child of",
		"and who it may review, since the rule is refused at the server either way")
}

// TestAppendedSystemPromptReviewBriefing pins which half of the review loop an agent is sent.
func TestAppendedSystemPromptReviewBriefing(t *testing.T) {
	const (
		giving    = "### When you are the one reviewing"
		asking    = "### Getting your own work read"
		answering = "### When you are the one being reviewed"
	)

	tests := []struct {
		name         string
		role         agentstore.Role
		ownsCheckout bool
		wantContain  []string
		wantAbsent   []string
	}{
		{
			name:         "an agent with a checkout of its own is told to get its work read",
			role:         agentstore.RoleImplementer,
			ownsCheckout: true,
			wantContain:  []string{asking, answering, "Commit on your branch and stop there"},
			wantAbsent:   []string{giving, "open_review_round"},
		},
		{
			name:        "a reviewer is told how to give one and never to ask for one",
			role:        agentstore.RoleReviewer,
			wantContain: []string{giving, answering, "open_review_round", "You do not fix what you find"},
			wantAbsent:  []string{asking},
		},
		{
			name:         "a reviewer is not told to get its own work read even in its own checkout",
			role:         agentstore.RoleReviewer,
			ownsCheckout: true,
			wantContain:  []string{giving},
			wantAbsent:   []string{asking},
		},
		{
			name:        "an agent reading somebody else's tree is sent neither half",
			role:        agentstore.RoleImplementer,
			wantContain: []string{answering},
			wantAbsent:  []string{asking, giving},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			launcher := NewLauncher(NewLauncherParams{
				Tmux:    tmuxtest.NewFake(),
				Addr:    "127.0.0.1:9999",
				LogPath: briefedLogPath,
			})

			prompt := launcher.appendedSystemPrompt(launchParams{Role: tt.role, OwnsCheckout: tt.ownsCheckout})

			for _, want := range tt.wantContain {
				assert.Contains(t, prompt, want)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, prompt, absent)
			}
		})
	}
}

// TestBriefingKeepsWhatEveryAgentNeeds pins the facts that stayed in the base briefing when the
// review halves were cut out of it.
func TestBriefingKeepsWhatEveryAgentNeeds(t *testing.T) {
	launcher := NewLauncher(NewLauncherParams{
		Tmux:    tmuxtest.NewFake(),
		Addr:    "127.0.0.1:9999",
		LogPath: briefedLogPath,
	})

	prompt := launcher.appendedSystemPrompt(launchParams{})

	assert.Contains(t, prompt, "add_review_message", "answering a review reaches every agent")
	assert.Contains(t, prompt, "spawn that one with request_review rather than create_agent",
		"since create_agent cannot mark a reviewer, and an unmarked child is told none of this")
	assert.Contains(t, prompt, "never touches a forge", "so does the rule against mailing one")
	for _, key := range []string{"`branch`", "`summary`"} {
		assert.Contains(t, prompt, key, "the metadata keys are named rather than left to invention")
	}
}

// TestRoleBriefing pins which document an agent is given.
func TestRoleBriefing(t *testing.T) {
	tests := []struct {
		name   string
		params launchParams
		want   string
	}{
		{
			name:   "an orchestrator stands behind a workflow",
			params: launchParams{Role: agentstore.RoleOrchestrator},
			want:   orchestratorDoc,
		},
		{
			name:   "an orchestrator owning a checkout is still an orchestrator",
			params: launchParams{Role: agentstore.RoleOrchestrator, OwnsCheckout: true},
			want:   orchestratorDoc,
		},
		{
			name:   "a reviewer reads somebody else's work",
			params: launchParams{Role: agentstore.RoleReviewer},
			want:   reviewerDoc,
		},
		{
			name:   "an agent holding its own checkout gets it read",
			params: launchParams{Role: agentstore.RoleImplementer, OwnsCheckout: true},
			want:   implementerDoc,
		},
		{
			name:   "an agent holding no checkout is told nothing of the three",
			params: launchParams{Role: agentstore.RoleImplementer},
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, roleBriefing(tt.params))
		})
	}
}

// TestContextBriefing pins that a workflow's context still reaches an orchestrator, and that
// the sentence saying whose job it describes lands immediately before it.
func TestContextBriefing(t *testing.T) {
	tests := []struct {
		name     string
		params   launchParams
		wantDoc  string
		wantLast bool
	}{
		{
			name: "an orchestrator is told whose job the context describes",
			params: launchParams{
				Role:            agentstore.RoleOrchestrator,
				WorkflowContext: "the implementer lands work on a branch",
			},
			wantDoc:  orchestratorContextDoc,
			wantLast: true,
		},
		{
			name:   "an orchestrator in a workflow with no context is told nothing",
			params: launchParams{Role: agentstore.RoleOrchestrator},
		},
		{
			name: "every other role reads the context unframed",
			params: launchParams{
				Role:            agentstore.RoleImplementer,
				WorkflowContext: "the implementer lands work on a branch",
			},
			wantLast: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantDoc, contextBriefing(tt.params))

			prompt := NewLauncher(NewLauncherParams{Addr: "127.0.0.1:9999"}).appendedSystemPrompt(tt.params)
			if !tt.wantLast {
				return
			}

			assert.Contains(t, prompt, tt.params.WorkflowContext,
				"the workflow's own facts reach every role")

			if tt.wantDoc == "" {
				return
			}

			assert.Contains(t, prompt, strings.TrimSpace(tt.wantDoc)+"\n\n"+tt.params.WorkflowContext,
				"the framing sits immediately before the block it frames")
		})
	}
}

// TestLaunchGivesTheAgentAStandInForGH covers the tools directory reaching the harness, which
// is how gh is handed a credential the agent never holds itself.
func TestLaunchGivesTheAgentAStandInForGH(t *testing.T) {
	cache := t.TempDir()
	fakeHarness := &harnesstest.Fake{}
	launcher := NewLauncher(NewLauncherParams{
		Tmux:     tmuxtest.NewFake(),
		Addr:     "127.0.0.1:9999",
		CacheDir: cache,
		BinPath:  "/usr/local/bin/weave",
	})

	require.NoError(t, launcher.launch(fakeHarness, launchParams{Pane: "pane"}))

	require.Len(t, fakeHarness.Launches, 1)
	tools := fakeHarness.Launches[0].ToolsDir
	assert.Equal(t, filepath.Join(cache, harness.ToolsDirName), tools)

	body, err := os.ReadFile(filepath.Join(tools, "gh"))
	require.NoError(t, err)
	assert.Contains(t, string(body), "'/usr/local/bin/weave'")
	assert.NotContains(t, string(body), "ghs_", "the stand-in holds no token of its own")
}

// TestLaunchWithoutAWeaveBinaryGivesNoStandIn covers a launcher that cannot name weave, whose
// agent reaches github the way it always did rather than through a stand-in that cannot mint.
func TestLaunchWithoutAWeaveBinaryGivesNoStandIn(t *testing.T) {
	fakeHarness := &harnesstest.Fake{}
	launcher := NewLauncher(NewLauncherParams{Tmux: tmuxtest.NewFake(), CacheDir: t.TempDir()})

	require.NoError(t, launcher.launch(fakeHarness, launchParams{Pane: "pane"}))

	require.Len(t, fakeHarness.Launches, 1)
	assert.Empty(t, fakeHarness.Launches[0].ToolsDir)
}

// TestChildBriefingReplacesTheDelegateOne pins what a child is told.
func TestChildBriefingReplacesTheDelegateOne(t *testing.T) {
	launcher := NewLauncher(NewLauncherParams{
		Tmux:    tmuxtest.NewFake(),
		Addr:    "127.0.0.1:9999",
		LogPath: briefedLogPath,
	})

	prompt := launcher.appendedSystemPrompt(launchParams{
		SpawnedByAgentID: "agent-parent",
		SpawnedByName:    "author",
		ParentAgentID:    "agent-parent",
		ParentName:       "author",
	})

	assert.Contains(t, prompt, "You are a child of agent author (agent-parent)")
	assert.Contains(t, prompt, "do not commit")
	assert.Contains(t, prompt, "you go when the parent goes")
	assert.NotContains(t, prompt, "You were spawned by agent",
		"the spawner paragraph would understate what a child runs under")
}
