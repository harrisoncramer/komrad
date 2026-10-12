package agents

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"komrad/config"
	"komrad/internal/harness"
	"komrad/internal/harnesstest"
	"komrad/internal/logging"
	"komrad/internal/logtest"
	agentstore "komrad/internal/store/agent"
	taskstore "komrad/internal/store/task"
	"komrad/internal/tmuxtest"

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

			_, err := launcher.launch(t.Context(), fakeHarness, launchParams{
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

			rest, ok = strings.CutSuffix(rest, "; tmux set-option -p -t 'pane' @komrad_cli_exited 1")
			require.True(t, ok, "the pane is marked once the agent exits")

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

	_, err := launcher.launch(t.Context(), &harnesstest.Fake{}, launchParams{Pane: "pane"})
	require.NoError(t, err)

	assert.Equal(t, fallbackShell, tmux.LastRespawn("pane").Argv[0])
}

// TestLaunchPassesWhatThePanelKnows covers what the launcher hands the harness, which is
// what the agent is told and which conversation it is.
func TestLaunchPassesWhatThePanelKnows(t *testing.T) {
	tmux := tmuxtest.NewFake()
	fakeHarness := &harnesstest.Fake{}
	launcher := NewLauncher(NewLauncherParams{Tmux: tmux, Addr: "127.0.0.1:9999"})

	_, err := launcher.launch(t.Context(), fakeHarness, launchParams{
		Pane:      "pane",
		Flags:     "--model opus",
		Prompt:    "do the thing",
		SessionID: "sess",
		AgentID:   "id-1",
		Resume:    true,
	})
	require.NoError(t, err)

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

// TestLaunchCompactsAtTheRolesPoint pins that an agent is launched with its own role's point.
func TestLaunchCompactsAtTheRolesPoint(t *testing.T) {
	tests := []struct {
		name string
		role agentstore.Role
		want int
	}{
		{
			name: "a writer",
			role: agentstore.RoleWriter,
			want: 250000,
		},
		{name: "an agent created without naming a role", want: 250000},
		{
			name: "an orchestrator",
			role: agentstore.RoleOrchestrator,
			want: 150000,
		},
		{
			name: "a role the config leaves to the CLI",
			role: agentstore.RoleReviewer,
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeHarness := &harnesstest.Fake{}
			launcher := NewLauncher(NewLauncherParams{
				Tmux: tmuxtest.NewFake(),
				Usage: config.Usage{
					Writer:       config.WriterUsage{AutoCompaction: 250000},
					Orchestrator: config.OrchestratorUsage{AutoCompaction: 150000},
				},
			})

			_, err := launcher.launch(t.Context(), fakeHarness, launchParams{Pane: "pane", Role: tt.role})
			require.NoError(t, err)

			require.Len(t, fakeHarness.Launches, 1)
			assert.Equal(t, tt.want, fakeHarness.Launches[0].AutoCompaction)
		})
	}
}

// TestLaunchTrustsTheDirForHarnessesThatAskIt covers the config deciding which CLIs have their
// launch directory trusted, and a failure to trust stopping the launch.
func TestLaunchTrustsTheDirForHarnessesThatAskIt(t *testing.T) {
	tests := []struct {
		name        string
		trust       map[string]bool
		dir         string
		trustErr    error
		wantTrusted []string
		wantErr     string
	}{
		{
			name:        "a harness the config trusts has its directory recorded before it starts",
			trust:       map[string]bool{harnesstest.FakeID: true},
			dir:         "/work",
			wantTrusted: []string{"/work"},
		},
		{
			name:  "a harness the config leaves off is left to ask",
			trust: map[string]bool{"some-other-cli": true},
			dir:   "/work",
		},
		{
			name:  "an agent with no directory has nothing to trust",
			trust: map[string]bool{harnesstest.FakeID: true},
		},
		{
			name:        "a directory that cannot be trusted launches nothing",
			trust:       map[string]bool{harnesstest.FakeID: true},
			dir:         "/work",
			trustErr:    errors.New("state is not json"),
			wantTrusted: []string{"/work"},
			wantErr:     "failed to trust the launch directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := tmuxtest.NewFake()
			fakeHarness := &harnesstest.Fake{TrustErr: tt.trustErr}
			launcher := NewLauncher(NewLauncherParams{
				Tmux:            tmux,
				CacheDir:        t.TempDir(),
				TrustLaunchDirs: tt.trust,
			})

			_, err := launcher.launch(t.Context(), fakeHarness, launchParams{Pane: "pane", Dir: tt.dir})

			assert.Equal(t, tt.wantTrusted, fakeHarness.Trusted)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				assert.Empty(t, fakeHarness.Launches)
				assert.Empty(t, tmux.LastRespawn("pane").Argv)

				return
			}
			require.NoError(t, err)
			assert.Len(t, fakeHarness.Launches, 1)
		})
	}
}

