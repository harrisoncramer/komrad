package agents

import (
	"os"
	"path/filepath"
	"testing"

	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	schedulestore "weave/internal/store/schedule"
	workflowstore "weave/internal/store/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// assertExecutableFileExists checks a generated action landed on disk and is runnable.
func assertExecutableFileExists(t *testing.T, path string) {
	t.Helper()

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode().Perm()&0o111)
}

func TestNewWorkflow(t *testing.T) {
	tests := []struct {
		name     string
		cfg      workflowstore.Config
		wantName string
	}{
		{
			name:     "a blank name falls back to a label, so the list never renders an empty row",
			cfg:      workflowstore.Config{Name: "   "},
			wantName: "workflow",
		},
		{
			name:     "a given name is kept",
			cfg:      workflowstore.Config{Name: "backend"},
			wantName: "backend",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			created, err := workflowsFor(newStore(t), newFakeTmux()).NewWorkflow(t.Context(), tt.cfg)
			require.NoError(t, err)
			require.NotNil(t, created)
			assert.Equal(t, tt.wantName, created.Name)
		})
	}
}

func TestUpdateWorkflow(t *testing.T) {
	tests := []struct {
		name      string
		create    bool
		cfg       workflowstore.Config
		wantErrIs error
		wantName  string
	}{
		{
			name:      "an id nobody holds",
			cfg:       workflowstore.Config{Name: "ghost"},
			wantErrIs: ErrWorkflowNotFound,
		},
		{
			name:     "a blank name keeps the one it had",
			create:   true,
			cfg:      workflowstore.Config{Name: "   "},
			wantName: "backend",
		},
		{
			name:     "a new name is written",
			create:   true,
			cfg:      workflowstore.Config{Name: "platform"},
			wantName: "platform",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			workflows := workflowsFor(st, newFakeTmux())

			id := "00000000-0000-0000-0000-000000000000"
			if tt.create {
				created, err := workflows.NewWorkflow(t.Context(), workflowstore.Config{Name: "backend"})
				require.NoError(t, err)
				id = created.ID
			}

			err := workflows.UpdateWorkflow(t.Context(), id, tt.cfg)
			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)
				return
			}

			require.NoError(t, err)

			stored, err := st.GetWorkflow(t.Context(), id)
			require.NoError(t, err)
			require.NotNil(t, stored)
			assert.Equal(t, tt.wantName, stored.Name)
		})
	}
}

// TestNewWorkflowWithWorktree covers the validation a worktree-enabled create is held to,
// and that enabling it writes the two generated scripts and points setup and cleanup at
// them, both refused before anything asks the store to insert a mock of a workflow that
// half-generated.
func TestNewWorkflowWithWorktree(t *testing.T) {
	tests := []struct {
		name      string
		cfg       workflowstore.Config
		wantErrIs error
	}{
		{
			name:      "enabling it with no cwd is refused, since there is no repo to work against",
			cfg:       workflowstore.Config{Name: "backend", Worktree: workflowstore.WorktreeConfig{Enabled: true}},
			wantErrIs: ErrWorktreeNeedsCwd,
		},
		{
			name: "enabling it alongside a setup action is refused, since weave names its own",
			cfg: workflowstore.Config{
				Name:        "backend",
				Cwd:         "/repo",
				SetupAction: "hand-written",
				Worktree:    workflowstore.WorktreeConfig{Enabled: true},
			},
			wantErrIs: ErrWorktreeOwnsActions,
		},
		{
			name: "enabling it alongside a cleanup action is refused too",
			cfg: workflowstore.Config{
				Name:          "backend",
				Cwd:           "/repo",
				CleanupAction: "hand-written",
				Worktree:      workflowstore.WorktreeConfig{Enabled: true},
			},
			wantErrIs: ErrWorktreeOwnsActions,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)

			_, err := workflowsFor(st, newFakeTmux()).NewWorkflow(t.Context(), tt.cfg)
			require.ErrorIs(t, err, tt.wantErrIs)

			entries, readErr := os.ReadDir(st.actionsDir)
			require.NoError(t, readErr)
			assert.Empty(t, entries, "a refused create writes nothing into the actions directory")
		})
	}
}

