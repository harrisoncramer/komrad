package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"komrad/config"
	"komrad/internal/agenterr"
	"komrad/internal/caller"
	"komrad/internal/harness"
	"komrad/internal/harnesstest"
	"komrad/internal/scriptrun"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	taskstore "komrad/internal/store/task"
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/tasks"
	"komrad/internal/tmuxtest"
	"komrad/lib/clock"
	"komrad/lib/xptr"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewAgent covers a launch end to end, where a refused one records nothing.
func TestNewAgent(t *testing.T) {
	tests := []struct {
		name           string
		dir            func(t *testing.T) string
		title          string
		windowErr      error
		respawnErr     error
		setOwnerErr    error
		badWorkflow    bool
		wantErrIs      error
		wantErr        string
		wantPaneKilled bool
		wantsFailedRow bool
	}{
		{
			name:      "a directory nobody can enter",
			dir:       func(*testing.T) string { return "/no/such/directory" },
			title:     "nowhere",
			wantErrIs: ErrBadDirectory,
		},
		{
			name: "a path that is a file",
			dir: func(t *testing.T) string {
				file := filepath.Join(t.TempDir(), "a-file")
				require.NoError(t, os.WriteFile(file, []byte("not a directory"), 0o644))

				return file
			},
			title:   "a-file",
			wantErr: "not a directory",
		},
		{
			name:        "a workflow that does not exist, which would render nowhere",
			dir:         func(t *testing.T) string { return t.TempDir() },
			title:       "orphan",
			badWorkflow: true,
			wantErrIs:   agenterr.ErrWorkflowNotFound,
		},
		{
			name:           "a window tmux refuses",
			dir:            func(t *testing.T) string { return t.TempDir() },
			title:          "doomed",
			windowErr:      errors.New("tmux refused the window name"),
			wantErr:        "failed to open a window",
			wantsFailedRow: true,
		},
		{
			name:           "a pane tmux refuses to stamp",
			dir:            func(t *testing.T) string { return t.TempDir() },
			title:          "unstamped",
			setOwnerErr:    errors.New("tmux refused the option"),
			wantErr:        "failed to stamp the pane",
			wantPaneKilled: true,
			wantsFailedRow: true,
		},
		{
			name:           "a launch that never starts in the pane",
			dir:            func(t *testing.T) string { return t.TempDir() },
			title:          "stillborn",
			respawnErr:     errors.New("tmux refused the command"),
			wantErr:        "failed to launch the agent",
			wantPaneKilled: true,
			wantsFailedRow: true,
		},
		{
			name:  "a launch that works",
			dir:   func(t *testing.T) string { return t.TempDir() },
			title: "My Agent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := tmuxtest.NewFake()
			tmux.NewWindowErr = tt.windowErr
			tmux.RespawnErr = tt.respawnErr
			tmux.SetOwnerErr = tt.setOwnerErr
			st := newStore(t)
			manager := st.agentService(agentServiceOptions{
				tmux:      tmux,
				harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{Installed: []string{"PreToolUse", "Stop"}}},
				addr:      "127.0.0.1:9999",
				binPath:   "/usr/local/bin/komrad",
				flags:     "--default-flag",
			})

			params := NewAgentParams{
				Title:      tt.title,
				Dir:        tt.dir(t),
				Flags:      "--extra",
				Prompt:     "do the thing",
				WorkflowID: seedWorkflow(t, st),
			}
			if tt.badWorkflow {
				params.WorkflowID = "no-such-workflow"
			}

			created, err := manager.launch.NewAgent(t.Context(), params)

			if tt.wantErrIs != nil || tt.wantErr != "" {
				require.Error(t, err)
				if tt.wantErrIs != nil {
					require.ErrorIs(t, err, tt.wantErrIs)
				}
				if tt.wantErr != "" {
					require.ErrorContains(t, err, tt.wantErr)
				}

				assert.Empty(t, tmux.Respawned, "nothing is started when the launch was refused")

				agents, listErr := st.Agents.List(t.Context())
				require.NoError(t, listErr)

				if tt.wantsFailedRow {
					require.Len(t, agents, 1, "a launch that had already claimed a slot leaves a row to look at")
					assert.Equal(t, agentstore.StateFailedToCreate, agents[0].State)
					assert.Contains(t, agents[0].FailureReason, tt.wantErr, "and why, for the row that is left")
					assert.Empty(t, agents[0].Hooks, "and no hooks, since its launch installed none")
				} else {
					assert.Empty(t, agents, "a refused launch before any slot was claimed records nothing")
				}

				if tt.wantPaneKilled {
					assert.NotEmpty(t, tmux.KilledPanes, "the open pane goes with the record")
				}

				return
			}

			require.NoError(t, err)
			require.NotNil(t, created)
			assert.Equal(t, "My-Agent", created.Name, "the caller learns the generated name")
			assert.NotEmpty(t, created.ID, "and the stored id")

			stored, err := st.Get(t.Context(), "My-Agent")
			require.NoError(t, err)
			require.NotNil(t, stored)
			assert.Equal(t, "%My-Agent", stored.Pane, "the new window's pane is the agent's handle")
			assert.Equal(t, params.Dir, stored.Cwd)
			assert.Equal(t, "--default-flag --extra", stored.Flags, "the configured flags come first")
			assert.Equal(t, "do the thing", stored.Prompt)
			assert.Equal(t, agentstore.StateIdle, stored.State)
			assert.Equal(t, []string{"PreToolUse", "Stop"}, stored.Hooks, "the hooks its launch installed")
			assert.NotEmpty(t, stored.SessionID, "a conversation id is pinned at launch")
			assert.NotEmpty(t, tmux.LastRespawn("%My-Agent").Argv, "the agent is launched into the new pane")
			assert.Equal(t, stored.ID, tmux.Owners["%My-Agent"], "the pane is stamped with the agent it is running")
			assert.True(t, manager.panes.IsAgentRunning(*stored), "and the agent reads as running in it")
		})
	}
}

// TestNewAgentIdempotency pins a retry finding the agent it already made, or the same prompt
// runs twice in two tmux windows.
func TestNewAgentIdempotency(t *testing.T) {
	tests := []struct {
		name         string
		first        string
		firstCaller  string
		second       string
		secondCaller string
		wantSame     bool
		wantErr      error
	}{
		{
			name:     "the same key returns the agent already created",
			first:    "schedule:s1:2026-07-28T09:00:00Z",
			second:   "schedule:s1:2026-07-28T09:00:00Z",
			wantSame: true,
			wantErr:  agenterr.ErrAlreadyLaunched,
		},
		{
			name:         "the same key from one writer returns the agent already created",
			first:        "launch-1",
			firstCaller:  "writer",
			second:       "launch-1",
			secondCaller: "writer",
			wantSame:     true,
			wantErr:      agenterr.ErrAlreadyLaunched,
		},
		{
			name:         "the same key from two writers under one orchestrator is two agents",
			first:        "launch-1",
			firstCaller:  "writer",
			second:       "launch-1",
			secondCaller: "other writer",
		},
		{
			name:   "the next occurrence is a different agent",
			first:  "schedule:s1:2026-07-28T09:00:00Z",
			second: "schedule:s1:2026-07-29T09:00:00Z",
		},
		{
			name: "two creates claiming no key are two agents",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := tmuxtest.NewFake()
			st := newStore(t)
			manager := newManager(tmux, st)
			dir := t.TempDir()
			workflowID := seedWorkflow(t, st)

			callerIDs := map[string]string{}
			if tt.firstCaller != "" {
				seedWithRole(t, st, workflowID, "orchestrator", agentstore.RoleOrchestrator)
				for _, name := range []string{"writer", "other writer"} {
					seeded := seedWithRole(t, st, workflowID, name, agentstore.RoleWriter)
					callerIDs[name] = seeded.ID
				}
			}
			seeded, err := st.Agents.List(t.Context())
			require.NoError(t, err)

			params := NewAgentParams{
				Title:          "morning-ci",
				Dir:            dir,
				Prompt:         "check CI",
				WorkflowID:     workflowID,
				IdempotencyKey: tt.first,
			}
			first, err := manager.launch.NewAgent(caller.With(t.Context(), caller.Caller{AgentID: callerIDs[tt.firstCaller]}), params)
			require.NoError(t, err)
			require.NotNil(t, first)

			params.IdempotencyKey = tt.second
			second, err := manager.launch.NewAgent(caller.With(t.Context(), caller.Caller{AgentID: callerIDs[tt.secondCaller]}), params)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr, "a key already spent says so rather than passing for a launch")
			} else {
				require.NoError(t, err)
			}
			require.NotNil(t, second, "the agent already under the key comes back either way")

			agents, err := st.Agents.List(t.Context())
			require.NoError(t, err)

			if tt.wantSame {
				assert.Equal(t, first.ID, second.ID, "the retry finds the agent it already created")
				assert.Len(t, agents, len(seeded)+1, "and no second agent is recorded")
				assert.Len(t, tmux.Respawned, 1, "nor is a second agent started")

				return
			}

			assert.NotEqual(t, first.ID, second.ID)
			assert.Len(t, agents, len(seeded)+2)
		})
	}
}