const briefedLogPath = "/var/cache/komrad/komrad.log"

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

				"KOMRAD_AGENT_ID",

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

			prompt := launcher.appendedSystemPrompt(t.Context(), launchParams{WorkflowContext: tt.context})

			for _, want := range tt.wantContain {
				assert.Contains(t, prompt, want)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, prompt, absent)
			}

			if tt.context != "" {
				assert.Less(t, strings.Index(prompt, "komrad"), strings.Index(prompt, tt.context),
					"the buildBriefing comes first")
			}
		})
	}
}

// TestAppendedSystemPromptDelegateBriefing pins that komrad writes the delegate briefing
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

			prompt := launcher.appendedSystemPrompt(t.Context(), launchParams{SpawnedByAgentID: tt.spawnedByAgentID, SpawnedByName: tt.spawnedByName})

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

			prompt := launcher.appendedSystemPrompt(t.Context(), launchParams{WorkflowContext: tt.workflowContext})

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
	require.Contains(t, launcher.appendedSystemPrompt(t.Context(), launchParams{}), "before-the-edit")

	require.NoError(t, os.WriteFile(path, []byte("after-the-edit"), 0o600))

	prompt := launcher.appendedSystemPrompt(t.Context(), launchParams{})
	assert.Contains(t, prompt, "after-the-edit")
	assert.NotContains(t, prompt, "before-the-edit")
}

// TestBriefingCarriesTheReviewRules pins the review surface being part of what komrad ships
// rather than something a person configures.
func TestBriefingCarriesTheReviewRules(t *testing.T) {
	launcher := NewLauncher(NewLauncherParams{Addr: "127.0.0.1:8787"})

	// No context file and no workflow.
	base := launcher.appendedSystemPrompt(t.Context(), launchParams{})
	reviewer := launcher.appendedSystemPrompt(t.Context(), launchParams{Role: agentstore.RoleReviewer})

	for _, want := range []string{
		"open_review_round",
		"create_review_thread",
		"release_review",
		"quoted_lines",
	} {
		assert.Contains(t, reviewer, want, "an agent giving a review is told the call by name")
	}

	assert.Contains(t, base, "add_review_message",
		"answering one reaches every agent, since a person may review any of them from the panel")
	assert.Contains(t, base, "`gh`", "and every agent is told plainly not to reach for the forge")
	assert.Contains(t, reviewer, "You review your task's writer and nothing else",
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
			role:         agentstore.RoleWriter,
			ownsCheckout: true,
			wantContain:  []string{asking, answering, "Commit on your branch, then call request_review"},
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
			role:        agentstore.RoleWriter,
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

			prompt := launcher.appendedSystemPrompt(t.Context(), launchParams{Role: tt.role, OwnsCheckout: tt.ownsCheckout})

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

	prompt := launcher.appendedSystemPrompt(t.Context(), launchParams{})

	assert.Contains(t, prompt, "add_review_message", "answering a review reaches every agent")
	assert.Contains(t, prompt, "Never spawn agents with your CLI's own subagent tools", "since komrad tracks no agent it did not launch")
	assert.Contains(t, prompt, "never touches a forge", "so does the rule against mailing one")
	assert.Contains(t, prompt, "Never watch, poll, or wait on a pull request's checks",
		"since a turn spent watching is one in which komrad's mail about the pull request cannot arrive")
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
			params: launchParams{Role: agentstore.RoleWriter, OwnsCheckout: true},
			want:   writerDoc,
		},
		{
			name:   "an agent holding no checkout is told nothing of the three",
			params: launchParams{Role: agentstore.RoleWriter},
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, roleBriefing(tt.params))
		})
	}
}

