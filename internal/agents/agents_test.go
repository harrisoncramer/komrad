package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"weave/internal/actions"
	"weave/internal/caller"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	transcriptstore "weave/internal/store/transcript"
	"weave/internal/store/workflow"
	"weave/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewAgent covers a launch end to end. A refused launch has to record
// nothing. A row pointing at a pane that does not exist reads as a live agent
// forever, and tmux reads an empty pane target as whatever the user is
// looking at.
func TestNewAgent(t *testing.T) {
	tests := []struct {
		name        string
		dir         func(t *testing.T) string
		title       string
		windowErr   error
		sendKeysErr error
		setOwnerErr error
		badWorkflow bool
		wantErrIs   error
		wantErr     string
		// wantPaneKilled says the window was already open when the launch failed, so
		// tearing the agent down has to take the pane with it.
		wantPaneKilled bool
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
			name:      "a window tmux refuses",
			dir:       func(t *testing.T) string { return t.TempDir() },
			title:     "doomed",
			windowErr: errors.New("tmux refused the window name"),
			wantErr:   "failed to open a window",
		},
		{
			// Without the stamp there is nothing tying the pane to the agent, so the row
			// would claim a pane nothing can verify is still its own. That is the state
			// this whole mechanism exists to avoid, so the launch fails instead.
			name:           "a pane tmux refuses to stamp",
			dir:            func(t *testing.T) string { return t.TempDir() },
			title:          "unstamped",
			setOwnerErr:    errors.New("tmux refused the option"),
			wantErr:        "failed to stamp the pane",
			wantPaneKilled: true,
		},
		{
			// The row is written before the command is typed, so a launch that dies here has
			// left one behind. It has to be taken back out or it lists as a live agent with
			// nothing running in it.
			name:           "a launch that never reaches the pane's shell",
			dir:            func(t *testing.T) string { return t.TempDir() },
			title:          "stillborn",
			sendKeysErr:    errors.New("tmux dropped the keys"),
			wantErr:        "failed to launch the agent",
			wantPaneKilled: true,
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

			created, err := manager.NewAgent(t.Context(), params)

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
				assert.Empty(t, agents, "a refused launch records nothing")

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
			assert.True(t, manager.IsAgentRunning(*stored), "and the agent reads as running in it")
		})
	}
}