// TestNewAgentRetriesUnderTheSameKey covers what a retry of one firing does about the agent the
// previous attempt left behind.
func TestNewAgentRetriesUnderTheSameKey(t *testing.T) {
	const key = "schedule:s1:2026-07-28T09:00:00Z"

	tests := []struct {
		name         string
		state        agentstore.State
		wantRelaunch bool
	}{
		{
			name:         "a create that failed is launched again",
			state:        agentstore.StateFailedToCreate,
			wantRelaunch: true,
		},
		{
			name:  "a cleanup that failed belongs to an agent that ran, so the key stands",
			state: agentstore.StateFailedToCleanup,
		},
		{
			name:  "an agent still coming up is left alone",
			state: agentstore.StateSettingUp,
		},
		{
			name:  "a working agent is left alone",
			state: agentstore.StateThinking,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := tmuxtest.NewFake()
			st := newStore(t)
			manager := newManager(tmux, st)

			params := NewAgentParams{
				Title:          "morning-ci",
				Dir:            t.TempDir(),
				Prompt:         "check CI",
				WorkflowID:     seedWorkflow(t, st),
				IdempotencyKey: key,
			}

			first, err := manager.launch.NewAgent(t.Context(), params)
			require.NoError(t, err)
			require.NoError(t, st.Agents.SetStateByID(t.Context(), first.ID, tt.state))

			second, err := manager.launch.NewAgent(t.Context(), params)
			require.NotNil(t, second)

			all, listErr := st.Agents.ListAll(t.Context())
			require.NoError(t, listErr)

			if !tt.wantRelaunch {
				require.ErrorIs(t, err, agenterr.ErrAlreadyLaunched)
				assert.Equal(t, first.ID, second.ID, "the retry is answered with the agent already under the key")
				assert.Len(t, all, 1, "and records no second agent")
				assert.Len(t, tmux.Respawned, 1, "nor starts one")

				return
			}

			require.NoError(t, err, "a failed create never launched, so the retry is free to")
			assert.NotEqual(t, first.ID, second.ID)
			assert.Len(t, all, 2, "the agent that failed is kept, since it says why it failed")
			assert.Len(t, tmux.Respawned, 2, "and the retry actually starts something")

			held, err := st.Agents.GetByIdempotencyKey(t.Context(), agentstore.IdempotencyKey{Key: key})
			require.NoError(t, err)
			require.NotNil(t, held)
			assert.Equal(t, second.ID, held.ID, "the key moves to the agent that launched")
		})
	}
}

type orchestratorState int

const (
	noOrchestrator orchestratorState = iota
	liveOrchestrator
	archivedOrchestrator
)

// TestNewAgentRecordsWhoSpawnedIt pins the caller header becoming the new agent's spawner,
// which the panel sends none of so what it creates has no spawner.
func TestNewAgentRecordsWhoSpawnedIt(t *testing.T) {
	tests := []struct {
		name             string
		spawnerRole      agentstore.Role
		orchestrator     orchestratorState
		reviewer         bool
		unknownSpawner   bool
		wantOrchestrator bool
		wantErrIs        error
	}{
		{
			name:           "a caller header naming no agent is refused",
			unknownSpawner: true,
			wantErrIs:      agenterr.ErrSpawnerUnknown,
		},
		{
			name: "a person creating from the panel records no spawner",
		},
		{
			name:        "a writer in a workflow with no orchestrator answers for what it spawns",
			spawnerRole: agentstore.RoleWriter,
		},
		{
			name:             "a writer's standalone agent answers to the workflow's orchestrator",
			spawnerRole:      agentstore.RoleWriter,
			orchestrator:     liveOrchestrator,
			wantOrchestrator: true,
		},
		{
			name:         "a writer whose orchestrator is archived answers for what it spawns",
			spawnerRole:  agentstore.RoleWriter,
			orchestrator: archivedOrchestrator,
		},
		{
			name:         "a writer's reviewer answers to the writer",
			spawnerRole:  agentstore.RoleWriter,
			orchestrator: liveOrchestrator,
			reviewer:     true,
		},
		{
			name:         "a reviewer's standalone agent answers to the reviewer",
			spawnerRole:  agentstore.RoleReviewer,
			orchestrator: liveOrchestrator,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := tmuxtest.NewFake()
			st := newStore(t)
			manager, fakeHarness := newManagerWithHarness(tmux, st, "")

			ctx := t.Context()
			workflowID := seedWorkflow(t, st)
			params := NewAgentParams{
				Title:      "delegate",
				Dir:        t.TempDir(),
				Prompt:     "do the thing",
				WorkflowID: workflowID,
			}

			var orchestrator agentstore.Agent
			if tt.orchestrator != noOrchestrator {
				orchestrator = seedWithRole(t, st, workflowID, "orchestrator", agentstore.RoleOrchestrator)
			}
			if tt.orchestrator == archivedOrchestrator {
				_, err := st.Agents.Archive(ctx, []string{orchestrator.ID})
				require.NoError(t, err)
			}

			var spawner *agentstore.Agent
			switch {
			case tt.unknownSpawner:
				ctx = caller.With(ctx, caller.Caller{AgentID: "no-such-agent"})
			case tt.spawnerRole != "":
				seeded := seedWithRole(t, st, workflowID, "spawner", tt.spawnerRole)
				spawner = &seeded
				ctx = caller.With(ctx, caller.Caller{AgentID: seeded.ID})
				if tt.reviewer {
					params = reviewerOf(seeded, "reviewer")
				}
			}

			created, err := manager.launch.NewAgent(ctx, params)

			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)
				assert.Empty(t, tmux.Respawned, "nothing is started when the caller cannot be attributed")

				agents, listErr := st.Agents.List(t.Context())
				require.NoError(t, listErr)
				assert.Empty(t, agents, "a refused create records nothing")

				return
			}
			require.NoError(t, err)
			require.NotNil(t, created)
			require.Len(t, fakeHarness.Launches, 1)
			launch := fakeHarness.Launches[0]

			if spawner == nil {
				assert.Nil(t, created.SpawnedByAgentID, "a create with no caller header names no spawner")
				assert.NotContains(t, launch.Context, "You were spawned by", "a delegate briefing with nothing to say names nobody")

				return
			}

			want := spawner
			if tt.wantOrchestrator {
				want = &orchestrator
			}
			require.NotNil(t, created.SpawnedByAgentID)
			assert.Equal(t, want.ID, *created.SpawnedByAgentID)
			if !tt.reviewer {
				assert.Contains(t, launch.Context, fmt.Sprintf("spawned by agent %s (%s)", want.Name, want.ID))
				assert.Contains(t, launch.Context, "tell it when you are done")
			}
		})
	}
}

// TestNewAgentGetsDirectory pins a ~ path being made absolute before the window is opened,
// which tmux does no expansion of.
func TestNewAgentGetsDirectory(t *testing.T) {
	tests := []struct {
		name        string
		dir         string
		workflowCwd string
		useClient   bool
	}{
		{
			name:        "a blank directory takes the workflow's",
			workflowCwd: "~/acmecorp/monorepo",
		},
		{
			name:        "what the caller asked for wins over the workflow's",
			dir:         "~/acmecorp/monorepo",
			workflowCwd: "/no/such/directory",
		},
		{
			name:      "with neither, the invoking client's directory answers",
			useClient: true,
		},
		{
			name: "a ~ path is expanded before tmux sees it",
			dir:  "~/acmecorp/monorepo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)

			want := filepath.Join(home, "acmecorp", "monorepo")
			require.NoError(t, os.MkdirAll(want, 0o755))

			tmux := tmuxtest.NewFake()
			if tt.useClient {
				tmux.ClientDir = want
			}

			st := newStore(t)
			manager := newManager(tmux, st)

			created, err := newWorkflowService(st, tmux).NewWorkflow(t.Context(), workflowstore.Config{Name: "backend", Cwd: tt.workflowCwd})
			require.NoError(t, err)

			_, err = manager.launch.NewAgent(t.Context(), NewAgentParams{
				Title:      "scoped",
				Dir:        tt.dir,
				WorkflowID: created.ID,
			})
			require.NoError(t, err)

			assert.Equal(t, want, tmux.NewWindows["scoped"], "tmux is given an absolute path")

			stored, err := st.Get(t.Context(), "scoped")
			require.NoError(t, err)
			require.NotNil(t, stored)
			assert.Equal(t, want, stored.Cwd, "and the stored cwd matches it")
		})
	}
}