// TestSpawningBriefing pins that every writer, with a checkout or without, is told to use create_agent and no reviewer is.
func TestSpawningBriefing(t *testing.T) {
	tests := []struct {
		name   string
		params launchParams
		want   string
	}{
		{
			name:   "a writer holding its own checkout spawns",
			params: launchParams{Role: agentstore.RoleWriter, OwnsCheckout: true},
			want:   spawningDoc,
		},
		{
			name:   "a writer holding no checkout still spawns",
			params: launchParams{Role: agentstore.RoleWriter},
			want:   spawningDoc,
		},
		{
			name:   "a reviewer sends new work to its task's writer",
			params: launchParams{Role: agentstore.RoleReviewer},
			want:   "",
		},
		{
			name:   "an orchestrator has spawning rules of its own",
			params: launchParams{Role: agentstore.RoleOrchestrator},
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, spawningBriefing(tt.params))
		})
	}

	t.Run("a reviewer is never told to call create_agent", func(t *testing.T) {
		launcher := NewLauncher(NewLauncherParams{Tmux: tmuxtest.NewFake(), LogPath: briefedLogPath})
		prompt := launcher.appendedSystemPrompt(t.Context(), launchParams{
			Role: agentstore.RoleReviewer,
		})

		assert.NotContains(t, prompt, "create_agent")
	})
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
				WorkflowContext: "the writer lands work on a branch",
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
				Role:            agentstore.RoleWriter,
				WorkflowContext: "the writer lands work on a branch",
			},
			wantLast: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantDoc, contextBriefing(tt.params))

			prompt := NewLauncher(NewLauncherParams{Addr: "127.0.0.1:9999"}).appendedSystemPrompt(t.Context(), tt.params)
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

// TestLaunchGivesTheAgentStandInsForItsForgeTools covers the tools directory reaching the
// harness, which is how gh and glab are handed a credential the agent never holds itself.
func TestLaunchGivesTheAgentStandInsForItsForgeTools(t *testing.T) {
	cache := t.TempDir()
	fakeHarness := &harnesstest.Fake{}
	launcher := NewLauncher(NewLauncherParams{
		Tmux:     tmuxtest.NewFake(),
		Addr:     "127.0.0.1:9999",
		CacheDir: cache,
		BinPath:  "/usr/local/bin/komrad",
	})

	_, err := launcher.launch(t.Context(), fakeHarness, launchParams{Pane: "pane"})
	require.NoError(t, err)

	require.Len(t, fakeHarness.Launches, 1)
	tools := fakeHarness.Launches[0].ToolsDir
	assert.Equal(t, filepath.Join(cache, harness.ToolsDirName), tools)

	for _, tool := range []string{"gh", "glab"} {
		body, err := os.ReadFile(filepath.Join(tools, tool))
		require.NoError(t, err)
		assert.Contains(t, string(body), "'/usr/local/bin/komrad'")
		assert.NotContains(t, string(body), "ghs_", "the stand-in holds no token of its own")
	}
}

// TestLaunchWithoutAKomradBinaryGivesNoStandIn covers a launcher that cannot name komrad, whose
// agent reaches its forge the way it always did rather than through a stand-in that cannot mint.
func TestLaunchWithoutAKomradBinaryGivesNoStandIn(t *testing.T) {
	fakeHarness := &harnesstest.Fake{}
	launcher := NewLauncher(NewLauncherParams{Tmux: tmuxtest.NewFake(), CacheDir: t.TempDir()})

	_, err := launcher.launch(t.Context(), fakeHarness, launchParams{Pane: "pane"})
	require.NoError(t, err)

	require.Len(t, fakeHarness.Launches, 1)
	assert.Empty(t, fakeHarness.Launches[0].ToolsDir)
}