// TestNewWorkflowGeneratesWorktreeScripts covers the create that succeeds: the two scripts
// land in the actions directory named after the new workflow's own id, and setup_action and
// cleanup_action are pointed at them.
func TestNewWorkflowGeneratesWorktreeScripts(t *testing.T) {
	st := newStore(t)

	created, err := workflowsFor(st, newFakeTmux()).NewWorkflow(t.Context(), workflowstore.Config{
		Name: "backend",
		Cwd:  "/repo",
		Worktree: workflowstore.WorktreeConfig{
			Enabled:   true,
			CopyPaths: []string{".env"},
		},
	})
	require.NoError(t, err)

	wantSetup := worktreeSetupName(created.ID)
	wantCleanup := worktreeCleanupName(created.ID)
	assert.Equal(t, wantSetup, created.SetupAction)
	assert.Equal(t, wantCleanup, created.CleanupAction)

	assertExecutableFileExists(t, filepath.Join(st.actionsDir, wantSetup))
	assertExecutableFileExists(t, filepath.Join(st.actionsDir, wantCleanup))

	setupScript, err := os.ReadFile(filepath.Join(st.actionsDir, wantSetup))
	require.NoError(t, err)
	assert.Contains(t, string(setupScript), "/repo")
	assert.Contains(t, string(setupScript), ".env")
}

// TestUpdateWorkflowWorktree covers turning worktree support on through an update, and
// turning it back off: off has to clear setup_action and cleanup_action when they still
// name the generated scripts, and leave them alone when a caller has since pointed them at
// something else.
func TestUpdateWorkflowWorktree(t *testing.T) {
	st := newStore(t)
	workflows := workflowsFor(st, newFakeTmux())

	created, err := workflows.NewWorkflow(t.Context(), workflowstore.Config{Name: "backend", Cwd: "/repo"})
	require.NoError(t, err)

	err = workflows.UpdateWorkflow(t.Context(), created.ID, workflowstore.Config{
		Name:     "backend",
		Cwd:      "/repo",
		Worktree: workflowstore.WorktreeConfig{Enabled: true},
	})
	require.NoError(t, err)

	afterEnable, err := st.GetWorkflow(t.Context(), created.ID)
	require.NoError(t, err)
	wantSetup := worktreeSetupName(created.ID)
	wantCleanup := worktreeCleanupName(created.ID)
	assert.Equal(t, wantSetup, afterEnable.SetupAction)
	assert.Equal(t, wantCleanup, afterEnable.CleanupAction)

	err = workflows.UpdateWorkflow(t.Context(), created.ID, workflowstore.Config{Name: "backend", Cwd: "/repo"})
	require.NoError(t, err)

	afterDisable, err := st.GetWorkflow(t.Context(), created.ID)
	require.NoError(t, err)
	assert.Empty(t, afterDisable.SetupAction, "disabling clears the generated setup action")
	assert.Empty(t, afterDisable.CleanupAction, "disabling clears the generated cleanup action")
}