// TestNewAgentInsideWorkflow pins the workflow context being a snapshot taken at launch, so
// a later edit reaches only agents spawned after it.
func TestNewAgentInsideWorkflow(t *testing.T) {
	tests := []struct {
		name        string
		context     string
		scoped      bool
		wantContext bool
	}{
		{
			name:   "an agent created outside every workflow carries none",
			scoped: false,
		},
		{
			name:   "a workflow with no context adds nothing",
			scoped: true,
		},
		{
			name:        "a workflow's context is appended to the system prompt",
			context:     "shared notes",
			scoped:      true,
			wantContext: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := tmuxtest.NewFake()
			st := newStore(t)
			manager, fakeHarness := newManagerWithHarness(tmux, st, "")

			created, err := newWorkflowService(st, tmux).NewWorkflow(t.Context(), workflowstore.Config{
				Name:    "backend",
				Context: tt.context,
			})
			require.NoError(t, err)

			params := NewAgentParams{
				Title:      "agent",
				Dir:        t.TempDir(),
				WorkflowID: created.ID,
			}

			_, err = manager.launch.NewAgent(t.Context(), params)
			require.NoError(t, err)

			stored, err := st.Get(t.Context(), "agent")
			require.NoError(t, err)
			require.NotNil(t, stored)
			assert.Equal(t, params.WorkflowID, stored.WorkflowID, "the agent carries its workflow's id")

			require.Len(t, fakeHarness.Launches, 1)
			prompt := fakeHarness.Launches[0].Context
			assert.Contains(t, prompt, "/mcp", "every agent is told about the endpoint")
			if tt.wantContext {
				assert.Contains(t, prompt, "shared notes")
				return
			}

			assert.NotContains(t, prompt, "shared notes")
		})
	}
}

// TestNewAgentRunsWorkflowSetup covers the two things a workflow's provisioner decides
// about a create, which are whether it happens at all and where the agent ends up running.
func TestNewAgentRunsWorkflowSetup(t *testing.T) {
	provisioned := t.TempDir()

	tests := []struct {
		name         string
		worktree     bool
		answered     string
		provisionErr error
		wantDir      string
		wantSetup    []string
		wantErr      error
	}{
		{
			name:         "setup that fails abandons the create",
			worktree:     true,
			provisionErr: ErrProvisionFailed,
			wantSetup:    []string{scriptrun.WorktreeProvisioner},
			wantErr:      ErrProvisionFailed,
		},
		{
			name:      "a directory setup names that is not usable abandons the create",
			worktree:  true,
			answered:  filepath.Join(t.TempDir(), "never-created"),
			wantSetup: []string{scriptrun.WorktreeProvisioner},
			wantErr:   ErrProvisionFailed,
		},
		{
			name: "a workflow provisioning nothing launches where it was told to",
		},
		{
			name:      "setup that names no directory leaves the create where it was",
			worktree:  true,
			wantSetup: []string{scriptrun.WorktreeProvisioner},
		},
		{
			name:      "the directory setup names is where the agent runs",
			worktree:  true,
			answered:  provisioned,
			wantDir:   provisioned,
			wantSetup: []string{scriptrun.WorktreeProvisioner},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := tmuxtest.NewFake()
			st := newStore(t)
			lifecycle := &fakeLifecycle{dir: tt.answered, provisionErr: tt.provisionErr}
			fakeHarness := &harnesstest.Fake{}
			manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: fakeHarness}, lifecycle)

			cfg := workflowstore.Config{Name: "backend"}
			if tt.worktree {
				cfg = worktreeConfig(t, "backend")
			}

			created, err := newWorkflowService(st, tmux).NewWorkflow(t.Context(), cfg)
			require.NoError(t, err)

			asked := t.TempDir()
			_, err = manager.launch.NewAgent(t.Context(), NewAgentParams{
				Title:      "agent",
				Dir:        asked,
				WorkflowID: created.ID,
			})

			assert.Equal(t, tt.wantSetup, lifecycle.provision, "the workflow's own choice reaches the runner")

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				stored, getErr := st.Get(t.Context(), "agent")
				require.NoError(t, getErr)
				require.NotNil(t, stored, "a refused setup leaves a row to look at rather than none")
				assert.Equal(t, agentstore.StateFailedToCreate, stored.State)
				assert.NotEmpty(t, stored.FailureReason, "and why")
				assert.Empty(t, fakeHarness.Launches, "and launches nothing")

				return
			}

			require.NoError(t, err)

			want := tt.wantDir
			if want == "" {
				want = asked
			}

			stored, err := st.Get(t.Context(), "agent")
			require.NoError(t, err)
			require.NotNil(t, stored)
			assert.Equal(t, want, stored.Cwd, "the row records where the agent actually runs")
			require.Len(t, fakeHarness.Launches, 1)
			assert.Equal(t, want, fakeHarness.Launches[0].Dir, "and the launch is told the same directory")
		})
	}
}

// TestNewAgentProvisionOverrideToNone pins the one thing an agent can say for itself about its
// directory: that it keeps the one it was given.
func TestNewAgentProvisionOverrideToNone(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	lifecycle := &fakeLifecycle{}
	fakeHarness := &harnesstest.Fake{}
	manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: fakeHarness}, lifecycle)

	created, err := newWorkflowService(st, tmux).NewWorkflow(t.Context(), worktreeConfig(t, "backend"))
	require.NoError(t, err)

	asked := t.TempDir()
	_, err = manager.launch.NewAgent(t.Context(), NewAgentParams{
		Title:            "agent",
		Dir:              asked,
		WorkflowID:       created.ID,
		ProvisionEnabled: xptr.Ptr(false),
	})
	require.NoError(t, err)

	assert.Empty(t, lifecycle.provision, "an explicit none refuses the workflow's worktree too")

	stored, err := st.Get(t.Context(), "agent")
	require.NoError(t, err)
	require.NotNil(t, stored)
	require.NotNil(t, stored.ProvisionEnabled)
	assert.False(t, *stored.ProvisionEnabled, "the override is what gets persisted")
	assert.Equal(t, asked, stored.Cwd, "and it runs where it was told to")
}

// TestNewAgentCeiling covers the ceiling, which has to refuse before anything is opened or
// a create refused after its workflow built a worktree leaves one behind every time.
func TestNewAgentCeiling(t *testing.T) {
	tests := []struct {
		name            string
		maxAgents       int
		existing        int
		archiveExisting bool
		wantErr         error
	}{
		{
			name:      "a create with room is untouched by the ceiling",
			maxAgents: 3,
			existing:  1,
		},
		{
			name:      "the create that would reach the ceiling is refused",
			maxAgents: 2,
			existing:  2,
			wantErr:   agenterr.ErrTooManyAgents,
		},
		{
			name:            "archiving one makes room again",
			maxAgents:       2,
			existing:        2,
			archiveExisting: true,
		},
		{
			name:      "a build that configures no ceiling refuses nothing",
			maxAgents: 0,
			existing:  5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := tmuxtest.NewFake()
			st := newStore(t)
			lifecycle := &fakeLifecycle{}
			manager := st.agentServiceCapped(tmux, lifecycle, tt.maxAgents)

			workflow, err := newWorkflowService(st, tmux).NewWorkflow(t.Context(), workflowstore.Config{
				Name:     "backend",
				Cwd:      fakeCheckout(t),
				Worktree: workflowstore.WorktreeConfig{Enabled: true},
			})
			require.NoError(t, err)

			for i := range tt.existing {
				name := fmt.Sprintf("existing-%d", i)
				seed(t, st, name, fmt.Sprintf("%%%d", i), agentstore.StateThinking)

				if tt.archiveExisting {
					require.NoError(t, st.SoftDelete(t.Context(), name))
				}
			}

			opened := len(tmux.NewWindows)

			created, err := manager.launch.NewAgent(t.Context(), NewAgentParams{
				Title:      "agent",
				Dir:        t.TempDir(),
				WorkflowID: workflow.ID,
			})

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				require.ErrorContains(t, err, "please archive one to continue", "the refusal says what to do about it")
				assert.Empty(t, lifecycle.provision, "a refused create runs no provisioner")
				assert.Len(t, tmux.NewWindows, opened, "and opens no window")

				return
			}

			require.NoError(t, err)
			require.NotNil(t, created)
		})
	}
}

// TestNewAgentCeilingAllowsARetriedLaunch covers the one create the ceiling does not measure,
// which is a retry carrying a key an agent already holds.
func TestNewAgentCeilingAllowsARetriedLaunch(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := st.agentServiceCapped(tmux, &fakeLifecycle{}, 1)

	params := NewAgentParams{
		Title:          "morning-ci",
		Dir:            t.TempDir(),
		Prompt:         "check CI",
		WorkflowID:     seedWorkflow(t, st),
		IdempotencyKey: "schedule:s1:2026-07-28T09:00:00Z",
	}

	first, err := manager.launch.NewAgent(t.Context(), params)
	require.NoError(t, err)
	require.NotNil(t, first)

	second, err := manager.launch.NewAgent(t.Context(), params)
	require.ErrorIs(t, err, agenterr.ErrAlreadyLaunched)
	require.NotNil(t, second)
	assert.Equal(t, first.ID, second.ID, "the retry finds the agent it already created")

	params.IdempotencyKey = ""
	_, err = manager.launch.NewAgent(t.Context(), params)
	require.ErrorIs(t, err, agenterr.ErrTooManyAgents)
}

