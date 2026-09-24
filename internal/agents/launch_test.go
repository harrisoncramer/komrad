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

	"weave/internal/caller"
	"weave/internal/harness"
	"weave/internal/harnesstest"
	agentstore "weave/internal/store/agent"
	reviewstore "weave/internal/store/review"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/tmuxtest"
	"weave/lib/xptr"

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
			wantErrIs:   ErrWorkflowNotFound,
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
			manager := newManager(tmux, st, "--default-flag")

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
					assert.Contains(t, metadataValue(agents[0].Metadata), tt.wantErr, "and why, for the row that is left")
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
			assert.NotEmpty(t, stored.SessionID, "a conversation id is pinned at launch")
			assert.NotEmpty(t, tmux.LastRespawn("%My-Agent").Argv, "the agent is launched into the new pane")
			assert.Equal(t, stored.ID, tmux.Owners["%My-Agent"], "the pane is stamped with the agent it is running")
			assert.True(t, manager.panes.IsAgentRunning(*stored), "and the agent reads as running in it")
		})
	}
}

func metadataValue(md []agentstore.Metadata) string {
	for _, entry := range md {
		if entry.Key == "error" {
			return entry.Value
		}
	}

	return ""
}

// TestNewAgentIdempotency pins a retry finding the agent it already made, or the same prompt
// runs twice in two tmux windows.
func TestNewAgentIdempotency(t *testing.T) {
	tests := []struct {
		name     string
		first    string
		second   string
		wantSame bool
		wantErr  error
	}{
		{
			name:     "the same key returns the agent already created",
			first:    "schedule:s1:2026-07-28T09:00:00Z",
			second:   "schedule:s1:2026-07-28T09:00:00Z",
			wantSame: true,
			wantErr:  ErrAlreadyLaunched,
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
			manager := newManager(tmux, st, "")
			dir := t.TempDir()

			params := NewAgentParams{
				Title:          "morning-ci",
				Dir:            dir,
				Prompt:         "check CI",
				WorkflowID:     seedWorkflow(t, st),
				IdempotencyKey: tt.first,
			}
			first, err := manager.launch.NewAgent(t.Context(), params)
			require.NoError(t, err)
			require.NotNil(t, first)

			params.IdempotencyKey = tt.second
			second, err := manager.launch.NewAgent(t.Context(), params)
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
				assert.Len(t, agents, 1, "and no second agent is recorded")
				assert.Len(t, tmux.Respawned, 1, "nor is a second agent started")

				return
			}

			assert.NotEqual(t, first.ID, second.ID)
			assert.Len(t, agents, 2)
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
			manager := newManager(tmux, st, "")

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
				require.ErrorIs(t, err, ErrAlreadyLaunched)
				assert.Equal(t, first.ID, second.ID, "the retry is answered with the agent already under the key")
				assert.Len(t, all, 1, "and records no second agent")
				assert.Len(t, tmux.Respawned, 1, "nor starts one")

				return
			}

			require.NoError(t, err, "a failed create never launched, so the retry is free to")
			assert.NotEqual(t, first.ID, second.ID)
			assert.Len(t, all, 2, "the agent that failed is kept, since it says why it failed")
			assert.Len(t, tmux.Respawned, 2, "and the retry actually starts something")

			held, err := st.Agents.GetByIdempotencyKey(t.Context(), key)
			require.NoError(t, err)
			require.NotNil(t, held)
			assert.Equal(t, second.ID, held.ID, "the key moves to the agent that launched")
		})
	}
}

