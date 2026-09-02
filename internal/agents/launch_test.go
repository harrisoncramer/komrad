package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"weave/internal/caller"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewAgent covers a launch end to end, where a refused one records nothing, since tmux
// reads an empty pane target as whatever the user is looking at.
func TestNewAgent(t *testing.T) {
	tests := []struct {
		name           string
		dir            func(t *testing.T) string
		title          string
		windowErr      error
		sendKeysErr    error
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
			name:           "a launch that never reaches the pane's shell",
			dir:            func(t *testing.T) string { return t.TempDir() },
			title:          "stillborn",
			sendKeysErr:    errors.New("tmux dropped the keys"),
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
			tmux := newFakeTmux()
			tmux.newWindowErr = tt.windowErr
			tmux.sendKeysErr = tt.sendKeysErr
			tmux.setOwnerErr = tt.setOwnerErr
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

				assert.Empty(t, tmux.sent, "nothing is typed when the launch was refused")

				agents, listErr := st.List(t.Context())
				require.NoError(t, listErr)

				if tt.wantsFailedRow {
					require.Len(t, agents, 1, "a launch that had already claimed a slot leaves a row to look at")
					assert.Equal(t, agentstore.StateFailedToCreate, agents[0].State)
					assert.Contains(t, metadataValue(agents[0].Metadata), tt.wantErr, "and why, for the row that is left")
				} else {
					assert.Empty(t, agents, "a refused launch before any slot was claimed records nothing")
				}

				if tt.wantPaneKilled {
					assert.NotEmpty(t, tmux.killedPanes, "the open pane goes with the record")
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
			assert.NotEmpty(t, tmux.lastKeys("%My-Agent"), "the agent is launched into the new pane")
			assert.Equal(t, stored.ID, tmux.owners["%My-Agent"], "the pane is stamped with the agent it is running")
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
			tmux := newFakeTmux()
			st := newStore(t)
			manager := newManager(tmux, st, "")
			dir := t.TempDir()

			params := NewAgentParams{
				Title: "morning-ci", Dir: dir, Prompt: "check CI",
				WorkflowID: seedWorkflow(t, st), IdempotencyKey: tt.first,
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

			agents, err := st.List(t.Context())
			require.NoError(t, err)

			if tt.wantSame {
				assert.Equal(t, first.ID, second.ID, "the retry finds the agent it already created")
				assert.Len(t, agents, 1, "and no second agent is recorded")
				assert.Len(t, tmux.sent, 1, "nor is a second agent started")

				return
			}

			assert.NotEqual(t, first.ID, second.ID)
			assert.Len(t, agents, 2)
		})
	}
}