// TestNewAgentCleansUpAfterASetupThatRan pins every refusal past the provisioner undoing its
// work.
func TestNewAgentCleansUpAfterASetupThatRan(t *testing.T) {
	tests := []struct {
		name        string
		answered    string
		windowErr   error
		setOwnerErr error
		respawnErr  error
	}{
		{
			name:     "a directory setup named that is not there",
			answered: filepath.Join(t.TempDir(), "never-created"),
		},
		{
			name:      "a window tmux refuses",
			answered:  t.TempDir(),
			windowErr: errors.New("tmux refused the window name"),
		},
		{
			name:        "a pane tmux refuses to stamp",
			answered:    t.TempDir(),
			setOwnerErr: errors.New("tmux refused the option"),
		},
		{
			name:       "a launch that never starts in the pane",
			answered:   t.TempDir(),
			respawnErr: errors.New("tmux refused the command"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := tmuxtest.NewFake()
			tmux.NewWindowErr = tt.windowErr
			tmux.SetOwnerErr = tt.setOwnerErr
			tmux.RespawnErr = tt.respawnErr
			st := newStore(t)
			lifecycle := &fakeLifecycle{dir: tt.answered}
			manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, lifecycle)

			created, err := newWorkflowService(st, tmux).NewWorkflow(t.Context(), workflowstore.Config{
				Name:     "backend",
				Cwd:      fakeCheckout(t),
				Worktree: workflowstore.WorktreeConfig{Enabled: true},
			})
			require.NoError(t, err)

			_, err = manager.launch.NewAgent(t.Context(), NewAgentParams{
				Title:      "agent",
				Dir:        t.TempDir(),
				WorkflowID: created.ID,
			})

			require.Error(t, err)
			assert.Equal(t, []string{scriptrun.WorktreeProvisioner}, lifecycle.provision)
			assert.Equal(t, []string{scriptrun.WorktreeDeprovisioner}, lifecycle.deprovision, "what setup built is taken back")

			if tt.windowErr != nil || tt.setOwnerErr != nil || tt.respawnErr != nil {
				require.Len(t, lifecycle.deprovisionCwd, 1)
				assert.Equal(t, tt.answered, lifecycle.deprovisionCwd[0], "cleanup runs against what setup actually built, not the workflow's planned dir")
			}

			agents, listErr := st.Agents.List(t.Context())
			require.NoError(t, listErr)
			require.Len(t, agents, 1, "the row stays, marked failed, rather than vanishing")
			assert.Equal(t, agentstore.StateFailedToCreate, agents[0].State)
			assert.NotEmpty(t, agents[0].FailureReason)
			assert.Empty(t, agents[0].Pane, "a failed create holds no pane")
		})
	}
}

// TestNewAgentDoesNotCleanUpASetupThatRefused pins no cleanup after a setup that refused,
// which would ask the script to undo work nobody did.
func TestNewAgentDoesNotCleanUpASetupThatRefused(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	lifecycle := &fakeLifecycle{provisionErr: ErrProvisionFailed}
	manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, lifecycle)

	created, err := newWorkflowService(st, tmux).NewWorkflow(t.Context(), workflowstore.Config{
		Name:     "backend",
		Cwd:      fakeCheckout(t),
		Worktree: workflowstore.WorktreeConfig{Enabled: true},
	})
	require.NoError(t, err)

	_, err = manager.launch.NewAgent(t.Context(), NewAgentParams{
		Title:      "agent",
		Dir:        t.TempDir(),
		WorkflowID: created.ID,
	})

	require.ErrorIs(t, err, ErrProvisionFailed)
	assert.Empty(t, lifecycle.deprovision)
}

// cancellingSetupLifecycle stands in for the caller going away while a provisioner is
// running: the create's own context is dead by the time the launch tries to write anything.
type cancellingSetupLifecycle struct {
	*fakeLifecycle
	cancel context.CancelFunc
}

func (c *cancellingSetupLifecycle) RunProvision(ctx context.Context, agent agentstore.Agent, name string) (string, error) {
	c.cancel()

	return c.fakeLifecycle.RunProvision(ctx, agent, name)
}

// TestNewAgentClosesOutACreateTheCallerAbandoned pins the write that marks a create failed
// outliving the request.
func TestNewAgentClosesOutACreateTheCallerAbandoned(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	lifecycle := &cancellingSetupLifecycle{fakeLifecycle: &fakeLifecycle{dir: t.TempDir()}, cancel: cancel}
	manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, lifecycle)

	created, err := newWorkflowService(st, tmux).NewWorkflow(t.Context(), workflowstore.Config{
		Name:     "backend",
		Cwd:      fakeCheckout(t),
		Worktree: workflowstore.WorktreeConfig{Enabled: true},
	})
	require.NoError(t, err)

	_, err = manager.launch.NewAgent(ctx, NewAgentParams{
		Title:      "agent",
		Dir:        t.TempDir(),
		WorkflowID: created.ID,
	})
	require.Error(t, err)

	agents, listErr := st.Agents.List(t.Context())
	require.NoError(t, listErr)
	require.Len(t, agents, 1)
	assert.Equal(t, agentstore.StateFailedToCreate, agents[0].State, "an abandoned create is not left setting up forever")
	assert.NotEmpty(t, agents[0].FailureReason, "the row says why it failed")

	assert.Equal(t, []string{scriptrun.WorktreeDeprovisioner}, lifecycle.deprovision, "what setup built is taken back")
	assert.Equal(t, []error{nil}, lifecycle.deprovisionCtxErr, "cleanup gets a context it can still read the workflow on")
}

// archivingSetupLifecycle stands in for the agent being archived while its provisioner is running.
type archivingSetupLifecycle struct {
	*fakeLifecycle
	archive func(id string) error
}

func (a *archivingSetupLifecycle) RunProvision(ctx context.Context, agent agentstore.Agent, name string) (string, error) {
	if err := a.archive(agent.ID); err != nil {
		return "", err
	}

	return a.fakeLifecycle.RunProvision(ctx, agent, name)
}

// TestNewAgentArchivedDuringSetupIsNotLaunched pins the window opened for an agent archived mid-setup being killed before any CLI starts in it.
func TestNewAgentArchivedDuringSetupIsNotLaunched(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	lifecycle := &archivingSetupLifecycle{fakeLifecycle: &fakeLifecycle{dir: t.TempDir()}}
	manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, lifecycle)
	lifecycle.archive = func(id string) error {
		return archiveWriters(t.Context(), manager.lifecycle, []string{id})
	}

	created, err := newWorkflowService(st, tmux).NewWorkflow(t.Context(), workflowstore.Config{
		Name:     "backend",
		Cwd:      fakeCheckout(t),
		Worktree: workflowstore.WorktreeConfig{Enabled: true},
	})
	require.NoError(t, err)

	_, err = manager.launch.NewAgent(t.Context(), NewAgentParams{
		Title:      "agent",
		Dir:        t.TempDir(),
		WorkflowID: created.ID,
	})

	require.ErrorIs(t, err, agenterr.ErrAgentArchived)
	agents, err := st.Agents.ListAll(t.Context())
	require.NoError(t, err)
	require.Len(t, agents, 1)
	require.NotNil(t, agents[0].ArchivedAt)
	assert.Empty(t, agents[0].Pane, "an archived agent holds no pane")
	assert.False(t, agents[0].Alive, "an archived agent is not alive")
	assert.Equal(t, agentstore.StateFailedToCreate, agents[0].State)
	assert.NotEmpty(t, agents[0].FailureReason)
	assert.Empty(t, tmux.Respawned, "no CLI is started for an archived agent")
	require.Equal(t, 1, tmux.WindowsOpened())
	assert.Contains(t, tmux.ReadKilledPanes(), "%"+agents[0].Name, "the window opened for an archived agent is killed")
}

// slowCleanupLifecycle finishes its cleanup only once the call that started it has had a chance to
// return.
type slowCleanupLifecycle struct {
	*fakeLifecycle
	finished atomic.Bool
}

func (c *slowCleanupLifecycle) RunDeprovision(ctx context.Context, agent agentstore.Agent, name string, done func(error)) error {
	if err := c.fakeLifecycle.RunDeprovision(ctx, agent, name, nil); err != nil {
		return err
	}

	go func() {
		clock.Sleep(context.WithoutCancel(ctx), 20*time.Millisecond)
		c.finished.Store(true)
		if done != nil {
			done(nil)
		}
	}()

	return nil
}