// TestNewAgentRecordsWhoSpawnedIt pins the caller header becoming the new agent's parent,
// which the panel sends none of so what it creates has no parent.
func TestNewAgentRecordsWhoSpawnedIt(t *testing.T) {
	tests := []struct {
		name           string
		spawnerName    string
		unknownSpawner bool
		wantErrIs      error
	}{
		{
			name:           "a caller header naming no agent is refused",
			unknownSpawner: true,
			wantErrIs:      ErrSpawnerUnknown,
		},
		{
			name: "a person creating from the panel records no parent",
		},
		{
			name:        "an agent creating another records its own id as the parent",
			spawnerName: "reviewer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := tmuxtest.NewFake()
			st := newStore(t)
			manager, fakeHarness := newManagerWithHarness(tmux, st, "")

			ctx := t.Context()
			var spawner *agentstore.Agent
			switch {
			case tt.unknownSpawner:
				ctx = caller.With(ctx, caller.Caller{AgentID: "no-such-agent"})
			case tt.spawnerName != "":
				seeded := seed(t, st, tt.spawnerName, "%"+tt.spawnerName, agentstore.StateIdle)
				spawner = &seeded
				ctx = caller.With(ctx, caller.Caller{AgentID: seeded.ID})
			}

			created, err := manager.launch.NewAgent(ctx, NewAgentParams{
				Title:      "delegate",
				Dir:        t.TempDir(),
				Prompt:     "do the thing",
				WorkflowID: seedWorkflow(t, st),
			})

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
				assert.Nil(t, created.SpawnedByAgentID, "a create with no caller header names no parent")
				assert.NotContains(t, launch.Context, "You were spawned by", "a delegate briefing with nothing to say names nobody")

				return
			}

			require.NotNil(t, created.SpawnedByAgentID)
			assert.Equal(t, spawner.ID, *created.SpawnedByAgentID)
			assert.Contains(t, launch.Context, fmt.Sprintf("spawned by agent %s (%s)", spawner.Name, spawner.ID))
			assert.Contains(t, launch.Context, "tell it when you are done")
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
			manager := newManager(tmux, st, "")

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
			wantSetup:    []string{worktreeProvisioner},
			wantErr:      ErrProvisionFailed,
		},
		{
			name:      "a directory setup names that is not usable abandons the create",
			worktree:  true,
			answered:  filepath.Join(t.TempDir(), "never-created"),
			wantSetup: []string{worktreeProvisioner},
			wantErr:   ErrProvisionFailed,
		},
		{
			name: "a workflow provisioning nothing launches where it was told to",
		},
		{
			name:      "setup that names no directory leaves the create where it was",
			worktree:  true,
			wantSetup: []string{worktreeProvisioner},
		},
		{
			name:      "the directory setup names is where the agent runs",
			worktree:  true,
			answered:  provisioned,
			wantDir:   provisioned,
			wantSetup: []string{worktreeProvisioner},
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
				assert.NotEmpty(t, metadataValue(stored.Metadata), "and why")
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
			wantErr:   ErrTooManyAgents,
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
				assert.Contains(t, err.Error(), "please archive one to continue", "the refusal says what to do about it")
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
	require.ErrorIs(t, err, ErrAlreadyLaunched)
	require.NotNil(t, second)
	assert.Equal(t, first.ID, second.ID, "the retry finds the agent it already created")

	params.IdempotencyKey = ""
	_, err = manager.launch.NewAgent(t.Context(), params)
	require.ErrorIs(t, err, ErrTooManyAgents)
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
			assert.Equal(t, []string{worktreeProvisioner}, lifecycle.provision)
			assert.Equal(t, []string{worktreeDeprovisioner}, lifecycle.deprovision, "what setup built is taken back")

			if tt.windowErr != nil || tt.setOwnerErr != nil || tt.respawnErr != nil {
				require.Len(t, lifecycle.deprovisionCwd, 1)
				assert.Equal(t, tt.answered, lifecycle.deprovisionCwd[0], "cleanup runs against what setup actually built, not the workflow's planned dir")
			}

			agents, listErr := st.Agents.List(t.Context())
			require.NoError(t, listErr)
			require.Len(t, agents, 1, "the row stays, marked failed, rather than vanishing")
			assert.Equal(t, agentstore.StateFailedToCreate, agents[0].State)
			assert.NotEmpty(t, metadataValue(agents[0].Metadata))
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
	assert.NotEmpty(t, metadataValue(agents[0].Metadata), "the row says why it failed")

	assert.Equal(t, []string{worktreeDeprovisioner}, lifecycle.deprovision, "what setup built is taken back")
	assert.Equal(t, []error{nil}, lifecycle.deprovisionCtxErr, "cleanup gets a context it can still read the workflow on")
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
		time.Sleep(20 * time.Millisecond)
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

			manager := newManager(tmuxtest.NewFake(), st, "")

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
	manager := newManager(tmuxtest.NewFake(), st, "")

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
			require.ErrorIs(t, err, ErrTooManyAgents, "caller %d", i)
		}
	}

	panes, err := tmux.GetPanes()
	require.NoError(t, err)
	require.Len(t, panes, ceiling)
}

