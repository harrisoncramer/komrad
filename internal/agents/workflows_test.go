package agents

import (
	"errors"
	"path/filepath"
	"testing"

	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	schedulestore "weave/internal/store/schedule"
	workflowstore "weave/internal/store/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewWorkflow pins what a workflow is stored as, a blank name falling back to a label so
// the list never draws an empty row.
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

// TestUpdateWorkflow pins that every field is written on an update, and which refusals an
// update can raise.
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
// and that enabling it is the whole of what a workflow says about its agents' directories.
func TestNewWorkflowWithWorktree(t *testing.T) {
	repo := initRepo(t)

	tests := []struct {
		name         string
		cfg          workflowstore.Config
		wantErrIs    error
		wantWorktree bool
	}{
		{
			name:      "enabling it with no cwd is refused, since there is no repo to work against",
			cfg:       workflowstore.Config{Name: "backend", Worktree: workflowstore.WorktreeConfig{Enabled: true}},
			wantErrIs: ErrWorktreeNeedsCwd,
		},
		{
			name: "enabling it points the provisioner at the built-in pair",
			cfg: workflowstore.Config{
				Name:     "backend",
				Cwd:      repo,
				Worktree: workflowstore.WorktreeConfig{Enabled: true, CopyPaths: []string{".env"}},
			},
			wantWorktree: true,
		},
		{
			name: "an absolute copy path is refused, since the lists are relative to the repo",
			cfg: workflowstore.Config{
				Name:     "backend",
				Cwd:      "/repo",
				Worktree: workflowstore.WorktreeConfig{Enabled: true, CopyPaths: []string{"/etc/passwd"}},
			},
			wantErrIs: ErrWorktreeUnsafePath,
		},
		{
			name: "a copy path climbing out of the repo is refused",
			cfg: workflowstore.Config{
				Name:     "backend",
				Cwd:      "/repo",
				Worktree: workflowstore.WorktreeConfig{Enabled: true, CopyPaths: []string{"../../secrets"}},
			},
			wantErrIs: ErrWorktreeUnsafePath,
		},
		{
			name: "a symlink path naming the git directory is refused, since it is the worktree's own link",
			cfg: workflowstore.Config{
				Name:     "backend",
				Cwd:      "/repo",
				Worktree: workflowstore.WorktreeConfig{Enabled: true, SymlinkPaths: []string{".git"}},
			},
			wantErrIs: ErrWorktreeUnsafePath,
		},
		{
			name: "a path with a newline in it is refused, since a newline is what separates the entries",
			cfg: workflowstore.Config{
				Name:     "backend",
				Cwd:      "/repo",
				Worktree: workflowstore.WorktreeConfig{Enabled: true, CopyPaths: []string{".env\n../escape"}},
			},
			wantErrIs: ErrWorktreeUnsafePath,
		},
		{
			name: "a path nested inside the repo is kept",
			cfg: workflowstore.Config{
				Name:     "backend",
				Cwd:      repo,
				Worktree: workflowstore.WorktreeConfig{Enabled: true, CopyPaths: []string{"config/local.env"}},
			},
			wantWorktree: true,
		},
		{
			name: "a copy path naming the repo itself is refused, since it would clear the worktree",
			cfg: workflowstore.Config{
				Name:     "backend",
				Cwd:      "/repo",
				Worktree: workflowstore.WorktreeConfig{Enabled: true, CopyPaths: []string{"."}},
			},
			wantErrIs: ErrWorktreeUnsafePath,
		},
		{
			name: "a symlink path that cleans away to the repo itself is refused too",
			cfg: workflowstore.Config{
				Name:     "backend",
				Cwd:      "/repo",
				Worktree: workflowstore.WorktreeConfig{Enabled: true, SymlinkPaths: []string{"vendor/.."}},
			},
			wantErrIs: ErrWorktreeUnsafePath,
		},
		{
			name: "a path reaching the git directory the long way round is refused",
			cfg: workflowstore.Config{
				Name:     "backend",
				Cwd:      "/repo",
				Worktree: workflowstore.WorktreeConfig{Enabled: true, CopyPaths: []string{"vendor/../.git"}},
			},
			wantErrIs: ErrWorktreeUnsafePath,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)

			created, err := workflowsFor(st, newFakeTmux()).NewWorkflow(t.Context(), tt.cfg)
			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantWorktree, created.Worktree.Enabled)
		})
	}
}