// TestNewAgentWaitsForTheCleanupOfAFailedCreate pins the teardown of a failed create being
// waited for.
func TestNewAgentWaitsForTheCleanupOfAFailedCreate(t *testing.T) {
	tmux := tmuxtest.NewFake()
	tmux.NewWindowErr = errors.New("tmux refused the window")
	st := newStore(t)
	lifecycle := &slowCleanupLifecycle{fakeLifecycle: &fakeLifecycle{dir: t.TempDir()}}
	manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, lifecycle)

	created, err := newWorkflowService(st, tmux).NewWorkflow(t.Context(), workflowstore.Config{
		Name:     "backend",
		Cwd:      fakeCheckout(t),
		Worktree: workflowstore.WorktreeConfig{Enabled: true},
	})
	require.NoError(t, err)

	_, err = manager.launch.NewAgent(t.Context(), NewAgentParams{
		Title:      "agent",
		Dir:        t.TempDir(),
		WorkflowID: created.ID,
	})
	require.Error(t, err)

	assert.True(t, lifecycle.finished.Load(), "the create returns only once its cleanup has finished")
}

// TestUniqueAgentName pins a taken name being stepped past rather than reused, counting only
// names taken inside the same workflow.
func TestUniqueAgentName(t *testing.T) {
	tests := []struct {
		name           string
		title          string
		prompt         string
		taken          []string
		takenElsewhere []string
		wantName       string
	}{
		{
			name:     "no title and no prompt falls back to a label",
			wantName: "agent",
		},
		{
			name:     "a taken title steps to the next free suffix",
			title:    "agent",
			taken:    []string{"agent"},
			wantName: "agent-2",
		},
		{
			name:           "the same title in another workflow needs no suffix",
			title:          "review",
			takenElsewhere: []string{"review"},
			wantName:       "review",
		},
		{
			name:     "characters tmux rejects are replaced",
			title:    "My Agent: v1.0",
			wantName: "My-Agent--v1-0",
		},
		{
			name:     "control characters are replaced",
			title:    "nightly\treview\x07",
			wantName: "nightly-review-",
		},
		{
			name:     "a blank title falls back to a slug of the prompt",
			prompt:   "Fix the login bug\nand more",
			wantName: "fix-the-login-bug",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			workflowID := seedWorkflow(t, st)
			for _, name := range tt.taken {
				seedInWorkflow(t, st, workflowID, name, "%1", agentstore.StateIdle)
			}
			for _, name := range tt.takenElsewhere {
				seed(t, st, name, "%2", agentstore.StateIdle)
			}

			manager := newManager(tmuxtest.NewFake(), st)

			assert.Equal(t, tt.wantName, manager.launch.uniqueAgentName(t.Context(), workflowID, tt.title, tt.prompt))
		})
	}
}

// TestNameTaken pins that a name is taken only within one workflow, so the same name is free
// in another.
func TestNameTaken(t *testing.T) {
	tests := []struct {
		name      string
		check     string
		elsewhere bool
		want      bool
	}{
		{
			name:  "a name nobody holds",
			check: "free",
			want:  false,
		},
		{
			name:      "a name held in another workflow",
			check:     "taken",
			elsewhere: true,
			want:      false,
		},
		{
			name:  "a name a live agent in this workflow holds",
			check: "taken",
			want:  true,
		},
	}

	st := newStore(t)
	workflowID := seedWorkflow(t, st)
	other := seedWorkflow(t, st)
	seedInWorkflow(t, st, workflowID, "taken", "%1", agentstore.StateIdle)
	manager := newManager(tmuxtest.NewFake(), st)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scope := workflowID
			if tt.elsewhere {
				scope = other
			}

			assert.Equal(t, tt.want, manager.launch.isNameTaken(t.Context(), scope, tt.check))
		})
	}
}

// TestNewAgentCeilingUnderConcurrency drives more creates than fit at once.
func TestNewAgentCeilingUnderConcurrency(t *testing.T) {
	t.Parallel()

	const (
		ceiling = 2
		callers = 8
	)

	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := st.agentServiceCapped(tmux, &fakeLifecycle{}, ceiling)
	workflowID := seedWorkflow(t, st)
	dir := t.TempDir()

	var wg sync.WaitGroup
	errs := make([]error, callers)

	for i := range callers {
		wg.Add(1)

		go func() {
			defer wg.Done()

			_, err := manager.launch.NewAgent(t.Context(), NewAgentParams{
				Title:      fmt.Sprintf("racer-%d", i),
				Dir:        dir,
				WorkflowID: workflowID,
			})
			errs[i] = err
		}()
	}

	wg.Wait()

	live, err := st.Agents.CountLive(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(ceiling), live)

	for i, err := range errs {
		if err != nil {
			require.ErrorIs(t, err, agenterr.ErrTooManyAgents, "caller %d", i)
		}
	}

	panes, err := tmux.GetPanes()
	require.NoError(t, err)
	require.Len(t, panes, ceiling)
}

// TestConcurrentLaunchesGiveATaskOneWriter pins two launches racing onto a writerless task leaving
// it one writer, the other refused.
func TestConcurrentLaunchesGiveATaskOneWriter(t *testing.T) {
	ctx := t.Context()
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, &fakeLifecycle{})
	workflow, err := newWorkflowService(st, tmux).NewWorkflow(ctx, worktreeConfig(t, "backend"))
	require.NoError(t, err)
	work := st.SeedTask(t, taskstore.CreateParams{WorkflowID: workflow.ID, Slug: "work"})

	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Go(func() {
			_, errs[i] = manager.launch.NewAgent(ctx, NewAgentParams{
				Title:      fmt.Sprintf("writer-%d", i),
				Prompt:     "go",
				WorkflowID: workflow.ID,
				Task:       taskByID(work.ID),
			})
		})
	}
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			require.ErrorIs(t, err, tasks.ErrHasWriter)
		}
	}
	onTask, err := st.Agents.ListOnTasks(ctx, []string{work.ID})
	require.NoError(t, err)
	writers := 0
	for _, agent := range onTask {
		if agent.Role == agentstore.RoleWriter {
			writers++
		}
	}
	assert.Equal(t, 1, writers)
}

// TestNewReviewerTakesItsLaunchFromItsTask pins what a reviewer is given rather than asked for:
// its task's checkout, no provisioner of its own, and its task's workflow.
func TestNewReviewerTakesItsLaunchFromItsTask(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := newManager(tmux, st)
	ctx := t.Context()

	dir := t.TempDir()
	writer, err := manager.launch.NewAgent(ctx, NewAgentParams{
		Title:      "author",
		Dir:        dir,
		Prompt:     "write the change",
		WorkflowID: seedWorkflow(t, st),
	})
	require.NoError(t, err)

	reviewer, err := manager.launch.NewAgent(ctx, reviewerOf(*writer, "reviewer"))
	require.NoError(t, err)
	require.NotNil(t, reviewer)

	assert.Equal(t, agentstore.RoleReviewer, reviewer.Role)
	assert.Equal(t, dir, reviewer.Cwd, "a reviewer runs where its task's writer runs")
	assert.Equal(t, writer.TaskID, reviewer.TaskID)
	assert.Equal(t, writer.WorkflowID, reviewer.WorkflowID)

	// An unset provisioner falls back to the workflow's, which for a worktree-enabled workflow
	// would tear the writer's checkout down, so the blank has to be on the row itself.
	require.NotNil(t, reviewer.ProvisionEnabled)
	assert.False(t, *reviewer.ProvisionEnabled)

	assert.Empty(t, provisionerFor(ctx, st.Workflows, *reviewer), "and a provisioner turned off leaves nothing to run against it")
}