// TestNewAgentIdempotency covers the key that makes a launch repeatable. A
// background job that retries after a launch half-succeeded has to find the
// agent it already made, or the same prompt runs twice in two tmux windows.
func TestNewAgentIdempotency(t *testing.T) {
	tests := []struct {
		name string
		// first and second are the keys the two calls claim.
		first  string
		second string
		// wantSame says the second call should return the first call's agent
		// rather than create another.
		wantSame bool
	}{
		{
			name:     "the same key returns the agent already created",
			first:    "schedule:s1:2026-07-28T09:00:00Z",
			second:   "schedule:s1:2026-07-28T09:00:00Z",
			wantSame: true,
		},
		{
			name:   "the next occurrence is a different agent",
			first:  "schedule:s1:2026-07-28T09:00:00Z",
			second: "schedule:s1:2026-07-29T09:00:00Z",
		},
		{
			// Two interactive creates claim nothing, so neither is the other, and
			// the unique index has to tolerate both.
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
			first, err := manager.NewAgent(t.Context(), params)
			require.NoError(t, err)
			require.NotNil(t, first)

			params.IdempotencyKey = tt.second
			second, err := manager.NewAgent(t.Context(), params)
			require.NoError(t, err)
			require.NotNil(t, second)

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

// TestNewAgentRecordsWhoSpawnedIt covers the header a request calling create_agent
// arrives with. An agent calling back through its own tools carries its own id in the
// caller header, which weave records as the new agent's parent and repeats to it in its
// launch context; the panel calls with no header at all, so what it creates has no parent.
func TestNewAgentRecordsWhoSpawnedIt(t *testing.T) {
	tests := []struct {
		name string
		// spawnerName seeds a parent agent under that name and puts its id on the
		// context as the caller; empty means the call carries no caller at all.
		spawnerName string
		// unknownSpawner puts an id on the context that names no agent at all,
		// which is a call weave cannot attribute rather than one it should trust.
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

			created, err := manager.NewAgent(ctx, NewAgentParams{
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

// TestNewAgentGetsDirectory covers where an agent starts. tmux does no tilde
// expansion, so a ~ path has to be absolute before the window is opened or the
// stored cwd would not be where the agent is running.
func TestNewAgentGetsDirectory(t *testing.T) {
	tests := []struct {
		name string
		dir  string
		// workflowCwd is the directory the enclosing workflow supplies, which is
		// what a launch with no directory of its own takes.
		workflowCwd string
		useClient   bool
	}{
		{
			name:        "a blank directory takes the workflow's",
			workflowCwd: "~/chariot/chariot",
		},
		{
			name: "what the caller asked for wins over the workflow's",
			dir:  "~/chariot/chariot",
			// Somewhere the test never creates, so taking it would fail rather than
			// pass quietly.
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

			created, err := workflowsFor(st, tmux).NewWorkflow(t.Context(), workflow.Config{Name: "backend", Cwd: tt.workflowCwd})
			require.NoError(t, err)

			_, err = manager.NewAgent(t.Context(), NewAgentParams{
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

// TestNewAgentInsideWorkflow covers what an agent inherits from the bucket it is
// created in. The context is a snapshot taken at launch, so a later edit reaches
// only agents spawned after it.
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

			created, err := workflowsFor(st, tmux).NewWorkflow(t.Context(), workflow.Config{
				Name:    "backend",
				Context: tt.context,
			})
			require.NoError(t, err)

			params := NewAgentParams{Title: "agent", Dir: t.TempDir(), WorkflowID: created.ID}

			_, err = manager.NewAgent(t.Context(), params)
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
		name string
		// action is the workflow's setup action, and answered the directory it names.
		action    string
		answered  string
		setupErr  error
		wantDir   string
		wantSetup []string
		wantErr   error
	}{
		{
			name:      "setup that fails abandons the create",
			action:    "make-worktree",
			setupErr:  ErrSetupFailed,
			wantSetup: []string{"make-worktree"},
			wantErr:   ErrSetupFailed,
		},
		{
			name:      "a directory setup names that is not usable abandons the create",
			action:    "make-worktree",
			answered:  filepath.Join(t.TempDir(), "never-created"),
			wantSetup: []string{"make-worktree"},
			wantErr:   ErrSetupFailed,
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
			lifecycle := &fakeLifecycle{dir: tt.answered, setupErr: tt.setupErr}
			h := &fakeHarness{}
			manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: h}, lifecycle)

			created, err := workflowsFor(st, tmux).NewWorkflow(t.Context(), workflow.Config{
				Name:        "backend",
				SetupAction: tt.action,
			})
			require.NoError(t, err)

			asked := t.TempDir()
			_, err = manager.NewAgent(t.Context(), NewAgentParams{
				Title:      "agent",
				Dir:        asked,
				WorkflowID: created.ID,
			})

			assert.Equal(t, tt.wantSetup, lifecycle.setup, "the workflow's own choice reaches the runner")

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				stored, getErr := st.Get(t.Context(), "agent")
				require.NoError(t, getErr)
				assert.Nil(t, stored, "a refused setup leaves no row behind")
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

func TestNewAgentSetupActionOverride(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	lifecycle := &fakeLifecycle{}
	h := &fakeHarness{}
	manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: h}, lifecycle)

	created, err := workflowsFor(st, tmux).NewWorkflow(t.Context(), workflow.Config{
		Name:        "backend",
		SetupAction: "make-worktree",
	})
	require.NoError(t, err)

	_, err = manager.NewAgent(t.Context(), NewAgentParams{
		Title:       "agent",
		Dir:         t.TempDir(),
		WorkflowID:  created.ID,
		SetupAction: xptr.Ptr("install-deps"),
	})
	require.NoError(t, err)

	assert.Equal(t, []string{"install-deps"}, lifecycle.setup, "the agent's own choice runs instead of the workflow's")

	stored, err := st.Get(t.Context(), "agent")
	require.NoError(t, err)
	require.NotNil(t, stored)
	require.NotNil(t, stored.SetupAction)
	assert.Equal(t, "install-deps", *stored.SetupAction, "the override is what gets persisted, not the workflow's action")
}

func TestNewAgentSetupActionOverrideToNone(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	lifecycle := &fakeLifecycle{}
	h := &fakeHarness{}
	manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: h}, lifecycle)

	created, err := workflowsFor(st, tmux).NewWorkflow(t.Context(), workflow.Config{
		Name:        "backend",
		SetupAction: "make-worktree",
	})
	require.NoError(t, err)

	_, err = manager.NewAgent(t.Context(), NewAgentParams{
		Title:       "agent",
		Dir:         t.TempDir(),
		WorkflowID:  created.ID,
		SetupAction: xptr.Ptr(""),
	})
	require.NoError(t, err)

	assert.Empty(t, lifecycle.setup, "an explicit none refuses the workflow's action too")

	stored, err := st.Get(t.Context(), "agent")
	require.NoError(t, err)
	require.NotNil(t, stored)
	require.NotNil(t, stored.SetupAction)
	assert.Empty(t, *stored.SetupAction)
}

// TestNewAgentCeiling covers the ceiling, which has to refuse before anything is opened or
// a create refused after its workflow built a worktree leaves one behind every time.
func TestNewAgentCeiling(t *testing.T) {
	tests := []struct {
		name string
		// maxAgents is the configured ceiling, zero for a build that wires none.
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

			workflow, err := workflowsFor(st, tmux).NewWorkflow(t.Context(), workflow.Config{
				Name:        "backend",
				SetupAction: "make-worktree",
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

			created, err := manager.NewAgent(t.Context(), NewAgentParams{
				Title:      "agent",
				Dir:        t.TempDir(),
				WorkflowID: workflow.ID,
			})

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Contains(t, err.Error(), "please archive one to continue", "the refusal says what to do about it")
				assert.Empty(t, lifecycle.setup, "a refused create runs no setup action")
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

	first, err := manager.NewAgent(t.Context(), params)
	require.NoError(t, err)
	require.NotNil(t, first)

	// The ceiling of one is full, and this is the retry of the launch that filled it.
	second, err := manager.NewAgent(t.Context(), params)
	require.NoError(t, err)
	require.NotNil(t, second)
	assert.Equal(t, first.ID, second.ID, "the retry finds the agent it already created")

	// A create claiming no key is a second agent, which there is no room for.
	params.IdempotencyKey = ""
	_, err = manager.NewAgent(t.Context(), params)
	require.ErrorIs(t, err, ErrTooManyAgents)
}

// TestNewAgentCleansUpAfterASetupThatRan covers a launch that refuses after its setup
// action has already built something. A worktree no row names is never reached again, so
// every refusal past the point the script ran has to undo it.
func TestNewAgentCleansUpAfterASetupThatRan(t *testing.T) {
	tests := []struct {
		name string
		// answered is the directory setup names, and each error below is a way the launch
		// can refuse once the script has already run.
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

			created, err := workflowsFor(st, tmux).NewWorkflow(t.Context(), workflow.Config{
				Name:          "backend",
				SetupAction:   "make-worktree",
				CleanupAction: "drop-worktree",
			})
			require.NoError(t, err)

			_, err = manager.NewAgent(t.Context(), NewAgentParams{
				Title:      "agent",
				Dir:        t.TempDir(),
				WorkflowID: created.ID,
			})

			require.Error(t, err)
			assert.Equal(t, []string{"make-worktree"}, lifecycle.setup)
			assert.Equal(t, []string{"drop-worktree"}, lifecycle.cleanup, "what setup built is taken back")

			if tt.windowErr != nil || tt.setOwnerErr != nil || tt.sendKeysErr != nil {
				require.Len(t, lifecycle.cleanupCwd, 1)
				assert.Equal(t, tt.answered, lifecycle.cleanupCwd[0], "cleanup runs against what setup actually built, not the workflow's planned dir")
			}

			agents, listErr := st.List(t.Context())
			require.NoError(t, listErr)
			assert.Empty(t, agents, "and no row is left behind")
		})
	}
}

// TestNewAgentDoesNotCleanUpASetupThatRefused covers the other side of it. A setup action
// that refused is the script saying it did not get as far as doing anything, so firing
// cleanup would be asking it to undo work nobody did.
func TestNewAgentDoesNotCleanUpASetupThatRefused(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	lifecycle := &fakeLifecycle{setupErr: ErrSetupFailed}
	manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: &fakeHarness{}}, lifecycle)

	created, err := workflowsFor(st, tmux).NewWorkflow(t.Context(), workflow.Config{
		Name:          "backend",
		SetupAction:   "make-worktree",
		CleanupAction: "drop-worktree",
	})
	require.NoError(t, err)

	_, err = manager.NewAgent(t.Context(), NewAgentParams{
		Title:      "agent",
		Dir:        t.TempDir(),
		WorkflowID: created.ID,
	})

	require.ErrorIs(t, err, ErrSetupFailed)
	assert.Empty(t, lifecycle.cleanup)
}

// TestDeleteAgentRunsWorkflowCleanup covers cleanup firing on the irreversible half only.
func TestDeleteAgentRunsWorkflowCleanup(t *testing.T) {
	tests := []struct {
		name        string
		archive     bool
		wantCleanup []string
	}{
		{
			name:        "deleting an agent releases what setup built",
			wantCleanup: []string{"drop-worktree"},
		},
		{
			name:    "archiving one keeps it",
			archive: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := newFakeTmux()
			st := newStore(t)
			lifecycle := &fakeLifecycle{}
			manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: &fakeHarness{}}, lifecycle)

			created, err := workflowsFor(st, tmux).NewWorkflow(t.Context(), workflow.Config{
				Name:          "backend",
				CleanupAction: "drop-worktree",
			})
			require.NoError(t, err)

			agent, err := manager.NewAgent(t.Context(), NewAgentParams{
				Title:      "agent",
				Dir:        t.TempDir(),
				WorkflowID: created.ID,
			})
			require.NoError(t, err)

			if tt.archive {
				require.NoError(t, manager.ArchiveAgents(t.Context(), []string{agent.ID}))
			} else {
				require.NoError(t, manager.DeleteAgents(t.Context(), []string{agent.ID}))
			}

			assert.Equal(t, tt.wantCleanup, lifecycle.cleanup)
		})
	}
}

// TestDeleteAgentRunsCleanupFromAnArchivedWorkflow covers deleting an agent out of the
// archive, where the worktree setup built is still on disk and the cleanup is the only
// thing that takes it back.
func TestDeleteAgentRunsCleanupFromAnArchivedWorkflow(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	lifecycle := &fakeLifecycle{}
	manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: &fakeHarness{}}, lifecycle)
	workflows := workflowsFor(st, tmux)

	created, err := workflows.NewWorkflow(t.Context(), workflow.Config{
		Name:          "backend",
		CleanupAction: "drop-worktree",
	})
	require.NoError(t, err)

	agent, err := manager.NewAgent(t.Context(), NewAgentParams{
		Title:      "agent",
		Dir:        t.TempDir(),
		WorkflowID: created.ID,
	})
	require.NoError(t, err)

	require.NoError(t, workflows.ArchiveWorkflow(t.Context(), created.ID))
	require.NoError(t, manager.DeleteAgents(t.Context(), []string{agent.ID}))

	assert.Equal(t, []string{"drop-worktree"}, lifecycle.cleanup)
}

// TestRunCleanupLogsOneThatWouldNotStart covers the cleanup that never becomes a process at
// all, which is what a renamed script leaves behind.
func TestRunCleanupLogsOneThatWouldNotStart(t *testing.T) {
	lifecycle := &fakeLifecycle{cleanupErr: actions.ErrActionNotFound}
	manager := NewAgentService(NewAgentServiceParams{Lifecycle: lifecycle})
	ctx, records := loggedContext(t)

	manager.runCleanup(ctx, agentstore.Agent{ID: "a1", Cwd: "/repo/.worktrees/auth"}, "was-renamed")

	got := recordFor(t, records(), "the cleanup action would not start")
	assert.Equal(t, "ERROR", got["severity_text"])
	assert.Equal(t, "was-renamed", got["weave.action.name"])
	assert.Equal(t, "/repo/.worktrees/auth", got["weave.action.agent.cwd"])
}

// TestCleanupActionLogsAWorkflowThatWouldNotRead covers the read that breaks rather than
// answering, which skips a teardown as quietly as a workflow naming none.
func TestCleanupActionLogsAWorkflowThatWouldNotRead(t *testing.T) {
	manager := NewAgentService(NewAgentServiceParams{Workflows: brokenWorkflowReader{err: errors.New("connection refused")}})
	ctx, records := loggedContext(t)

	agent := agentstore.Agent{ID: "a1", Cwd: "/repo/.worktrees/auth", WorkflowID: "wf1"}
	assert.Empty(t, manager.getCleanupAction(ctx, agent))

	got := recordFor(t, records(), "the workflow behind an agent could not be read")
	assert.Equal(t, "ERROR", got["severity_text"])
	assert.Equal(t, "wf1", got["weave.workflow.id"])
	assert.Equal(t, "a1", got["weave.action.agent.id"])
	assert.Equal(t, "/repo/.worktrees/auth", got["weave.action.agent.cwd"])
}

// TestCleanupActionSaysNothingAboutAWorkflowNamingNone covers the quiet half, since a
// workflow with no cleanup is not a fault and a line about one would be noise on every
// delete.
func TestCleanupActionSaysNothingAboutAWorkflowNamingNone(t *testing.T) {
	manager := NewAgentService(NewAgentServiceParams{Workflows: fakeWorkflowReader{}})
	ctx, records := loggedContext(t)

	assert.Empty(t, manager.getCleanupAction(ctx, agentstore.Agent{ID: "a1", WorkflowID: "wf1"}))
	assert.Empty(t, records())
}

func TestCleanupActionOverridesTheWorkflow(t *testing.T) {
	manager := NewAgentService(NewAgentServiceParams{
		Workflows: fakeWorkflowReader{workflow: &workflow.Workflow{ID: "wf1", CleanupAction: "drop-worktree"}},
	})
	got := manager.getCleanupAction(context.Background(), agentstore.Agent{ID: "a1", WorkflowID: "wf1", CleanupAction: xptr.Ptr("custom-cleanup")})
	assert.Equal(t, "custom-cleanup", got)
}

func TestCleanupActionOverrideToNoneWinsOverTheWorkflow(t *testing.T) {
	manager := NewAgentService(NewAgentServiceParams{
		Workflows: fakeWorkflowReader{workflow: &workflow.Workflow{ID: "wf1", CleanupAction: "drop-worktree"}},
	})
	got := manager.getCleanupAction(context.Background(), agentstore.Agent{ID: "a1", WorkflowID: "wf1", CleanupAction: xptr.Ptr("")})
	assert.Empty(t, got)
}

func TestSetupActionOverridesTheWorkflow(t *testing.T) {
	manager := NewAgentService(NewAgentServiceParams{
		Workflows: fakeWorkflowReader{workflow: &workflow.Workflow{ID: "wf1", SetupAction: "make-worktree"}},
	})
	got := manager.getSetupAction(context.Background(), agentstore.Agent{ID: "a1", WorkflowID: "wf1", SetupAction: xptr.Ptr("custom-setup")})
	assert.Equal(t, "custom-setup", got)
}

func TestSetupActionOverrideToNoneWinsOverTheWorkflow(t *testing.T) {
	manager := NewAgentService(NewAgentServiceParams{
		Workflows: fakeWorkflowReader{workflow: &workflow.Workflow{ID: "wf1", SetupAction: "make-worktree"}},
	})
	got := manager.getSetupAction(context.Background(), agentstore.Agent{ID: "a1", WorkflowID: "wf1", SetupAction: xptr.Ptr("")})
	assert.Empty(t, got)
}

// brokenWorkflowReader fails every read, which is what a store whose database has gone
// away does.
type brokenWorkflowReader struct {
	err error
}

func (b brokenWorkflowReader) Get(ctx context.Context, id string) (*workflow.Workflow, error) {
	return b.GetIncludingArchived(ctx, id)
}

func (b brokenWorkflowReader) GetIncludingArchived(context.Context, string) (*workflow.Workflow, error) {
	return nil, b.err
}

// TestUniqueAgentName covers the name an agent ends up with. The store's unique
// index is the authority, so a taken name has to be stepped past rather than
// reused, which would clobber the agent holding it. The index is scoped to the
// workflow, so a name is only taken when it is taken in the same one.
func TestUniqueAgentName(t *testing.T) {
	tests := []struct {
		name   string
		title  string
		prompt string
		// taken are agents seeded into the workflow the name is generated for, and
		// takenElsewhere the same into a workflow of their own.
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

			assert.Equal(t, tt.wantName, manager.uniqueAgentName(t.Context(), workflowID, tt.title, tt.prompt))
		})
	}
}