// TestUpdateWorkflowWorktreeDisableKeepsAHandWrittenOverride covers a caller who pointed
// setup_action or cleanup_action somewhere else after enabling worktree support: turning it
// off must not clobber that choice.
func TestUpdateWorkflowWorktreeDisableKeepsAHandWrittenOverride(t *testing.T) {
	st := newStore(t)
	workflows := workflowsFor(st, newFakeTmux())

	created, err := workflows.NewWorkflow(t.Context(), workflowstore.Config{
		Name:     "backend",
		Cwd:      "/repo",
		Worktree: workflowstore.WorktreeConfig{Enabled: true},
	})
	require.NoError(t, err)

	err = workflows.UpdateWorkflow(t.Context(), created.ID, workflowstore.Config{
		Name:        "backend",
		Cwd:         "/repo",
		SetupAction: "hand-written",
	})
	require.NoError(t, err)

	got, err := st.GetWorkflow(t.Context(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, "hand-written", got.SetupAction)
}

// TestArchiveWorkflow covers what becomes of the agents inside, and what stops
// the archive. Both halves commit together, so an agent is never left pointing at
// a workflow nothing lists, and the agents are stopped on the way rather than left
// running behind rows the panel no longer shows.
func TestArchiveWorkflow(t *testing.T) {
	tests := []struct {
		name   string
		create bool
		// withSchedule leaves a live schedule launching into the workflow, which is
		// what refuses the delete.
		withSchedule bool
		wantErrIs    error
	}{
		{
			name:      "an id nobody holds",
			wantErrIs: ErrWorkflowNotFound,
		},
		{
			name:   "the agents are archived alongside it, still findable in search",
			create: true,
		},
		{
			// Deleting it would leave the schedule with nowhere to run, and switching
			// off recurring work as a side effect of tidying is not something to do
			// quietly.
			name:         "a workflow still scheduled into is refused",
			create:       true,
			withSchedule: true,
			wantErrIs:    ErrWorkflowHasSchedules,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			tmux := newFakeTmux()
			manager := newManager(tmux, st, "")
			workflows := workflowsFor(st, tmux)
			ctx := t.Context()

			id := "00000000-0000-0000-0000-000000000000"
			if tt.create {
				created, err := workflows.NewWorkflow(ctx, workflowstore.Config{Name: "backend"})
				require.NoError(t, err)
				id = created.ID

				_, err = manager.NewAgent(ctx, NewAgentParams{
					Title:      "agent",
					Dir:        t.TempDir(),
					WorkflowID: id,
				})
				require.NoError(t, err)
			}
			if tt.withSchedule {
				st.SeedSchedule(t, schedulestore.Config{
					Name: "nightly", Cron: "0 2 * * *", Prompt: "sweep", Enabled: true, WorkflowID: id,
				})
			}

			err := workflows.ArchiveWorkflow(ctx, id)
			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)

				if tt.create {
					listed, listErr := st.Workflows.List(ctx)
					require.NoError(t, listErr)
					assert.Len(t, listed, 1, "a refused delete leaves the workflow alone")
					assert.Empty(t, tmux.killedPanes, "and leaves the agents inside it running")
				}

				return
			}

			require.NoError(t, err)

			listed, err := st.Workflows.List(ctx)
			require.NoError(t, err)
			assert.Empty(t, listed, "the workflow is hidden")

			agent, err := st.Get(ctx, "agent")
			require.NoError(t, err)
			assert.Nil(t, agent, "the agent is hidden from the live listing")

			all, err := st.ListAll(ctx)
			require.NoError(t, err)
			require.Len(t, all, 1)
			assert.NotNil(t, all[0].ArchivedAt, "and archived rather than removed")
			assert.Equal(t, []string{"%agent"}, tmux.killedPanes, "its CLI is stopped rather than left generating")
			assert.Empty(t, all[0].Pane, "and the pane it no longer owns is forgotten")
		})
	}
}