// TestNewReviewerRefusals pins each reviewer a launch refuses and records nothing for.
func TestNewReviewerRefusals(t *testing.T) {
	tests := []struct {
		name          string
		prepareWriter func(t *testing.T, st *testStore, writer agentstore.Agent) agentstore.Agent
		params        func(writer agentstore.Agent) NewAgentParams
		wantErrIs     error
	}{
		{
			name: "a directory of its own",
			params: func(writer agentstore.Agent) NewAgentParams {
				params := reviewerOf(writer, "reviewer")
				params.Dir = "/tmp"

				return params
			},
			wantErrIs: agenterr.ErrReviewerOwnsLaunch,
		},
		{
			name: "a provisioner of its own",
			params: func(writer agentstore.Agent) NewAgentParams {
				params := reviewerOf(writer, "reviewer")
				params.ProvisionEnabled = xptr.Ptr(true)

				return params
			},
			wantErrIs: agenterr.ErrReviewerOwnsLaunch,
		},
		{
			name: "a schedule of its own",
			params: func(writer agentstore.Agent) NewAgentParams {
				params := reviewerOf(writer, "reviewer")
				params.ScheduleID = xptr.Ptr("b9b1a1de-0000-7000-8000-000000000000")

				return params
			},
			wantErrIs: agenterr.ErrReviewerOwnsLaunch,
		},
		{
			name: "a workflow other than its task's",
			params: func(writer agentstore.Agent) NewAgentParams {
				params := reviewerOf(writer, "reviewer")
				params.WorkflowID = "b9b1a1de-0000-7000-8000-000000000000"

				return params
			},
			wantErrIs: tasks.ErrOtherWorkflow,
		},
		{
			name: "a task made as it joins one",
			params: func(writer agentstore.Agent) NewAgentParams {
				params := reviewerOf(writer, "reviewer")
				params.TaskSlug = "other-work"

				return params
			},
			wantErrIs: errTaskMadeAndJoined,
		},
		{
			name: "a task nothing answers to",
			params: func(writer agentstore.Agent) NewAgentParams {
				params := reviewerOf(writer, "reviewer")
				params.Task = taskByID("b9b1a1de-0000-7000-8000-000000000000")

				return params
			},
			wantErrIs: store.ErrNotFound,
		},
		{
			name: "a task already archived",
			prepareWriter: func(t *testing.T, st *testStore, writer agentstore.Agent) agentstore.Agent {
				t.Helper()
				require.NoError(t, st.Tasks.Archive(t.Context(), *writer.TaskID))

				return writer
			},
			wantErrIs: tasks.ErrArchived,
		},
		{
			name: "a task being deleted",
			prepareWriter: func(t *testing.T, st *testStore, writer agentstore.Agent) agentstore.Agent {
				t.Helper()
				require.NoError(t, st.Tasks.MarkDeleting(t.Context(), []string{*writer.TaskID}))

				return writer
			},
			wantErrIs: taskstore.ErrDeleting,
		},
		{
			name: "a task whose worktree was removed",
			prepareWriter: func(t *testing.T, st *testStore, writer agentstore.Agent) agentstore.Agent {
				t.Helper()
				require.NoError(t, st.Tasks.SetHead(t.Context(), *writer.TaskID, taskstore.SetHeadParams{WorktreeRemovalReason: taskstore.WorktreeGone}))

				return writer
			},
			wantErrIs: tasks.ErrWorktreeRemoved,
		},
		{
			name: "a writer with no directory",
			prepareWriter: func(t *testing.T, st *testStore, writer agentstore.Agent) agentstore.Agent {
				t.Helper()
				require.NoError(t, st.Agents.Update(t.Context(), withCwd(writer, "")))

				return writer
			},
			wantErrIs: agenterr.ErrReviewerNeedsDirectory,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := tmuxtest.NewFake()
			st := newStore(t)
			manager := newManager(tmux, st)

			writer := withCwd(seedRunning(t, st, tmux, "author", "%author", agentstore.StateIdle), t.TempDir())
			require.NoError(t, st.Agents.Update(t.Context(), writer))
			if tt.prepareWriter != nil {
				writer = tt.prepareWriter(t, st, writer)
			}

			params := reviewerOf(writer, "second-reviewer")
			if tt.params != nil {
				params = tt.params(writer)
			}

			before, err := st.Agents.ListAll(t.Context())
			require.NoError(t, err)

			created, err := manager.launch.NewAgent(t.Context(), params)
			require.ErrorIs(t, err, tt.wantErrIs)
			assert.Nil(t, created)

			after, err := st.Agents.ListAll(t.Context())
			require.NoError(t, err)
			assert.Len(t, after, len(before), "the refusal records nothing")
		})
	}
}

// TestNewAgentWorksOnATask pins which task a new agent is put on.
func TestNewAgentWorksOnATask(t *testing.T) {
	tests := []struct {
		name      string
		params    func(workflowID string, work, elsewhere *taskstore.Task) NewAgentParams
		wantErrIs error
		wantTask  string
		wantGoal  string
	}{
		{
			name: "a task in another workflow is refused",
			params: func(workflowID string, _, elsewhere *taskstore.Task) NewAgentParams {
				return NewAgentParams{
					Title:      "fix-login",
					Prompt:     "go",
					WorkflowID: workflowID,
					Task:       taskByID(elsewhere.ID),
				}
			},
			wantErrIs: tasks.ErrOtherWorkflow,
		},
		{
			name: "a task named by id is joined",
			params: func(workflowID string, work, _ *taskstore.Task) NewAgentParams {
				return NewAgentParams{
					Title:      "fix-login",
					Prompt:     "go",
					WorkflowID: workflowID,
					Task:       taskByID(work.ID),
				}
			},
			wantTask: "work",
		},
		{
			name: "no task makes one of its own under an unused slug",
			params: func(workflowID string, _, _ *taskstore.Task) NewAgentParams {
				return NewAgentParams{
					Title:      "work",
					Prompt:     "Fix the login page.\nMore.",
					WorkflowID: workflowID,
				}
			},
			wantTask: "work-2",
		},
		{
			name: "joining a task and describing one to make is refused",
			params: func(workflowID string, work, _ *taskstore.Task) NewAgentParams {
				return NewAgentParams{
					Title:      "fix-login",
					Prompt:     "go",
					WorkflowID: workflowID,
					Task:       taskByID(work.ID),
					TaskSlug:   "login",
				}
			},
			wantErrIs: errTaskMadeAndJoined,
		},
		{
			name: "a slug asked for that a task already has is refused",
			params: func(workflowID string, work, _ *taskstore.Task) NewAgentParams {
				return NewAgentParams{
					Title:      "fix-login",
					Prompt:     "go",
					WorkflowID: workflowID,
					TaskSlug:   work.Slug,
				}
			},
			wantErrIs: store.ErrNameTaken,
		},
		{
			name: "a slug and a goal asked for make the task as given",
			params: func(workflowID string, _, _ *taskstore.Task) NewAgentParams {
				return NewAgentParams{
					Title:      "Fix login",
					Prompt:     "Look at it.",
					WorkflowID: workflowID,
					TaskSlug:   "login",
					TaskGoal:   "Logins work.",
				}
			},
			wantTask: "login",
			wantGoal: "Logins work.",
		},
		{
			name: "an orchestrator is put on no task",
			params: func(workflowID string, _, _ *taskstore.Task) NewAgentParams {
				return NewAgentParams{
					Title:      "lead",
					Prompt:     "go",
					WorkflowID: workflowID,
					Role:       agentstore.RoleOrchestrator,
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := tmuxtest.NewFake()
			st := newStore(t)
			fakeHarness := &harnesstest.Fake{}
			manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: fakeHarness}, &fakeLifecycle{})
			workflow, err := newWorkflowService(st, tmux).NewWorkflow(t.Context(), worktreeConfig(t, "backend"))
			require.NoError(t, err)
			workflowID := workflow.ID
			work := st.SeedTask(t, taskstore.CreateParams{WorkflowID: workflowID, Slug: "work"})
			elsewhere := st.SeedTask(t, taskstore.CreateParams{Slug: "elsewhere"})

			params := tt.params(workflowID, work, elsewhere)
			params.Dir = t.TempDir()
			created, err := manager.launch.NewAgent(t.Context(), params)
			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)
				assert.Nil(t, created)

				listed, listErr := st.Agents.ListAll(t.Context())
				require.NoError(t, listErr)
				assert.Empty(t, listed, "the refusal records nothing")

				return
			}
			require.NoError(t, err)

			require.Len(t, fakeHarness.Launches, 1)
			briefing := fakeHarness.Launches[0].Context
			stored := agentByID(t, st, created.ID)
			if tt.wantTask == "" {
				assert.Nil(t, stored.TaskID)
				assert.NotContains(t, briefing, "You work on task")

				return
			}
			require.NotNil(t, stored.TaskID)
			task, err := st.Tasks.Get(t.Context(), *stored.TaskID)
			require.NoError(t, err)
			assert.Equal(t, tt.wantTask, task.Slug)
			if tt.wantGoal != "" {
				assert.Equal(t, tt.wantGoal, task.Goal)
			}
			assert.Equal(t, workflowID, task.WorkflowID)
			assert.Contains(t, briefing, "You work on task "+tt.wantTask+".", "the agent is told its task")
		})
	}
}

// TestNewAgentClaimsItsTasksCheckout pins that a task's writer makes its checkout, and who else
// may join the task once it has.
func TestNewAgentClaimsItsTasksCheckout(t *testing.T) {
	tests := []struct {
		name         string
		noWorktrees  bool
		second       func(first agentstore.Agent, task *taskstore.Task, dir string) NewAgentParams
		wantErrIs    error
		wantCheckout bool
	}{
		{
			name: "a second agent with a directory of its own is refused",
			second: func(first agentstore.Agent, task *taskstore.Task, dir string) NewAgentParams {
				return NewAgentParams{
					Title:      "second",
					Prompt:     "go",
					WorkflowID: first.WorkflowID,
					Task:       taskByID(task.ID),
					Dir:        dir,
				}
			},
			wantErrIs:    agenterr.ErrReviewerOwnsLaunch,
			wantCheckout: true,
		},
		{
			name: "a reviewer works in its writer's checkout",
			second: func(first agentstore.Agent, _ *taskstore.Task, _ string) NewAgentParams {
				return reviewerOf(first, "reviewer")
			},
			wantCheckout: true,
		},
		{
			name:        "a reviewer in a workflow without worktrees claims no checkout",
			noWorktrees: true,
			second: func(first agentstore.Agent, _ *taskstore.Task, _ string) NewAgentParams {
				return reviewerOf(first, "reviewer")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := tmuxtest.NewFake()
			st := newStore(t)
			manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, &fakeLifecycle{})
			cfg := worktreeConfig(t, "backend")
			if tt.noWorktrees {
				cfg.Worktree = workflowstore.WorktreeConfig{}
			}
			workflow, err := newWorkflowService(st, tmux).NewWorkflow(t.Context(), cfg)
			require.NoError(t, err)
			work := st.SeedTask(t, taskstore.CreateParams{WorkflowID: workflow.ID, Slug: "work"})

			first, err := manager.launch.NewAgent(t.Context(), NewAgentParams{
				Title:      "writer",
				Prompt:     "go",
				WorkflowID: workflow.ID,
				Task:       taskByID(work.ID),
				Dir:        t.TempDir(),
			})
			require.NoError(t, err)

			_, err = manager.launch.NewAgent(t.Context(), tt.second(*first, work, t.TempDir()))
			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)
			} else {
				require.NoError(t, err)
			}

			task, err := st.Tasks.Get(t.Context(), work.ID)
			require.NoError(t, err)
			want := ""
			if tt.wantCheckout {
				want = agentByID(t, st, first.ID).Cwd
			}
			assert.Equal(t, want, task.Checkout.Cwd)
		})
	}
}