// TestNewWorkflowRefusesACwdThatIsNotAPrimaryCheckout pins that a worktree workflow is
// refused unless its cwd is a repository agents can be branched off.
func TestNewWorkflowRefusesACwdThatIsNotAPrimaryCheckout(t *testing.T) {
	tests := []struct {
		name string
		cwd  func(t *testing.T) string
	}{
		{
			name: "cwd is not a git repository at all",
			cwd: func(t *testing.T) string {
				return t.TempDir()
			},
		},
		{
			name: "cwd is a linked worktree rather than the primary checkout",
			cwd: func(t *testing.T) string {
				repo := initRepo(t)
				tree := filepath.Join(t.TempDir(), "tree")
				runGit(t, repo, "worktree", "add", "--quiet", "-b", "feature", tree)

				return tree
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := workflowsFor(newStore(t), newFakeTmux()).NewWorkflow(t.Context(), workflowstore.Config{
				Name:     "backend",
				Cwd:      tt.cwd(t),
				Worktree: workflowstore.WorktreeConfig{Enabled: true},
			})
			require.ErrorIs(t, err, ErrWorktreeCwdNotPrimaryCheckout)
		})
	}
}

// TestUpdateWorkflowRefusesAnUnsafeWorktreePath covers the same validation on the update path.
func TestUpdateWorkflowRefusesAnUnsafeWorktreePath(t *testing.T) {
	st := newStore(t)
	workflows := workflowsFor(st, newFakeTmux())

	created, err := workflows.NewWorkflow(t.Context(), workflowstore.Config{
		Name:     "backend",
		Cwd:      initRepo(t),
		Worktree: workflowstore.WorktreeConfig{Enabled: true},
	})
	require.NoError(t, err)

	err = workflows.UpdateWorkflow(t.Context(), created.ID, workflowstore.Config{
		Name:     "backend",
		Cwd:      "/repo",
		Worktree: workflowstore.WorktreeConfig{Enabled: true, SymlinkPaths: []string{"../elsewhere"}},
	})
	require.ErrorIs(t, err, ErrWorktreeUnsafePath)
}

// TestWorkflowWorktreePathsAreStoredCleaned covers the spelling a workflow's paths are stored
// in.
func TestWorkflowWorktreePathsAreStoredCleaned(t *testing.T) {
	st := newStore(t)
	workflows := workflowsFor(st, newFakeTmux())
	repo := initRepo(t)

	created, err := workflows.NewWorkflow(t.Context(), workflowstore.Config{
		Name: "backend",
		Cwd:  repo,
		Worktree: workflowstore.WorktreeConfig{
			Enabled:      true,
			CopyPaths:    []string{"./.env", "config/./local.env"},
			SymlinkPaths: []string{"./node_modules/"},
			DiffExclude:  []string{"./proto/gen"},
		},
	})
	require.NoError(t, err)

	assert.Equal(t, []string{".env", "config/local.env"}, created.Worktree.CopyPaths)
	assert.Equal(t, []string{"node_modules"}, created.Worktree.SymlinkPaths)
	assert.Equal(t, []string{"proto/gen"}, created.Worktree.DiffExclude)

	err = workflows.UpdateWorkflow(t.Context(), created.ID, workflowstore.Config{
		Name: "backend",
		Cwd:  repo,
		Worktree: workflowstore.WorktreeConfig{
			Enabled:      true,
			CopyPaths:    []string{"./secrets/api.key"},
			SymlinkPaths: []string{"./vendor"},
			DiffExclude:  []string{"./db/models"},
		},
	})
	require.NoError(t, err)

	updated, err := st.GetWorkflow(t.Context(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"secrets/api.key"}, updated.Worktree.CopyPaths)
	assert.Equal(t, []string{"vendor"}, updated.Worktree.SymlinkPaths)
	assert.Equal(t, []string{"db/models"}, updated.Worktree.DiffExclude)
}