func TestNameTaken(t *testing.T) {
	tests := []struct {
		name  string
		check string
		// elsewhere asks about the other workflow, where the same name is free.
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

			assert.Equal(t, tt.want, manager.isNameTaken(t.Context(), scope, tt.check))
		})
	}
}

// TestArchiveAgent covers hiding an agent. The record and its transcript stay,
// so search still reaches it, but the agent is stopped.
func TestArchiveAgent(t *testing.T) {
	tests := []struct {
		name string
		seed string
		// missing aims the call at an id nobody holds rather than at the seeded agent.
		missing   bool
		wantErrIs error
	}{
		{
			name:      "an id nobody holds is not a quiet success",
			missing:   true,
			wantErrIs: ErrAgentNotFound,
		},
		{
			name: "a live agent is stopped and hidden",
			seed: "target",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			tmux := newFakeTmux()
			manager := newManager(tmux, st, "")

			target := "00000000-0000-0000-0000-000000000000"
			if tt.seed != "" {
				target = seedRunning(t, st, tmux, tt.seed, "%1", agentstore.StateThinking).ID
			}
			if tt.missing {
				target = "00000000-0000-0000-0000-000000000000"
			}

			err := manager.ArchiveAgents(t.Context(), []string{target})
			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)
				return
			}

			require.NoError(t, err)
			assert.Contains(t, tmux.killedPanes, "%1", "the agent's pane is killed")

			stored, err := st.Get(t.Context(), tt.seed)
			require.NoError(t, err)
			assert.Nil(t, stored, "the agent is hidden from listings")
		})
	}
}