// TestRetryRecordsItsTasksCheckout covers a retried writer taking up the checkout claim its
// failed create left behind.
func TestRetryRecordsItsTasksCheckout(t *testing.T) {
	tmux := tmuxtest.NewFake()
	tmux.RespawnErr = errors.New("bad flags")
	st := newStore(t)
	manager := st.agentServiceWithLifecycle(tmux, harnesstest.Registry{Harness: &harnesstest.Fake{}}, &fakeLifecycle{})
	workflow, err := newWorkflowService(st, tmux).NewWorkflow(t.Context(), worktreeConfig(t, "backend"))
	require.NoError(t, err)
	work := st.SeedTask(t, taskstore.CreateParams{WorkflowID: workflow.ID, Slug: "work"})

	_, err = manager.launch.NewAgent(t.Context(), NewAgentParams{
		Title:      "writer",
		Prompt:     "go",
		WorkflowID: workflow.ID,
		Task:       taskByID(work.ID),
		Dir:        t.TempDir(),
	})
	require.Error(t, err)
	failed, err := st.Agents.List(t.Context())
	require.NoError(t, err)
	require.Len(t, failed, 1)
	tmux.RespawnErr = nil

	_, err = manager.launch.RetryCreate(t.Context(), RetryCreateParams{ID: failed[0].ID})
	require.NoError(t, err)

	task, err := st.Tasks.Get(t.Context(), work.ID)
	require.NoError(t, err)
	assert.Equal(t, agentByID(t, st, failed[0].ID).Cwd, task.Checkout.Cwd)
}

// TestNewReviewerUnderTheReviewerCeiling pins the cap on the reviewers one task holds.
func TestNewReviewerUnderTheReviewerCeiling(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := st.agentService(agentServiceOptions{
		tmux:      tmux,
		harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}},
		addr:      "127.0.0.1:9999",
		usage:     config.Usage{Task: config.TaskUsage{MaxReviewers: 1}},
	})
	ctx := t.Context()

	writer := seedRunning(t, st, tmux, "author", "%author", agentstore.StateIdle)
	require.NoError(t, st.Agents.Update(ctx, withCwd(writer, t.TempDir())))

	join := func(title string) error {
		_, err := manager.launch.NewAgent(ctx, reviewerOf(writer, title))

		return err
	}

	require.NoError(t, join("reviewer"))
	require.ErrorIs(t, join("second-reviewer"), agenterr.ErrTooManyReviewers)
}

// TestNewAgentUnderTheTaskCeiling pins the cap on the tasks one orchestrator starts, which leaves
// their reviewers out.
func TestNewAgentUnderTheTaskCeiling(t *testing.T) {
	tests := []struct {
		name      string
		maxTasks  int
		reviewer  bool
		wantErrIs error
	}{
		{
			name:      "a second task past a limit of one is refused",
			maxTasks:  1,
			wantErrIs: agenterr.ErrTooManyTasks,
		},
		{
			name:     "a reviewer on a task it started does not count",
			maxTasks: 1,
			reviewer: true,
		},
		{
			name: "no limit of its own leaves max_agents as the cap",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := tmuxtest.NewFake()
			st := newStore(t)
			manager := st.agentService(agentServiceOptions{
				tmux:      tmux,
				harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}},
				addr:      "127.0.0.1:9999",
				usage: config.Usage{
					Task:         config.TaskUsage{MaxReviewers: 3},
					Orchestrator: config.OrchestratorUsage{MaxTasks: tt.maxTasks},
				},
			})

			orchestrator := seedOrchestrator(t, st, tmux)
			ctx := caller.With(t.Context(), caller.Caller{AgentID: orchestrator.ID})

			first, err := manager.launch.NewAgent(ctx, NewAgentParams{
				Title:      "first",
				Dir:        t.TempDir(),
				Prompt:     "do the thing",
				WorkflowID: orchestrator.WorkflowID,
			})
			require.NoError(t, err)

			second := NewAgentParams{
				Title:      "second",
				Dir:        t.TempDir(),
				Prompt:     "do the other thing",
				WorkflowID: orchestrator.WorkflowID,
			}
			if tt.reviewer {
				second = reviewerOf(*first, "reader")
			}

			_, err = manager.launch.NewAgent(ctx, second)
			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)

				return
			}

			require.NoError(t, err)
		})
	}
}

// TestRestoreUnderTheTaskCeiling pins that bringing back a task an orchestrator started is held
// to the same cap as starting one.
func TestRestoreUnderTheTaskCeiling(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := st.agentService(agentServiceOptions{
		tmux:      tmux,
		harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}},
		addr:      "127.0.0.1:9999",
		lifecycle: &fakeLifecycle{},
		usage:     config.Usage{Orchestrator: config.OrchestratorUsage{MaxTasks: 1}},
	})

	orchestrator := seedOrchestrator(t, st, tmux)
	ctx := caller.With(t.Context(), caller.Caller{AgentID: orchestrator.ID})

	spawn := func(title string) *agentstore.Agent {
		created, err := manager.launch.NewAgent(ctx, NewAgentParams{
			Title:      title,
			Dir:        t.TempDir(),
			Prompt:     "do the thing",
			WorkflowID: orchestrator.WorkflowID,
		})
		require.NoError(t, err)

		return created
	}

	first := spawn("first")
	require.NoError(t, archiveWriters(t.Context(), manager.lifecycle, []string{first.ID}))
	spawn("second")

	_, err := manager.lifecycle.RestoreAgents(t.Context(), []string{first.ID})
	require.ErrorIs(t, err, agenterr.ErrTooManyTasks)
}

func reviewerOf(writer agentstore.Agent, title string) NewAgentParams {
	return NewAgentParams{
		Title:      title,
		Prompt:     "read the change",
		WorkflowID: writer.WorkflowID,
		Task:       taskByID(*writer.TaskID),
	}
}

func withCwd(agent agentstore.Agent, dir string) agentstore.Agent {
	agent.Cwd = dir

	return agent
}

// seedOrchestrator records the agent folded onto a workflow's own row.
func seedOrchestrator(t *testing.T, st *testStore, tmux paneStamper) agentstore.Agent {
	t.Helper()
	ctx := t.Context()

	const name = "orchestrator"
	workflowID := seedWorkflow(t, st)

	require.NoError(t, st.createAgent(ctx, agentstore.Agent{
		Name:       name,
		Pane:       "%" + name,
		State:      agentstore.StateIdle,
		WorkflowID: workflowID,
		Role:       agentstore.RoleOrchestrator,
	}))

	got, err := st.Agents.GetInWorkflow(ctx, workflowID, name)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NoError(t, tmux.SetPaneOwner(got.Pane, got.ID))

	return *got
}

// TestNewAgentGetsHarness pins which CLI a new agent lands on, taking what the caller asked
// for ahead of the workflow's default and the configured one.
func TestNewAgentGetsHarness(t *testing.T) {
	tests := []struct {
		name      string
		requested string
		workflow  string
		want      string
	}{
		{
			name: "neither the agent nor its workflow names one, so it takes the configured default",
			want: harnesstest.FakeID,
		},
		{
			name:     "the workflow's default when the agent names none",
			workflow: "codex",
			want:     "codex",
		},
		{
			name:      "what the caller asked for beats the workflow",
			requested: "codex",
			workflow:  harnesstest.FakeID,
			want:      "codex",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			tmux := tmuxtest.NewFake()

			service := st.agentService(agentServiceOptions{
				tmux:      tmux,
				harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}},
			})

			created, err := st.Workflows.Create(t.Context(), workflowstore.Config{
				Name:     "workflow-" + uuid.NewString(),
				Harness:  tt.workflow,
				Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"},
			})
			require.NoError(t, err)

			_, err = service.launch.NewAgent(t.Context(), NewAgentParams{
				Title:      "agent",
				Dir:        t.TempDir(),
				Harness:    tt.requested,
				WorkflowID: created.ID,
			})
			require.NoError(t, err)

			stored, err := st.Get(t.Context(), "agent")
			require.NoError(t, err)
			require.NotNil(t, stored)
			assert.Equal(t, tt.want, stored.Harness)
		})
	}
}