// TestUpdateWorkflowWorktree covers turning worktree support on through an update and turning
// it back off, which is the whole of what a workflow can say about its agents' directories.
func TestUpdateWorkflowWorktree(t *testing.T) {
	st := newStore(t)
	workflows := workflowsFor(st, newFakeTmux())

	created, err := workflows.NewWorkflow(t.Context(), workflowstore.Config{Name: "backend", Cwd: "/repo"})
	require.NoError(t, err)

	err = workflows.UpdateWorkflow(t.Context(), created.ID, workflowstore.Config{
		Name:     "backend",
		Cwd:      initRepo(t),
		Worktree: workflowstore.WorktreeConfig{Enabled: true},
	})
	require.NoError(t, err)

	afterEnable, err := st.GetWorkflow(t.Context(), created.ID)
	require.NoError(t, err)
	assert.True(t, afterEnable.Worktree.Enabled)

	err = workflows.UpdateWorkflow(t.Context(), created.ID, workflowstore.Config{Name: "backend", Cwd: "/repo"})
	require.NoError(t, err)

	afterDisable, err := st.GetWorkflow(t.Context(), created.ID)
	require.NoError(t, err)
	assert.False(t, afterDisable.Worktree.Enabled, "and off means the agents keep what they are given")
}

// TestUpdateWorkflowWorktreeDisablePreservesTheDetails checks that disabling worktree
// support leaves the base branch and the two path lists alone, so re-enabling later hands
// them back rather than starting blank.
func TestUpdateWorkflowWorktreeDisablePreservesTheDetails(t *testing.T) {
	st := newStore(t)
	workflows := workflowsFor(st, newFakeTmux())

	details := workflowstore.WorktreeConfig{
		BaseBranch:   "develop",
		CopyPaths:    []string{".env"},
		SymlinkPaths: []string{"node_modules"},
	}

	repo := initRepo(t)

	created, err := workflows.NewWorkflow(t.Context(), workflowstore.Config{
		Name: "backend",
		Cwd:  repo,
		Worktree: workflowstore.WorktreeConfig{
			Enabled:      true,
			BaseBranch:   details.BaseBranch,
			CopyPaths:    details.CopyPaths,
			SymlinkPaths: details.SymlinkPaths,
		},
	})
	require.NoError(t, err)

	disabledDetails := details
	disabledDetails.Enabled = false
	err = workflows.UpdateWorkflow(t.Context(), created.ID, workflowstore.Config{
		Name:     "backend",
		Cwd:      "/repo",
		Worktree: disabledDetails,
	})
	require.NoError(t, err)

	disabled, err := st.GetWorkflow(t.Context(), created.ID)
	require.NoError(t, err)
	assert.False(t, disabled.Worktree.Enabled)
	assert.Equal(t, "develop", disabled.Worktree.BaseBranch, "the base branch is not stripped just because the toggle went off")
	assert.Equal(t, []string{".env"}, disabled.Worktree.CopyPaths, "the copy paths are not stripped just because the toggle went off")
	assert.Equal(t, []string{"node_modules"}, disabled.Worktree.SymlinkPaths, "the symlink paths are not stripped just because the toggle went off")

	enabledDetails := details
	enabledDetails.Enabled = true
	err = workflows.UpdateWorkflow(t.Context(), created.ID, workflowstore.Config{
		Name:     "backend",
		Cwd:      repo,
		Worktree: enabledDetails,
	})
	require.NoError(t, err)

	reenabled, err := st.GetWorkflow(t.Context(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, "develop", reenabled.Worktree.BaseBranch, "re-enabling hands back what was there before")
	assert.Equal(t, []string{".env"}, reenabled.Worktree.CopyPaths, "re-enabling hands back what was there before")
}

// TestWorkflowArchiveOnPRClosed covers the setting surviving a create and an update.
func TestWorkflowArchiveOnPRClosed(t *testing.T) {
	st := newStore(t)
	workflows := workflowsFor(st, newFakeTmux())
	repo := initRepo(t)

	created, err := workflows.NewWorkflow(t.Context(), workflowstore.Config{
		Name:              "backend",
		Cwd:               repo,
		Worktree:          workflowstore.WorktreeConfig{Enabled: true},
		ArchiveOnPRClosed: true,
	})
	require.NoError(t, err)
	assert.True(t, created.ArchiveOnPRClosed)

	err = workflows.UpdateWorkflow(t.Context(), created.ID, workflowstore.Config{
		Name:     "backend",
		Cwd:      repo,
		Worktree: workflowstore.WorktreeConfig{Enabled: true},
	})
	require.NoError(t, err)

	updated, err := st.GetWorkflow(t.Context(), created.ID)
	require.NoError(t, err)
	assert.False(t, updated.ArchiveOnPRClosed)
}

// TestArchiveWorkflowKillsAPaneWrittenBeforeItsOwnWrite covers the same gap an archive of one
// agent has, on the archive of a whole folder of them: the agents it read held no pane, so it
// killed none, and the transaction that follows takes one off a row and hides it.
func TestArchiveWorkflowKillsAPaneWrittenBeforeItsOwnWrite(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	ctx := t.Context()

	workflowID := seedWorkflow(t, st)
	agent := seedInWorkflow(t, st, workflowID, "dormant", "", agentstore.StateIdle)

	workflows := NewWorkflowService(WorkflowParams{
		Workflows: st.Workflows,
		Agents:    st.Agents,
		Schedules: st.Schedules,
		Tmux:      tmux,
		Tx:        txWritingPaneFirst{txRunner: st.Pool, inTheGap: resumeInto(t, st, tmux, agent, "%9")},
	})

	require.NoError(t, workflows.ArchiveWorkflow(ctx, workflowID))

	after := agentByID(t, st, agent.ID)
	require.NotNil(t, after.ArchivedAt, "the agent really is archived with its workflow")
	assert.Contains(t, tmux.killedPanes, "%9", "so the archive kills the pane its write took")
}

// TestArchiveWorkflow pins both halves committing together, so no agent is left running
// behind a workflow nothing lists.
func TestArchiveWorkflow(t *testing.T) {
	tests := []struct {
		name         string
		create       bool
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

				_, err = manager.launch.NewAgent(ctx, NewAgentParams{
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

			_, err = st.Get(ctx, "agent")
			require.ErrorIs(t, err, store.ErrNotFound, "the agent is hidden from the live listing")

			all, err := st.ListAll(ctx)
			require.NoError(t, err)
			require.Len(t, all, 1)
			assert.NotNil(t, all[0].ArchivedAt, "and archived rather than removed")
			assert.Equal(t, []string{"%agent"}, tmux.killedPanes, "its CLI is stopped rather than left generating")
			assert.Empty(t, all[0].Pane, "and the pane it no longer owns is forgotten")
		})
	}
}

// TestRestoreWorkflow covers a workflow coming back with its agents as rows, and a name a
// live agent has taken since refusing the whole restore.
func TestRestoreWorkflow(t *testing.T) {
	tests := []struct {
		name      string
		archive   bool
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

				_, err = manager.launch.NewAgent(ctx, NewAgentParams{
					Title:      "agent",
					Dir:        t.TempDir(),
					WorkflowID: id,
				})
				require.NoError(t, err)
			}
			if tt.archive {
				require.NoError(t, workflows.ArchiveWorkflow(ctx, id))
			}

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

			assert.Empty(t, inside[0].Pane, "and it comes back owning no pane")
		})
	}
}