// TestRestoreWorkflow covers taking a workflow out of the archive. Its agents come back
// with it, as rows rather than as running CLIs, and a name a live agent has taken since
// refuses the whole restore.
func TestRestoreWorkflow(t *testing.T) {
	tests := []struct {
		name string
		// archive says the workflow was archived first, the only state a restore acts on.
		archive bool
		// takeName leaves a live agent holding the archived agent's name, which the
		// unique index would refuse as it comes back.
		takeName  bool
		wantErrIs error
	}{
		{
			name:      "an id nobody holds",
			wantErrIs: ErrWorkflowNotFound,
		},
		{
			name:      "a workflow that is not archived",
			wantErrIs: ErrWorkflowNotFound,
		},
		{
			name:     "the agents inside come back with it",
			archive:  true,
			takeName: false,
		},
		{
			name:      "an agent whose name was taken is refused",
			archive:   true,
			takeName:  true,
			wantErrIs: store.ErrNameTaken,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			tmux := newFakeTmux()
			manager := newManager(tmux, st, "")
			workflows := workflowsFor(st, tmux)
			ctx := t.Context()

			id := "00000000-0000-0000-0000-000000000000"
			if tt.name != "an id nobody holds" {
				created, err := workflows.NewWorkflow(ctx, workflowstore.Config{Name: "backend"})
				require.NoError(t, err)
				id = created.ID

				_, err = manager.NewAgent(ctx, NewAgentParams{
					Title:      "agent",
					Dir:        t.TempDir(),
					WorkflowID: id,
				})
				require.NoError(t, err)
			}
			if tt.archive {
				require.NoError(t, workflows.ArchiveWorkflow(ctx, id))
			}
			// The namesake goes into the archived workflow itself, which is the scope the
			// unique index covers. It is written straight to the store, since a create
			// through the service refuses a workflow nothing lists.
			if tt.takeName {
				require.NoError(t, st.Agents.Create(ctx, agentstore.Agent{
					Name:       "agent",
					State:      agentstore.StateIdle,
					Harness:    testHarnessID,
					WorkflowID: id,
				}))
			}

			opened := len(tmux.newWindows)
			restored, err := workflows.RestoreWorkflow(ctx, id)

			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)

				if tt.takeName {
					listed, listErr := st.Workflows.List(ctx)
					require.NoError(t, listErr)
					assert.NotContains(t, workflowNames(listed), "backend", "the refused restore leaves it archived")

					archived, archivedErr := st.Agents.ListArchivedInWorkflow(ctx, id)
					require.NoError(t, archivedErr)
					assert.Len(t, archived, 1, "with its agent still inside")
				}

				return
			}

			require.NoError(t, err)
			require.NotNil(t, restored)

			listed, err := st.Workflows.List(ctx)
			require.NoError(t, err)
			assert.Contains(t, workflowNames(listed), "backend", "the workflow is back in the listing")

			live, err := st.List(ctx)
			require.NoError(t, err)
			inside := make([]agentstore.Agent, 0, len(live))
			for _, agent := range live {
				if agent.WorkflowID == id {
					inside = append(inside, agent)
				}
			}
			require.Len(t, inside, 1, "the workflow's agent is listed again")
			assert.Len(t, tmux.newWindows, opened, "and nothing was started for it")

			assert.Equal(t, "agent", inside[0].Name)
			// The restore leaves pane_id alone, which only lands a dormant agent because
			// the archive cleared it on the way in.
			assert.Empty(t, inside[0].Pane, "and it comes back owning no pane")
		})
	}
}

// TestRestoreWorkflowCeiling covers a folder that no longer fits. A restore without room
// for every agent inside writes nothing.
func TestRestoreWorkflowCeiling(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	manager := newManager(tmux, st, "")
	workflows := st.workflowServiceCapped(tmux, 3)
	ctx := t.Context()

	created, err := workflows.NewWorkflow(ctx, workflowstore.Config{Name: "backend"})
	require.NoError(t, err)

	for _, title := range []string{"one", "two"} {
		_, err := manager.NewAgent(ctx, NewAgentParams{Title: title, Dir: t.TempDir(), WorkflowID: created.ID})
		require.NoError(t, err)
	}
	require.NoError(t, workflows.ArchiveWorkflow(ctx, created.ID))

	elsewhere, err := workflows.NewWorkflow(ctx, workflowstore.Config{Name: "frontend"})
	require.NoError(t, err)
	for _, title := range []string{"live-one", "live-two"} {
		_, err := manager.NewAgent(ctx, NewAgentParams{Title: title, Dir: t.TempDir(), WorkflowID: elsewhere.ID})
		require.NoError(t, err)
	}

	_, err = workflows.RestoreWorkflow(ctx, created.ID)

	require.ErrorIs(t, err, ErrTooManyAgents)
	listed, err := st.Workflows.List(ctx)
	require.NoError(t, err)
	require.Len(t, listed, 1, "the refused restore leaves the workflow archived")
	assert.Equal(t, "frontend", listed[0].Name)

	archived, err := st.Agents.ListArchivedInWorkflow(ctx, created.ID)
	require.NoError(t, err)
	assert.Len(t, archived, 2, "and both of its agents with it")
}