// TestDeleteAgent covers destroying an agent for good, which is what emptying
// the archive does. It is keyed by id rather than by name because the agents it
// runs against are archived, and an archived name is not unique: the index that
// makes a name identify one agent holds among live rows only.
func TestDeleteAgent(t *testing.T) {
	tests := []struct {
		name    string
		archive bool
		target  func(seeded agentstore.Agent) string
		// wantPaneKilled is false for an archived agent, which gave up its pane at
		// archive time and has none left to kill.
		wantPaneKilled bool
		wantErrIs      error
	}{
		{
			name:      "an id nobody holds is not a quiet success",
			target:    func(agentstore.Agent) string { return "019fa434-e7a7-7ac9-bab5-718681da797d" },
			wantErrIs: ErrAgentNotFound,
		},
		{
			name:           "a live agent is stopped and destroyed",
			target:         func(seeded agentstore.Agent) string { return seeded.ID },
			wantPaneKilled: true,
		},
		{
			// The panel's archive key is what most agents go out through, so everything
			// reaching this path has been archived already. A delete that could not see
			// those rows would report every agent in the archive as missing.
			name:    "an archived agent is destroyed too",
			archive: true,
			target:  func(seeded agentstore.Agent) string { return seeded.ID },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			tmux := newFakeTmux()
			manager := newManager(tmux, st, "")
			ctx := t.Context()

			seeded := seedRunning(t, st, tmux, "target", "%1", agentstore.StateThinking)
			require.NoError(t, st.Transcripts.Upsert(ctx, transcriptstore.UpsertParams{
				AgentID: seeded.ID, Content: "something it said", Offset: 1,
			}))
			if tt.archive {
				require.NoError(t, st.SoftDelete(ctx, seeded.Name))
			}

			err := manager.DeleteAgents(ctx, []string{tt.target(seeded)})
			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)

				all, listErr := st.ListAll(ctx)
				require.NoError(t, listErr)
				assert.Len(t, all, 1, "a delete that matched nothing removes nothing")

				return
			}

			require.NoError(t, err)
			if tt.wantPaneKilled {
				assert.Contains(t, tmux.killedPanes, "%1", "the agent's pane is killed")
			}

			all, err := st.ListAll(ctx)
			require.NoError(t, err)
			assert.Empty(t, all, "the row is gone from every listing, not hidden from one")

			tr, err := st.GetTranscript(ctx, seeded.ID)
			require.NoError(t, err)
			assert.Empty(t, tr.Content, "the transcript goes with the agent")
		})
	}
}