// TestNewAgentRetriesUnderTheSameKey covers what a retry of one firing does about the agent
// the previous attempt left behind, since answering it with a create that failed is what
// turned one launch failure into an hourly run that silently never happened.
func TestNewAgentRetriesUnderTheSameKey(t *testing.T) {
	const key = "schedule:s1:2026-07-28T09:00:00Z"

	tests := []struct {
		name         string
		state        string
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
			tmux := newFakeTmux()
			st := newStore(t)
			manager := newManager(tmux, st, "")

			params := NewAgentParams{
				Title: "morning-ci", Dir: t.TempDir(), Prompt: "check CI",
				WorkflowID: seedWorkflow(t, st), IdempotencyKey: key,
			}

			first, err := manager.launch.NewAgent(t.Context(), params)
			require.NoError(t, err)
			require.NoError(t, st.Agents.SetStateByID(t.Context(), first.ID, tt.state))

			second, err := manager.launch.NewAgent(t.Context(), params)
			require.NotNil(t, second)

			all, listErr := st.ListAll(t.Context())
			require.NoError(t, listErr)

			if !tt.wantRelaunch {
				require.ErrorIs(t, err, ErrAlreadyLaunched)
				assert.Equal(t, first.ID, second.ID, "the retry is answered with the agent already under the key")
				assert.Len(t, all, 1, "and records no second agent")
				assert.Len(t, tmux.sent, 1, "nor starts one")

				return
			}

			require.NoError(t, err, "a failed create never launched, so the retry is free to")
			assert.NotEqual(t, first.ID, second.ID)
			assert.Len(t, all, 2, "the agent that failed is kept, since it says why it failed")
			assert.Len(t, tmux.sent, 2, "and the retry actually starts something")

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
			tmux := newFakeTmux()
			st := newStore(t)
			manager, h := newManagerWithHarness(tmux, st, "")

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
				Title: "delegate", Dir: t.TempDir(), Prompt: "do the thing",
				WorkflowID: seedWorkflow(t, st),
			})

			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)
				assert.Empty(t, tmux.sent, "nothing is typed when the caller cannot be attributed")

				agents, listErr := st.List(t.Context())
				require.NoError(t, listErr)
				assert.Empty(t, agents, "a refused create records nothing")

				return
			}
			require.NoError(t, err)
			require.NotNil(t, created)
			require.Len(t, h.launches, 1)
			launch := h.launches[0]

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
// since tmux does no tilde expansion.
func TestNewAgentGetsDirectory(t *testing.T) {
	tests := []struct {
		name        string
		dir         string
		workflowCwd string
		useClient   bool
	}{
		{
			name:        "a blank directory takes the workflow's",
			workflowCwd: "~/chariot/chariot",
		},
		{
			name:        "what the caller asked for wins over the workflow's",
			dir:         "~/chariot/chariot",
			workflowCwd: "/no/such/directory",
		},
		{
			name:      "with neither, the invoking client's directory answers",
			useClient: true,
		},
		{
			name: "a ~ path is expanded before tmux sees it",
			dir:  "~/chariot/chariot",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)

			want := filepath.Join(home, "chariot", "chariot")
			require.NoError(t, os.MkdirAll(want, 0o755))

			tmux := newFakeTmux()
			if tt.useClient {
				tmux.clientDir = want
			}

			st := newStore(t)
			manager := newManager(tmux, st, "")

			created, err := workflowsFor(st, tmux).NewWorkflow(t.Context(), workflowstore.Config{Name: "backend", Cwd: tt.workflowCwd})
			require.NoError(t, err)

			_, err = manager.launch.NewAgent(t.Context(), NewAgentParams{
				Title: "scoped", Dir: tt.dir, WorkflowID: created.ID,
			})
			require.NoError(t, err)

			assert.Equal(t, want, tmux.newWindows["scoped"], "tmux is given an absolute path")

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
			tmux := newFakeTmux()
			st := newStore(t)
			manager, h := newManagerWithHarness(tmux, st, "")

			created, err := workflowsFor(st, tmux).NewWorkflow(t.Context(), workflowstore.Config{
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

			require.Len(t, h.launches, 1)
			prompt := h.launches[0].Context
			assert.Contains(t, prompt, "/mcp", "every agent is told about the endpoint")
			if tt.wantContext {
				assert.Contains(t, prompt, "shared notes")
				return
			}

			assert.NotContains(t, prompt, "shared notes")
		})
	}
}