// TestRestoreWorkflowCeiling pins that a restore without room for every agent inside writes
// nothing.
func TestRestoreWorkflowCeiling(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	manager := newManager(tmux, st, "")
	workflows := st.workflowService(workflowServiceOptions{tmux: tmux, maxAgents: 3})
	ctx := t.Context()

	created, err := workflows.NewWorkflow(ctx, workflowstore.Config{Name: "backend"})
	require.NoError(t, err)

	for _, title := range []string{"one", "two"} {
		_, err := manager.launch.NewAgent(ctx, NewAgentParams{
			Title:      title,
			Dir:        t.TempDir(),
			WorkflowID: created.ID,
		})
		require.NoError(t, err)
	}
	require.NoError(t, workflows.ArchiveWorkflow(ctx, created.ID))

	elsewhere, err := workflows.NewWorkflow(ctx, workflowstore.Config{Name: "frontend"})
	require.NoError(t, err)
	for _, title := range []string{"live-one", "live-two"} {
		_, err := manager.launch.NewAgent(ctx, NewAgentParams{
			Title:      title,
			Dir:        t.TempDir(),
			WorkflowID: elsewhere.ID,
		})
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

func workflowNames(listed []workflowstore.Workflow) []string {
	names := make([]string, 0, len(listed))
	for _, w := range listed {
		names = append(names, w.Name)
	}

	return names
}

// TestRestoreAgentRestoresItsWorkflow pins that one agent brings its archived workflow back
// with it, or it would be live under a header the tree does not list.
func TestRestoreAgentRestoresItsWorkflow(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	manager := newManager(tmux, st, "")
	workflows := workflowsFor(st, tmux)
	ctx := t.Context()

	created, err := workflows.NewWorkflow(ctx, workflowstore.Config{Name: "backend"})
	require.NoError(t, err)

	agent, err := manager.launch.NewAgent(ctx, NewAgentParams{
		Title:      "one",
		Dir:        t.TempDir(),
		WorkflowID: created.ID,
	})
	require.NoError(t, err)
	other, err := manager.launch.NewAgent(ctx, NewAgentParams{
		Title:      "two",
		Dir:        t.TempDir(),
		WorkflowID: created.ID,
	})
	require.NoError(t, err)

	require.NoError(t, workflows.ArchiveWorkflow(ctx, created.ID))

	restored, err := manager.lifecycle.RestoreAgents(ctx, []string{agent.ID})
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

// TestRestoreWorkflowRefusesTwoArchivedNamesakes pins the collision between returning agents,
// which archived rows sitting outside the unique index make possible.
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
	require.ErrorContains(t, err, "agent", "the refusal names what is in the way")

	archived, err := st.Agents.ListArchivedInWorkflow(ctx, created.ID)
	require.NoError(t, err)
	assert.Len(t, archived, 2, "and both stay archived")
}

// TestDeleteWorkflow covers permanently removing an archived workflow, where a straggler
// DeleteAgents cannot finish leaves both it and the workflow behind rather than erroring.
func TestDeleteWorkflow(t *testing.T) {
	tests := []struct {
		name        string
		create      bool
		archive     bool
		addAgent    bool
		addSchedule bool
		wantErrIs   error
	}{
		{
			name:      "an id nobody holds",
			wantErrIs: ErrWorkflowNotFound,
		},
		{
			name:      "a live workflow is refused",
			create:    true,
			wantErrIs: ErrWorkflowNotFound,
		},
		{
			name:    "an archived workflow with no agents is deleted outright",
			create:  true,
			archive: true,
		},
		{
			name:     "its archived agents go with it",
			create:   true,
			addAgent: true,
			archive:  true,
		},
		{
			name:        "its archived schedules go with it",
			create:      true,
			addSchedule: true,
			archive:     true,
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
			}
			if tt.addAgent {
				_, err := manager.launch.NewAgent(ctx, NewAgentParams{
					Title:      "agent",
					Dir:        t.TempDir(),
					WorkflowID: id,
				})
				require.NoError(t, err)
			}
			var scheduleID string
			if tt.addSchedule {
				schedule, err := st.Schedules.Create(ctx, schedulestore.Config{
					Name:       "nightly",
					Cron:       "0 3 * * *",
					WorkflowID: id,
				})
				require.NoError(t, err)
				require.NoError(t, st.Schedules.Archive(ctx, schedule.ID))
				scheduleID = schedule.ID
			}
			if tt.archive {
				require.NoError(t, workflows.ArchiveWorkflow(ctx, id))
			}

			err := workflows.DeleteWorkflow(ctx, id)
			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)

				return
			}

			require.NoError(t, err)

			_, err = st.Workflows.GetIncludingArchived(ctx, id)
			require.ErrorIs(t, err, store.ErrNotFound, "the workflow row is gone")

			if tt.addAgent {
				all, err := st.ListAll(ctx)
				require.NoError(t, err)
				assert.Empty(t, all, "its agent goes with it")
			}

			if tt.addSchedule {
				_, err := st.Schedules.Get(ctx, scheduleID)
				assert.ErrorIs(t, err, store.ErrNotFound, "its archived schedule goes with it")
			}
		})
	}
}