// TestAReissuedPaneIsNotTheAgents covers the whole point of stamping a pane. tmux
// only promises a pane id is unique while the server that issued it lives, so a
// dormant agent's recorded pane routinely names a pane the next server gave to a
// different agent. Every read of a stored pane id has to survive that: the agent
// must not read as running, and nothing may reach into the pane that is now
// somebody else's.
func TestAReissuedPaneIsNotTheAgents(t *testing.T) {
	// The two agents share a pane id the way rows in the panel's database do after a
	// restart: the live one holds it because it is running there, and the dormant one
	// because that is what its row still says.
	const shared = "%4"

	setup := func(t *testing.T) (*AgentService, *testStore, *fakeTmux, agentstore.Agent) {
		t.Helper()
		st := newStore(t)
		tmux := newFakeTmux()
		dormant := seed(t, st, "dormant", shared, agentstore.StateIdle)
		seedRunning(t, st, tmux, "running", shared, agentstore.StateThinking)

		return newManager(tmux, st, ""), st, tmux, dormant
	}

	t.Run("the dormant agent does not read as running", func(t *testing.T) {
		manager, _, _, dormant := setup(t)
		assert.False(t, manager.IsAgentRunning(dormant))
	})

	t.Run("archiving it leaves the pane running", func(t *testing.T) {
		manager, _, tmux, dormant := setup(t)
		require.NoError(t, manager.ArchiveAgents(t.Context(), []string{dormant.ID}))
		assert.Empty(t, tmux.killedPanes, "killing by a stale id would stop the agent using that pane now")
	})

	t.Run("deleting it leaves the pane running", func(t *testing.T) {
		manager, _, tmux, dormant := setup(t)
		require.NoError(t, manager.DeleteAgents(t.Context(), []string{dormant.ID}))
		assert.Empty(t, tmux.killedPanes, "killing by a stale id would stop the agent using that pane now")
	})

	t.Run("resuming it opens a window rather than adopting the pane", func(t *testing.T) {
		manager, st, tmux, dormant := setup(t)
		resumed, err := manager.ResumeAgent(t.Context(), dormant.ID)
		require.NoError(t, err)
		assert.NotEqual(t, shared, resumed.Pane, "a resume that read the pane as its own would relaunch nothing")
		assert.Equal(t, dormant.ID, tmux.owners[resumed.Pane], "and the new pane is its own")

		still := agentByID(t, st, dormant.ID)
		assert.Equal(t, resumed.Pane, still.Pane)
	})

	t.Run("a reconcile pass forgets the claim without reading the pane", func(t *testing.T) {
		_, st, tmux, dormant := setup(t)
		tmux.captures[shared] = busyFrame

		rec := NewReconciler(tmux, fakeHarnesses{harness: &fakeHarness{}}, st.Agents)
		require.NoError(t, rec.Reconcile(t.Context()))

		after := agentByID(t, st, dormant.ID)
		assert.Empty(t, after.Pane, "the stale claim is dropped, so it cannot collide again")
		assert.Equal(t, agentstore.StateIdle, after.State, "the frame belongs to another agent and says nothing about this one")
		assert.Equal(t, agentstore.StateThinking, stateOf(t, st, "running"), "the agent actually in that pane is read normally")
	})
}