// TestNewAgentRunsWorkflowSetup covers the two things a workflow's setup action decides
// about a create, which are whether it happens at all and where the agent ends up running.
func TestNewAgentRunsWorkflowSetup(t *testing.T) {
	provisioned := t.TempDir()

	tests := []struct {
		name         string
		action       string
		answered     string
		provisionErr error
		wantDir      string
		wantSetup    []string
		wantErr      error
	}{
		{
			name:         "setup that fails abandons the create",
			action:       "make-worktree",
			provisionErr: ErrProvisionFailed,
			wantSetup:    []string{"make-worktree"},
			wantErr:      ErrProvisionFailed,
		},
		{
			name:      "a directory setup names that is not usable abandons the create",
			action:    "make-worktree",
			answered:  filepath.Join(t.TempDir(), "never-created"),
			wantSetup: []string{"make-worktree"},
			wantErr:   ErrProvisionFailed,
		},
		{
			name: "a workflow naming none launches where it was told to",
		},
		{
			name:      "setup that names no directory leaves the create where it was",
			action:    "install-deps",
			wantSetup: []string{"install-deps"},
		},
		{
			name:      "the directory setup names is where the agent runs",
			action:    "make-worktree",
			answered:  provisioned,
			wantDir:   provisioned,
			wantSetup: []string{"make-worktree"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := newFakeTmux()
			st := newStore(t)
			lifecycle := &fakeLifecycle{dir: tt.answered, provisionErr: tt.provisionErr}
			h := &fakeHarness{}
			manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: h}, lifecycle)

			created, err := workflowsFor(st, tmux).NewWorkflow(t.Context(), workflowstore.Config{
				Name:            "backend",
				ProvisionAction: tt.action,
			})
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
				assert.Empty(t, h.launches, "and launches nothing")

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
			require.Len(t, h.launches, 1)
			assert.Equal(t, want, h.launches[0].Dir, "and the launch is told the same directory")
		})
	}
}

// TestNewAgentProvisionActionOverride pins that an agent's own setup action is the one run,
// rather than the workflow's.
func TestNewAgentProvisionActionOverride(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	lifecycle := &fakeLifecycle{}
	h := &fakeHarness{}
	manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: h}, lifecycle)

	created, err := workflowsFor(st, tmux).NewWorkflow(t.Context(), workflowstore.Config{
		Name:            "backend",
		ProvisionAction: "make-worktree",
	})
	require.NoError(t, err)

	_, err = manager.launch.NewAgent(t.Context(), NewAgentParams{
		Title:           "agent",
		Dir:             t.TempDir(),
		WorkflowID:      created.ID,
		ProvisionAction: xptr.Ptr("install-deps"),
	})
	require.NoError(t, err)

	assert.Equal(t, []string{"install-deps"}, lifecycle.provision, "the agent's own choice runs instead of the workflow's")

	stored, err := st.Get(t.Context(), "agent")
	require.NoError(t, err)
	require.NotNil(t, stored)
	require.NotNil(t, stored.ProvisionAction)
	assert.Equal(t, "install-deps", *stored.ProvisionAction, "the override is what gets persisted, not the workflow's action")
}

// TestNewAgentProvisionActionOverrideToNone pins that an override set to nothing means no setup
// at all rather than falling back to the workflow.
func TestNewAgentProvisionActionOverrideToNone(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	lifecycle := &fakeLifecycle{}
	h := &fakeHarness{}
	manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: h}, lifecycle)

	created, err := workflowsFor(st, tmux).NewWorkflow(t.Context(), workflowstore.Config{
		Name:            "backend",
		ProvisionAction: "make-worktree",
	})
	require.NoError(t, err)

	_, err = manager.launch.NewAgent(t.Context(), NewAgentParams{
		Title:           "agent",
		Dir:             t.TempDir(),
		WorkflowID:      created.ID,
		ProvisionAction: xptr.Ptr(""),
	})
	require.NoError(t, err)

	assert.Empty(t, lifecycle.provision, "an explicit none refuses the workflow's action too")

	stored, err := st.Get(t.Context(), "agent")
	require.NoError(t, err)
	require.NotNil(t, stored)
	require.NotNil(t, stored.ProvisionAction)
	assert.Empty(t, *stored.ProvisionAction)
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
			tmux := newFakeTmux()
			st := newStore(t)
			lifecycle := &fakeLifecycle{}
			manager := st.agentServiceCapped(tmux, lifecycle, tt.maxAgents)

			workflow, err := workflowsFor(st, tmux).NewWorkflow(t.Context(), workflowstore.Config{
				Name:            "backend",
				ProvisionAction: "make-worktree",
			})
			require.NoError(t, err)

			for i := range tt.existing {
				name := fmt.Sprintf("existing-%d", i)
				seed(t, st, name, fmt.Sprintf("%%%d", i), agentstore.StateThinking)

				if tt.archiveExisting {
					require.NoError(t, st.SoftDelete(t.Context(), name))
				}
			}

			opened := len(tmux.newWindows)

			created, err := manager.launch.NewAgent(t.Context(), NewAgentParams{
				Title:      "agent",
				Dir:        t.TempDir(),
				WorkflowID: workflow.ID,
			})

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Contains(t, err.Error(), "please archive one to continue", "the refusal says what to do about it")
				assert.Empty(t, lifecycle.provision, "a refused create runs no setup action")
				assert.Len(t, tmux.newWindows, opened, "and opens no window")

				return
			}

			require.NoError(t, err)
			require.NotNil(t, created)
		})
	}
}