// TestDeleteWorkflowRefusesAScheduleAnotherWorkflowsAgentsHold pins the refusal landing before
// any agent is cascaded to.
func TestDeleteWorkflowRefusesAScheduleAnotherWorkflowsAgentsHold(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	manager := newManager(tmux, st, "")
	workflows := workflowsFor(st, tmux)
	ctx := t.Context()

	launcher, err := workflows.NewWorkflow(ctx, workflowstore.Config{Name: "launcher"})
	require.NoError(t, err)
	moved, err := workflows.NewWorkflow(ctx, workflowstore.Config{Name: "moved"})
	require.NoError(t, err)

	schedule, err := st.Schedules.Create(ctx, schedulestore.Config{
		Name:       "nightly",
		Cron:       "0 3 * * *",
		WorkflowID: launcher.ID,
	})
	require.NoError(t, err)

	_, err = manager.launch.NewAgent(ctx, NewAgentParams{
		Title:      "agent",
		Dir:        t.TempDir(),
		WorkflowID: launcher.ID,
		ScheduleID: &schedule.ID,
	})
	require.NoError(t, err)

	require.NoError(t, st.Schedules.Update(ctx, schedule.ID, schedulestore.Config{
		Name:       "nightly",
		Cron:       "0 3 * * *",
		WorkflowID: moved.ID,
	}))
	require.NoError(t, st.Schedules.Archive(ctx, schedule.ID))

	_, err = manager.launch.NewAgent(ctx, NewAgentParams{
		Title:      "inside",
		Dir:        t.TempDir(),
		WorkflowID: moved.ID,
	})
	require.NoError(t, err)

	require.NoError(t, workflows.ArchiveWorkflow(ctx, moved.ID))

	require.ErrorIs(t, workflows.DeleteWorkflow(ctx, moved.ID), ErrScheduleHasAgents)

	got, err := st.Workflows.GetIncludingArchived(ctx, moved.ID)
	require.NoError(t, err)
	assert.NotNil(t, got, "the workflow stays archived to be tried again")

	all, err := st.ListAll(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 2, "the agents inside it are left alone")
}