// TestWorkflowFlagsFollowItsHarness pins that a workflow's flags reach only the agents running
// the CLI it names.
func TestWorkflowFlagsFollowItsHarness(t *testing.T) {
	tests := []struct {
		name      string
		requested string
		want      string
	}{
		{
			name: "an agent on the workflow's own CLI takes its flags",
			want: "--default-flag --model opus",
		},
		{
			name:      "an agent on another CLI is launched without them",
			requested: "codex",
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			tmux := tmuxtest.NewFake()

			service := st.agentService(agentServiceOptions{
				tmux:      tmux,
				harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}},
				flags:     "--default-flag",
			})

			created, err := st.Workflows.Create(t.Context(), workflowstore.Config{
				Name:     "workflow-" + uuid.NewString(),
				Harness:  harnesstest.FakeID,
				Flags:    "--model opus",
				Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"},
			})
			require.NoError(t, err)

			_, err = service.launch.NewAgent(t.Context(), NewAgentParams{
				Title:      "agent",
				Dir:        t.TempDir(),
				Harness:    tt.requested,
				WorkflowID: created.ID,
			})
			require.NoError(t, err)

			stored, err := st.Get(t.Context(), "agent")
			require.NoError(t, err)
			require.NotNil(t, stored)
			assert.Equal(t, tt.want, stored.Flags)
		})
	}
}

// TestReviewerTakesTheWorkflowsReviewerLaunch pins that blank reviewer flags fall back to the workflow's only on the workflow's own CLI.
func TestReviewerTakesTheWorkflowsReviewerLaunch(t *testing.T) {
	tests := []struct {
		name            string
		role            agentstore.Role
		reviewerHarness string
		reviewerFlags   string
		wantHarness     string
		wantFlags       string
	}{
		{
			name:        "a reviewer in a workflow naming no reviewer launch takes the workflow's own",
			role:        agentstore.RoleReviewer,
			wantHarness: harnesstest.FakeID,
			wantFlags:   "--default-flag --model opus",
		},
		{
			name:            "a reviewer takes the reviewer CLI and flags",
			role:            agentstore.RoleReviewer,
			reviewerHarness: "codex",
			reviewerFlags:   "--model gpt-5",
			wantHarness:     "codex",
			wantFlags:       "--model gpt-5",
		},
		{
			name:            "a reviewer on another CLI with no reviewer flags is launched without the workflow's",
			role:            agentstore.RoleReviewer,
			reviewerHarness: "codex",
			wantHarness:     "codex",
			wantFlags:       "",
		},
		{
			name:          "reviewer flags alone keep the workflow's CLI",
			role:          agentstore.RoleReviewer,
			reviewerFlags: "--model sonnet",
			wantHarness:   harnesstest.FakeID,
			wantFlags:     "--default-flag --model sonnet",
		},
		{
			name:            "a writer ignores the reviewer launch",
			role:            agentstore.RoleWriter,
			reviewerHarness: "codex",
			reviewerFlags:   "--model gpt-5",
			wantHarness:     harnesstest.FakeID,
			wantFlags:       "--default-flag --model opus",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			tmux := tmuxtest.NewFake()

			service := st.agentService(agentServiceOptions{
				tmux:      tmux,
				harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}},
				flags:     "--default-flag",
			})

			created, err := st.Workflows.Create(t.Context(), workflowstore.Config{
				Name:            "workflow-" + uuid.NewString(),
				Harness:         harnesstest.FakeID,
				Flags:           "--model opus",
				ReviewerHarness: tt.reviewerHarness,
				ReviewerFlags:   tt.reviewerFlags,
				Worktree:        workflowstore.WorktreeConfig{BaseBranch: "main"},
			})
			require.NoError(t, err)

			_, err = service.launch.NewAgent(t.Context(), NewAgentParams{
				Title:      "agent",
				Dir:        t.TempDir(),
				WorkflowID: created.ID,
				Role:       tt.role,
			})
			require.NoError(t, err)

			stored, err := st.Get(t.Context(), "agent")
			require.NoError(t, err)
			require.NotNil(t, stored)
			assert.Equal(t, tt.wantHarness, stored.Harness)
			assert.Equal(t, tt.wantFlags, stored.Flags)
		})
	}
}

// TestNewAgentRefusesUnknownHarness pins that a CLI this binary cannot run is refused before
// a window is opened or a row written.
func TestNewAgentRefusesUnknownHarness(t *testing.T) {
	st := newStore(t)
	tmux := tmuxtest.NewFake()
	service := st.agentService(agentServiceOptions{
		tmux:      tmux,
		harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}, Only: []string{harnesstest.FakeID}},
	})

	_, err := service.launch.NewAgent(t.Context(), NewAgentParams{
		Title:      "agent",
		Dir:        t.TempDir(),
		Harness:    "gemini",
		WorkflowID: seedWorkflow(t, st),
	})

	require.ErrorIs(t, err, harness.ErrNotRegistered)

	agents, listErr := st.Agents.List(t.Context())
	require.NoError(t, listErr)
	assert.Empty(t, agents, "a refused harness records nothing")
	assert.Empty(t, tmux.NewWindows, "and opens no window")
	assert.Empty(t, tmux.Respawned, "and starts nothing")
}

// TestNewAgentFlagsFollowTheHarness pins that an agent inherits only the flags configured for
// the CLI it runs.
func TestNewAgentFlagsFollowTheHarness(t *testing.T) {
	st := newStore(t)
	tmux := tmuxtest.NewFake()

	service := NewLaunchService(NewLaunchServiceParams{
		Tmux:               tmux,
		Agents:             st.Agents,
		Tasks:              tasks.NewTaskService(tasks.NewTaskServiceParams{Tasks: st.Tasks}),
		Workflows:          st.Workflows,
		Tx:                 st.Pool,
		Launcher:           NewLauncher(NewLauncherParams{Tmux: tmux}),
		Harnesses:          harnesstest.Registry{Harness: &harnesstest.Fake{}},
		UserDefaultHarness: harnesstest.FakeID,
		UserDefaultFlags: map[string]string{
			harnesstest.FakeID: "--model opus",
			"codex":            "--sandbox read-only",
		},
	})

	_, err := service.NewAgent(t.Context(), NewAgentParams{
		Title:      "agent",
		Dir:        t.TempDir(),
		Harness:    "codex",
		Flags:      "--extra",
		WorkflowID: seedWorkflow(t, st),
	})
	require.NoError(t, err)

	stored, err := st.Get(t.Context(), "agent")
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, "--sandbox read-only --extra", stored.Flags,
		"the flags configured for the harness it runs, then its own")
}

// TestLaunchFlags pins that the user defaults are never prepended to flags already holding them.
func TestLaunchFlags(t *testing.T) {
	tests := []struct {
		name   string
		params launchFlagsParams
		want   string
	}{
		{
			name: "flags that already carry the defaults are not given them twice",
			params: launchFlagsParams{
				harnessID:  "codex",
				agentFlags: "--dangerously-bypass-approvals-and-sandbox -c 'approval=\"approve\"' --extra",
			},
			want: "--dangerously-bypass-approvals-and-sandbox -c 'approval=\"approve\"' --extra",
		},
		{
			name: "workflow flags that already carry the defaults are not given them twice",
			params: launchFlagsParams{
				harnessID:       "codex",
				workflowHarness: "codex",
				workflowFlags:   "--dangerously-bypass-approvals-and-sandbox -c 'approval=\"approve\"'",
			},
			want: "--dangerously-bypass-approvals-and-sandbox -c 'approval=\"approve\"'",
		},
		{
			name: "a flag that only starts like the defaults still gets them",
			params: launchFlagsParams{
				harnessID:  harnesstest.FakeID,
				agentFlags: "--model opusplan",
			},
			want: "--model opus --model opusplan",
		},
		{
			name: "workflow flags for another CLI are dropped",
			params: launchFlagsParams{
				harnessID:       "codex",
				workflowHarness: harnesstest.FakeID,
				workflowFlags:   "--model opus",
			},
			want: "--dangerously-bypass-approvals-and-sandbox -c 'approval=\"approve\"'",
		},
		{
			name: "a CLI with no defaults takes its own flags",
			params: launchFlagsParams{
				harnessID:  "gemini",
				agentFlags: "--yolo",
			},
			want: "--yolo",
		},
		{
			name: "the defaults go ahead of the agent's own flags",
			params: launchFlagsParams{
				harnessID:  "codex",
				agentFlags: "--extra",
			},
			want: "--dangerously-bypass-approvals-and-sandbox -c 'approval=\"approve\"' --extra",
		},
	}

	service := &LaunchService{
		userDefaultFlags: map[string]string{
			"codex":            "--dangerously-bypass-approvals-and-sandbox -c 'approval=\"approve\"'",
			harnesstest.FakeID: "--model opus",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, service.launchFlags(tt.params))
		})
	}
}