// TestNewAgentCeilingAllowsARetriedLaunch covers the one create the ceiling does not
// measure, since a retry carrying a key an agent already holds creates nothing.
func TestNewAgentCeilingAllowsARetriedLaunch(t *testing.T) {
	tmux := newFakeTmux()
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

// TestNewAgentCleansUpAfterASetupThatRan pins every refusal past the setup script undoing
// its work, since a worktree no row names is never reached again.
func TestNewAgentCleansUpAfterASetupThatRan(t *testing.T) {
	tests := []struct {
		name        string
		answered    string
		windowErr   error
		setOwnerErr error
		sendKeysErr error
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
			name:        "a launch that never reaches the pane's shell",
			answered:    t.TempDir(),
			sendKeysErr: errors.New("tmux dropped the keys"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := newFakeTmux()
			tmux.newWindowErr = tt.windowErr
			tmux.setOwnerErr = tt.setOwnerErr
			tmux.sendKeysErr = tt.sendKeysErr
			st := newStore(t)
			lifecycle := &fakeLifecycle{dir: tt.answered}
			manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: &fakeHarness{}}, lifecycle)

			created, err := workflowsFor(st, tmux).NewWorkflow(t.Context(), workflowstore.Config{
				Name:              "backend",
				ProvisionAction:   "make-worktree",
				DeprovisionAction: "drop-worktree",
			})
			require.NoError(t, err)

			_, err = manager.launch.NewAgent(t.Context(), NewAgentParams{
				Title:      "agent",
				Dir:        t.TempDir(),
				WorkflowID: created.ID,
			})

			require.Error(t, err)
			assert.Equal(t, []string{"make-worktree"}, lifecycle.provision)
			assert.Equal(t, []string{"drop-worktree"}, lifecycle.deprovision, "what setup built is taken back")

			if tt.windowErr != nil || tt.setOwnerErr != nil || tt.sendKeysErr != nil {
				require.Len(t, lifecycle.deprovisionCwd, 1)
				assert.Equal(t, tt.answered, lifecycle.deprovisionCwd[0], "cleanup runs against what setup actually built, not the workflow's planned dir")
			}

			agents, listErr := st.List(t.Context())
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
	tmux := newFakeTmux()
	st := newStore(t)
	lifecycle := &fakeLifecycle{provisionErr: ErrProvisionFailed}
	manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: &fakeHarness{}}, lifecycle)

	created, err := workflowsFor(st, tmux).NewWorkflow(t.Context(), workflowstore.Config{
		Name:              "backend",
		ProvisionAction:   "make-worktree",
		DeprovisionAction: "drop-worktree",
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

// cancellingSetupLifecycle stands in for the caller going away while a setup action is
// running: the create's own context is dead by the time the launch tries to write anything.
type cancellingSetupLifecycle struct {
	*fakeLifecycle
	cancel context.CancelFunc
}

func (c *cancellingSetupLifecycle) RunProvisionAction(ctx context.Context, agent agentstore.Agent, name, pane string) (string, error) {
	c.cancel()

	return c.fakeLifecycle.RunProvisionAction(ctx, agent, name, pane)
}

// TestNewAgentClosesOutACreateTheCallerAbandoned pins the write that marks a create failed
// outliving the request, since nothing else moves a row out of setting_up.
func TestNewAgentClosesOutACreateTheCallerAbandoned(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	lifecycle := &cancellingSetupLifecycle{fakeLifecycle: &fakeLifecycle{dir: t.TempDir()}, cancel: cancel}
	manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: &fakeHarness{}}, lifecycle)

	created, err := workflowsFor(st, tmux).NewWorkflow(t.Context(), workflowstore.Config{
		Name:              "backend",
		ProvisionAction:   "make-worktree",
		DeprovisionAction: "drop-worktree",
	})
	require.NoError(t, err)

	_, err = manager.launch.NewAgent(ctx, NewAgentParams{
		Title:      "agent",
		Dir:        t.TempDir(),
		WorkflowID: created.ID,
	})
	require.Error(t, err)

	agents, listErr := st.List(t.Context())
	require.NoError(t, listErr)
	require.Len(t, agents, 1)
	assert.Equal(t, agentstore.StateFailedToCreate, agents[0].State, "an abandoned create is not left setting up forever")
	assert.NotEmpty(t, metadataValue(agents[0].Metadata), "the row says why it failed")

	assert.Equal(t, []string{"drop-worktree"}, lifecycle.deprovision, "what setup built is taken back")
	assert.Equal(t, []error{nil}, lifecycle.deprovisionCtxErr, "cleanup gets a context it can still read the workflow on")
}