// TestDeleteWorkflowLeavesAStragglerAndItsWorkflowBehind covers a deprovisioner that will
// not run: DeleteAgents cannot destroy that row, so the workflow it is still in cannot be
// destroyed either, and neither is an error the caller sees, the same as emptying the archive.
func TestDeleteWorkflowLeavesAStragglerAndItsWorkflowBehind(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	lifecycle := &fakeLifecycle{deprovisionRunErr: errors.New("worktree is dirty")}
	agentSvc := st.agentServiceWithLifecycle(tmux, fakeHarnesses{harness: &fakeHarness{}}, lifecycle)
	workflows := NewWorkflowService(WorkflowParams{
		Workflows:    st.Workflows,
		Agents:       st.Agents,
		AgentDeleter: agentSvc.lifecycle,
		Emptied:      agentSvc.emptied,
		Schedules:    st.Schedules,
		Tmux:         tmux,
		Tx:           st.Pool,
	})
	ctx := t.Context()

	created, err := workflows.NewWorkflow(ctx, worktreeConfig(t, "backend"))
	require.NoError(t, err)

	agent, err := agentSvc.launch.NewAgent(ctx, NewAgentParams{
		Title:      "agent",
		Dir:        t.TempDir(),
		WorkflowID: created.ID,
	})
	require.NoError(t, err)

	require.NoError(t, workflows.ArchiveWorkflow(ctx, created.ID))

	require.NoError(t, workflows.DeleteWorkflow(ctx, created.ID))

	got, err := st.Workflows.GetIncludingArchived(ctx, created.ID)
	require.NoError(t, err)
	require.NotNil(t, got, "the workflow the straggler is still in stays archived")

	assert.Equal(t, agentstore.StateFailedToCleanup, agentByID(t, st, agent.ID).State)
}