// TestRestore covers bringing an archived agent back. It returns as a row and nothing
// else, and a name taken while it sat hidden is refused rather than worked around, since
// which of the two agents keeps the name is the user's to decide.
func TestRestore(t *testing.T) {
	tests := []struct {
		name      string
		reuseName bool
		wantErrIs error
	}{
		{
			name:      "a name taken since is refused",
			reuseName: true,
			wantErrIs: store.ErrNameTaken,
		},
		{
			name: "a free name is kept",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			tmux := newFakeTmux()
			manager := newManager(tmux, st, "")
			ctx := t.Context()

			workflowID := seedWorkflow(t, st)
			require.NoError(t, st.Create(ctx, agentstore.Agent{
				Name:       "foo",
				State:      agentstore.StateIdle,
				SessionID:  "sess-old",
				WorkflowID: workflowID,
			}))
			original, err := st.Agents.GetInWorkflow(ctx, workflowID, "foo")
			require.NoError(t, err)
			require.NoError(t, st.SoftDelete(ctx, "foo"))

			// The namesake goes in the same workflow, since that is the scope the
			// unique index covers.
			if tt.reuseName {
				require.NoError(t, st.Create(ctx, agentstore.Agent{
					Name:       "foo",
					State:      agentstore.StateIdle,
					WorkflowID: workflowID,
				}))
			}

			_, err = manager.RestoreAgents(ctx, []string{original.ID})

			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)
				assert.ErrorContains(t, err, "rename whatever holds", "the refusal says what to do about it")

				refused := agentByID(t, st, original.ID)
				assert.NotNil(t, refused.ArchivedAt, "the agent stays in the archive")
				assert.Equal(t, "foo", refused.Name, "under the name it went in with")

				return
			}

			require.NoError(t, err)

			restored := agentByID(t, st, original.ID)
			assert.Nil(t, restored.ArchivedAt, "the agent is back in the listing")
			assert.Equal(t, "foo", restored.Name)
			assert.Equal(t, agentstore.StateIdle, restored.State)
			assert.Equal(t, "sess-old", restored.SessionID, "the conversation is preserved")
			assert.Empty(t, tmux.owners, "a restore starts nothing, so no pane was opened")
			assert.False(t, manager.IsAgentRunning(restored), "it comes back dormant, waiting to be resumed")

			resumed, err := manager.ResumeAgent(ctx, original.ID)
			require.NoError(t, err)
			assert.Equal(t, original.ID, tmux.owners[resumed.Pane], "resuming stamps the pane it came back in")
			assert.True(t, manager.IsAgentRunning(*resumed), "so it reads as running again")
		})
	}
}