// workflowNames is the names of a listing, for asserting which workflows a read holds.
func workflowNames(listed []workflowstore.Workflow) []string {
	names := make([]string, 0, len(listed))
	for _, w := range listed {
		names = append(names, w.Name)
	}

	return names
}

// TestRestoreAgentRestoresItsWorkflow covers the other direction. One agent out of an
// archived workflow brings the workflow with it, or it is live under a header the tree
// does not list and gets no row anywhere.
func TestRestoreAgentRestoresItsWorkflow(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	manager := newManager(tmux, st, "")
	workflows := workflowsFor(st, tmux)
	ctx := t.Context()

	created, err := workflows.NewWorkflow(ctx, workflowstore.Config{Name: "backend"})
	require.NoError(t, err)

	agent, err := manager.NewAgent(ctx, NewAgentParams{Title: "one", Dir: t.TempDir(), WorkflowID: created.ID})
	require.NoError(t, err)
	other, err := manager.NewAgent(ctx, NewAgentParams{Title: "two", Dir: t.TempDir(), WorkflowID: created.ID})
	require.NoError(t, err)

	require.NoError(t, workflows.ArchiveWorkflow(ctx, created.ID))

	restored, err := manager.RestoreAgents(ctx, []string{agent.ID})
	require.NoError(t, err)
	require.NotNil(t, restored)

	listed, err := st.Workflows.List(ctx)
	require.NoError(t, err)
	assert.Contains(t, workflowNames(listed), "backend", "the workflow came back with its agent")

	siblings, err := st.Agents.ListArchivedInWorkflow(ctx, created.ID)
	require.NoError(t, err)
	require.Len(t, siblings, 1, "the agents nobody asked for stay archived")
	assert.Equal(t, other.ID, siblings[0].ID)
}

// TestRestoreWorkflowRefusesTwoArchivedNamesakes covers a collision between the returning
// agents themselves. Archived rows sit outside the unique index, so one workflow can hold
// two under a name, and checking live names alone leaves the caller a constraint error.
func TestRestoreWorkflowRefusesTwoArchivedNamesakes(t *testing.T) {
	st := newStore(t)
	workflows := workflowsFor(st, newFakeTmux())
	ctx := t.Context()

	created, err := workflows.NewWorkflow(ctx, workflowstore.Config{Name: "backend"})
	require.NoError(t, err)

	for range 2 {
		require.NoError(t, st.Agents.Create(ctx, agentstore.Agent{
			Name:       "agent",
			State:      agentstore.StateIdle,
			Harness:    testHarnessID,
			WorkflowID: created.ID,
		}))
		require.NoError(t, st.SoftDelete(ctx, "agent"))
	}

	require.NoError(t, workflows.ArchiveWorkflow(ctx, created.ID))

	_, err = workflows.RestoreWorkflow(ctx, created.ID)

	require.ErrorIs(t, err, store.ErrNameTaken)
	assert.ErrorContains(t, err, "agent", "the refusal names what is in the way")

	archived, err := st.Agents.ListArchivedInWorkflow(ctx, created.ID)
	require.NoError(t, err)
	assert.Len(t, archived, 2, "and both stay archived")
}