// TestNewAgentCleansUpAFailedCreateWithNoProvisionAction pins the deliberate reading of a
// workflow that declares cleanup but no setup: a failed create still hands the row to
// cleanup, against the dir it was going to run in, the same way deleting such an agent does.
func TestNewAgentCleansUpAFailedCreateWithNoProvisionAction(t *testing.T) {
	tmux := newFakeTmux()
	tmux.newWindowErr = errors.New("tmux refused the window")
	st := newStore(t)
	lifecycle := &fakeLifecycle{}
	manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: &fakeHarness{}}, lifecycle)

	created, err := workflowsFor(st, tmux).NewWorkflow(t.Context(), workflowstore.Config{
		Name:              "backend",
		DeprovisionAction: "drop-worktree",
	})
	require.NoError(t, err)

	dir := t.TempDir()
	_, err = manager.launch.NewAgent(t.Context(), NewAgentParams{
		Title:      "agent",
		Dir:        dir,
		WorkflowID: created.ID,
	})
	require.Error(t, err)

	assert.Empty(t, lifecycle.provision)
	assert.Equal(t, []string{"drop-worktree"}, lifecycle.deprovision)
	assert.Equal(t, []string{dir}, lifecycle.deprovisionCwd)
}

// slowCleanupLifecycle finishes its cleanup only once the call that started it has had a
// chance to return, which is what tells an awaited teardown apart from a fired one.
type slowCleanupLifecycle struct {
	*fakeLifecycle
	finished atomic.Bool
}

func (c *slowCleanupLifecycle) RunDeprovisionAction(ctx context.Context, agent agentstore.Agent, name string, done func(error)) error {
	if err := c.fakeLifecycle.RunDeprovisionAction(ctx, agent, name, nil); err != nil {
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
// waited for, since the row is closed out on the next line and a directory still being taken
// back under it is a create that half happened.
func TestNewAgentWaitsForTheCleanupOfAFailedCreate(t *testing.T) {
	tmux := newFakeTmux()
	tmux.newWindowErr = errors.New("tmux refused the window")
	st := newStore(t)
	lifecycle := &slowCleanupLifecycle{fakeLifecycle: &fakeLifecycle{dir: t.TempDir()}}
	manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: &fakeHarness{}}, lifecycle)

	created, err := workflowsFor(st, tmux).NewWorkflow(t.Context(), workflowstore.Config{
		Name:              "backend",
		ProvisionAction:   "make-worktree",
		DeprovisionAction: "drop-worktree",
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

			manager := newManager(newFakeTmux(), st, "")

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
	manager := newManager(newFakeTmux(), st, "")

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