// TestRestoreCeiling covers the ceiling on the way out of the archive. An unarchived
// agent holds a slot whether or not anything is running in it, so a restore has to be
// refused where a create would be, or the archive is a way around the limit.
func TestRestoreCeiling(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	manager := st.agentServiceCapped(tmux, &fakeLifecycle{}, 2)
	ctx := t.Context()

	archived := seed(t, st, "archived", "%0", agentstore.StateIdle)
	require.NoError(t, st.SoftDelete(ctx, "archived"))

	seed(t, st, "live-0", "%1", agentstore.StateThinking)
	liveTwo := seed(t, st, "live-1", "%2", agentstore.StateThinking)

	_, err := manager.RestoreAgents(ctx, []string{archived.ID})

	require.ErrorIs(t, err, ErrTooManyAgents)
	still := agentByID(t, st, archived.ID)
	assert.NotNil(t, still.ArchivedAt, "the refused restore leaves it in the archive")

	require.NoError(t, manager.ArchiveAgents(ctx, []string{liveTwo.ID}))
	_, err = manager.RestoreAgents(ctx, []string{archived.ID})
	require.NoError(t, err, "archiving one makes room again")
}

// TestResumeRefusesArchived covers resuming an agent that is still in the archive. The
// pane would sit behind a row nothing lists, so it is refused and names the way out.
// TestResumeRunsTheSetupAction covers the workflow's setup action firing again on the way
// back up. Archive leaves whatever setup built alone and a restore only takes the row out of
// the archive, so resume is the moment a workspace has to exist again, and a script that
// makes one is reached with the agent's recorded directory to work from.
func TestResumeRunsTheSetupAction(t *testing.T) {
	tests := []struct {
		name string
		// named is the directory the action prints, empty for one that prints none.
		named      string
		resumeErr  error
		wantWindow bool
		wantErr    string
	}{
		{
			name:      "an action that refuses keeps the agent dormant",
			resumeErr: errors.New("fatal: 'auth' is already checked out"),
			wantErr:   "already checked out",
		},
		{
			name:    "an action naming somewhere that is not a directory is refused",
			named:   "/definitely/not/here",
			wantErr: "a setup action named",
		},
		{
			name:       "an action naming nowhere leaves the recorded directory alone",
			wantWindow: true,
		},
		{
			name:       "an action naming a directory brings the agent back up in it",
			named:      "named",
			wantWindow: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			tmux := newFakeTmux()
			ctx := t.Context()

			recorded := t.TempDir()
			named := tt.named
			if named == "named" {
				named = t.TempDir()
			}

			lifecycle := &fakeLifecycle{resumeDir: named, resumeErr: tt.resumeErr}
			manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: &fakeHarness{}}, lifecycle)

			created, err := st.Workflows.Create(ctx, workflow.Config{
				Name:        "resume-" + tt.name,
				SetupAction: "make-worktree",
			})
			require.NoError(t, err)

			agent := seedInWorkflow(t, st, created.ID, "auth", "%0", agentstore.StateIdle)
			agent.Cwd = recorded
			require.NoError(t, st.Agents.Update(ctx, agent))

			resumed, err := manager.ResumeAgent(ctx, agent.ID)

			assert.Equal(t, []string{"make-worktree"}, lifecycle.resumed, "the workflow's own action is what fired")

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.wantErr)
				assert.Empty(t, tmux.newWindows, "a refused action opens no window")
				assert.Equal(t, recorded, agentByID(t, st, agent.ID).Cwd, "and moves nobody")

				return
			}

			require.NoError(t, err)
			require.True(t, tt.wantWindow)

			want := recorded
			if named != "" {
				want = named
			}

			assert.Equal(t, want, resumed.Cwd)
			assert.Equal(t, want, tmux.newWindows["auth"], "the window opens where the action said")
		})
	}
}

// TestResumeWithoutASetupActionFiresNothing covers the ordinary workflow, which names no
// setup action and comes back up the way it always has.
func TestResumeWithoutASetupActionFiresNothing(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	lifecycle := &fakeLifecycle{}
	manager := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: &fakeHarness{}}, lifecycle)

	agent := seed(t, st, "auth", "%0", agentstore.StateIdle)

	_, err := manager.ResumeAgent(t.Context(), agent.ID)

	require.NoError(t, err)
	assert.Empty(t, lifecycle.resumed)
}

func TestResumeRefusesArchived(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	manager := newManager(tmux, st, "")
	ctx := t.Context()

	archived := seed(t, st, "archived", "%0", agentstore.StateIdle)
	require.NoError(t, st.SoftDelete(ctx, "archived"))

	_, err := manager.ResumeAgent(ctx, archived.ID)

	require.ErrorIs(t, err, ErrAgentArchived)
	assert.ErrorContains(t, err, "restore it first", "the refusal says what to do about it")
	assert.Empty(t, tmux.newWindows, "and opens no window")
}