// TestNewChildAgentTakesItsLaunchFromTheParent pins what a child is given rather than asked
// for: the parent's checkout, no provisioner of its own, and the parent's workflow.
func TestNewChildAgentTakesItsLaunchFromTheParent(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := newManager(tmux, st, "")
	ctx := t.Context()

	dir := t.TempDir()
	parent, err := manager.launch.NewAgent(ctx, NewAgentParams{
		Title:      "author",
		Dir:        dir,
		Prompt:     "write the change",
		WorkflowID: seedWorkflow(t, st),
	})
	require.NoError(t, err)

	child, err := manager.launch.NewAgent(ctx, NewAgentParams{
		Title:         "reviewer",
		Prompt:        "read the change",
		WorkflowID:    parent.WorkflowID,
		ParentAgentID: parent.ID,
	})
	require.NoError(t, err)
	require.NotNil(t, child)

	assert.Equal(t, dir, child.Cwd, "a child runs where its parent runs")
	require.NotNil(t, child.ParentAgentID)
	assert.Equal(t, parent.ID, *child.ParentAgentID)
	assert.Equal(t, parent.WorkflowID, child.WorkflowID)

	// An unset provisioner falls back to the workflow's, which for a worktree-enabled workflow
	// would tear the parent's checkout down, so the blank has to be on the row itself.
	require.NotNil(t, child.ProvisionEnabled)
	assert.False(t, *child.ProvisionEnabled)

	assert.Empty(t, deprovisionerFor(ctx, st.Workflows, *child), "and a provisioner turned off leaves nothing to run against it")
}