// TestLaunchLogsItsHooks pins the hooks a launch answers with and the line it leaves under its agent naming them.
func TestLaunchLogsItsHooks(t *testing.T) {
	tests := []struct {
		name          string
		binPath       string
		wantInstalled []string
		wantHooks     any
	}{
		{
			name:          "a launch pointing its hooks at komrad",
			binPath:       "/usr/local/bin/komrad",
			wantInstalled: []string{"PreToolUse", "Stop"},
			wantHooks:     []any{"PreToolUse", "Stop"},
		},
		{
			name: "a launch with no komrad binary to point them at",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			launcher := NewLauncher(NewLauncherParams{
				Tmux:     tmuxtest.NewFake(),
				CacheDir: t.TempDir(),
				BinPath:  tt.binPath,
			})

			var written strings.Builder
			ctx := logging.WithLogger(t.Context(), logging.New(&written, slog.LevelInfo))

			hooks, err := launcher.launch(ctx, &harnesstest.Fake{Installed: []string{"PreToolUse", "Stop"}}, launchParams{
				Pane:    "pane",
				AgentID: "id-1",
			})
			require.NoError(t, err)
			assert.Equal(t, tt.wantInstalled, hooks)

			record := logtest.Record(t, written.String())
			assert.Equal(t, "id-1", record[logging.TargetAgentID])
			assert.Equal(t, tt.wantHooks, record[logging.HookInstalled])
		})
	}
}

// TestReviewerBriefing pins what a reviewer is told.
func TestReviewerBriefing(t *testing.T) {
	launcher := NewLauncher(NewLauncherParams{
		Tmux:    tmuxtest.NewFake(),
		Addr:    "127.0.0.1:9999",
		LogPath: briefedLogPath,
	})

	prompt := launcher.appendedSystemPrompt(t.Context(), launchParams{
		Role:             agentstore.RoleReviewer,
		SpawnedByAgentID: "agent-author",
		SpawnedByName:    "author",
	})

	assert.Contains(t, prompt, "You were spawned by agent author (agent-author)")
	assert.Contains(t, prompt, "never commit")
}

// TestTaskBriefing pins what an agent is told of its task's goal and notes, and where it is pointed
// for the rest.
func TestTaskBriefing(t *testing.T) {
	pointers := "You work on task fix-login. get_task with that slug returns its goal and notes, list_review_threads its open review threads, and list_documents with task fix-login its documents.\n\n"

	tests := []struct {
		name string
		task *taskstore.Task
		want string
	}{
		{
			name: "an agent on no task is told nothing",
			want: "",
		},
		{
			name: "notes over the cap are read with get_task",
			task: &taskstore.Task{
				Slug:  "fix-login",
				Goal:  "Logins work.",
				Notes: strings.Repeat("x", maxBriefedTask),
			},
			want: pointers + "Its goal and notes are too long to repeat here, so read them with get_task before you start.",
		},
		{
			name: "a task with no goal leaves it out",
			task: &taskstore.Task{Slug: "fix-login"},
			want: pointers + "It has no notes yet.",
		},
		{
			name: "a task with no notes says so",
			task: &taskstore.Task{Slug: "fix-login", Goal: "Logins work."},
			want: pointers + "Its goal:\n\nLogins work.\n\nIt has no notes yet.",
		},
		{
			name: "notes under the cap are inlined",
			task: &taskstore.Task{
				Slug:  "fix-login",
				Goal:  "Logins work.",
				Notes: "The cookie expires early.",
			},
			want: pointers + "Its goal:\n\nLogins work.\n\nIts notes as they stood when you started:\n\nThe cookie expires early.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, taskBriefing(tt.task))
		})
	}
}