// TestNewChildAgentRefusals pins each child a launch refuses and records nothing for.
func TestNewChildAgentRefusals(t *testing.T) {
	tests := []struct {
		name          string
		prepareParent func(t *testing.T, st *testStore, parent agentstore.Agent) agentstore.Agent
		params        func(parent agentstore.Agent) NewAgentParams
		wantErrIs     error
	}{
		{
			name: "a directory of its own",
			params: func(parent agentstore.Agent) NewAgentParams {
				params := childOf(parent, "reviewer")
				params.Dir = "/tmp"

				return params
			},
			wantErrIs: ErrChildOwnsLaunch,
		},
		{
			name: "a provisioner of its own",
			params: func(parent agentstore.Agent) NewAgentParams {
				params := childOf(parent, "reviewer")
				params.ProvisionEnabled = xptr.Ptr(true)

				return params
			},
			wantErrIs: ErrChildOwnsLaunch,
		},
		{
			name: "a workflow other than its parent's",
			params: func(parent agentstore.Agent) NewAgentParams {
				params := childOf(parent, "reviewer")
				params.WorkflowID = "b9b1a1de-0000-7000-8000-000000000000"

				return params
			},
			wantErrIs: ErrChildOwnsLaunch,
		},
		{
			name: "a parent nothing answers to",
			params: func(parent agentstore.Agent) NewAgentParams {
				params := childOf(parent, "reviewer")
				params.ParentAgentID = "b9b1a1de-0000-7000-8000-000000000000"

				return params
			},
			wantErrIs: ErrParentNotFound,
		},
		{
			name: "a parent already archived",
			prepareParent: func(t *testing.T, st *testStore, parent agentstore.Agent) agentstore.Agent {
				t.Helper()
				require.NoError(t, st.SoftDelete(t.Context(), parent.Name))

				return parent
			},
			wantErrIs: ErrAgentArchived,
		},
		{
			name: "a parent with no directory",
			prepareParent: func(t *testing.T, st *testStore, parent agentstore.Agent) agentstore.Agent {
				t.Helper()
				require.NoError(t, st.Agents.Update(t.Context(), withCwd(parent, "")))

				return parent
			},
			wantErrIs: ErrChildNeedsParentDirectory,
		},
		{
			name: "a parent that is itself a child",
			prepareParent: func(t *testing.T, st *testStore, parent agentstore.Agent) agentstore.Agent {
				t.Helper()

				return seedChild(t, st, tmuxtest.NewFake(), parent)
			},
			wantErrIs: ErrParentIsChild,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := tmuxtest.NewFake()
			st := newStore(t)
			manager := newManager(tmux, st, "")

			parent := withCwd(seedRunning(t, st, tmux, "author", "%author", agentstore.StateIdle), t.TempDir())
			require.NoError(t, st.Agents.Update(t.Context(), parent))
			if tt.prepareParent != nil {
				parent = tt.prepareParent(t, st, parent)
			}

			params := childOf(parent, "second-reviewer")
			if tt.params != nil {
				params = tt.params(parent)
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

// TestNewChildAgentUnderTheChildCeiling pins the per-parent cap.
func TestNewChildAgentUnderTheChildCeiling(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := st.agentService(agentServiceOptions{
		tmux:        tmux,
		harnesses:   harnesstest.Registry{Harness: &harnesstest.Fake{}},
		addr:        "127.0.0.1:9999",
		maxChildren: 1,
	})
	ctx := t.Context()

	parent := seedRunning(t, st, tmux, "author", "%author", agentstore.StateIdle)
	require.NoError(t, st.Agents.Update(ctx, withCwd(parent, t.TempDir())))

	attach := func(title string) error {
		_, err := manager.launch.NewAgent(ctx, childOf(parent, title))

		return err
	}

	require.NoError(t, attach("reviewer"))
	require.ErrorIs(t, attach("second-reviewer"), ErrTooManyChildren)
}

func childOf(parent agentstore.Agent, title string) NewAgentParams {
	return NewAgentParams{
		Title:         title,
		Prompt:        "read the change",
		WorkflowID:    parent.WorkflowID,
		ParentAgentID: parent.ID,
	}
}

func withCwd(agent agentstore.Agent, dir string) agentstore.Agent {
	agent.Cwd = dir

	return agent
}

// TestNewChildAgentRefusesAnOrchestratorParent pins that the agent folded onto a workflow's own row
// holds no children.
func TestNewChildAgentRefusesAnOrchestratorParent(t *testing.T) {
	tmux := tmuxtest.NewFake()
	st := newStore(t)
	manager := newManager(tmux, st, "")
	ctx := t.Context()

	parent := seedOrchestrator(t, st, tmux)
	require.NoError(t, st.Agents.Update(ctx, withCwd(parent, t.TempDir())))

	created, err := manager.launch.NewAgent(ctx, childOf(parent, "reader"))
	require.ErrorIs(t, err, ErrParentIsOrchestrator)
	assert.Nil(t, created)
	require.ErrorContains(t, err, "provisionEnabled false", "the refusal names the shape to use instead")

	live, err := st.Agents.List(ctx)
	require.NoError(t, err)
	assert.Len(t, live, 1, "the refusal records nothing beside the orchestrator")
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
		Reviews:            reviewstore.NewReviewStore(st.Pool.GetDB()),
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
