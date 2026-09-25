package doctor

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"testing"

	"komrad/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func dirsEnv(cfg *config.Config, dirs map[string]DirState, owners WorktreeOwners, ownersErr error) Env {
	return Env{
		Settings:   Settings{DatabaseURL: writableURL},
		LoadConfig: func() (*config.Config, error) { return cfg, nil },
		InspectDir: func(path string) (DirState, error) {
			state, known := dirs[path]
			if !known {
				return DirState{}, nil
			}

			if !state.Present {
				return DirState{}, fs.ErrPermission
			}

			return state, nil
		},
		ReadWorktreeOwners: func(context.Context, string) (WorktreeOwners, error) {
			return owners, ownersErr
		},
	}
}

func configWithDirs(t *testing.T, hooks, worktrees string) *config.Config {
	t.Helper()

	cfg := config.Defaults()
	cfg.Hooks.Dir = hooks
	cfg.Worktrees.Dir = worktrees

	return cfg
}

func presentDir(files, subdirs []string, writable bool) DirState {
	return DirState{
		Present:     true,
		Directory:   true,
		Writable:    writable,
		Files:       files,
		Directories: subdirs,
	}
}

// TestHooksCheck covers the reason this check exists: a hooks directory that has been moved
// away lists no hooks rather than saying it is gone, so an empty list and a missing directory
// look the same everywhere else in komrad.
func TestHooksCheck(t *testing.T) {
	const dir = "/home/u/.config/komrad/hooks"

	tests := []struct {
		name       string
		state      map[string]DirState
		want       Status
		wantCode   string
		wantDetail string
	}{
		{
			name:       "two hooks in it",
			state:      map[string]DirState{dir: presentDir([]string{"notify", "sync"}, nil, true)},
			want:       StatusOK,
			wantDetail: "2 hooks",
		},
		{
			name:       "there but empty",
			state:      map[string]DirState{dir: presentDir(nil, nil, true)},
			want:       StatusOK,
			wantDetail: "no hooks",
		},
		{
			name:     "not there at all",
			state:    nil,
			want:     StatusWarn,
			wantCode: "hooks_dir_absent",
		},
		{
			name:     "a file where the directory should be",
			state:    map[string]DirState{dir: {Present: true}},
			want:     StatusFail,
			wantCode: "hooks_dir_not_a_directory",
		},
		{
			name:     "there and unreadable",
			state:    map[string]DirState{dir: {}},
			want:     StatusFail,
			wantCode: "hooks_dir_unreadable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := dirsEnv(configWithDirs(t, dir, ""), tt.state, WorktreeOwners{}, nil)

			result := newHooksCheck().Run(t.Context(), env)

			assert.Equal(t, tt.want, result.Status)
			assert.Equal(t, tt.wantCode, codeOf(result.Err))
			if tt.wantDetail != "" {
				assert.Contains(t, result.Detail, tt.wantDetail)
			}
		})
	}
}

// TestHooksCheckReportsThePathAsTyped covers that a configured path is reported the way it was
// written.
func TestHooksCheckReportsThePathAsTyped(t *testing.T) {
	typed := "~/hooks"
	resolved := config.ExpandHome(typed)
	require.NotEqual(t, typed, resolved, "this test needs a home directory to expand")

	env := dirsEnv(configWithDirs(t, typed, ""),
		map[string]DirState{resolved: presentDir([]string{"notify"}, nil, true)}, WorktreeOwners{}, nil)

	result := newHooksCheck().Run(t.Context(), env)

	assert.Equal(t, StatusOK, result.Status)
	assert.Contains(t, result.Detail, typed)
	assert.NotContains(t, result.Detail, resolved)
}

// TestWorktreesCheck covers the directory a provisioner has to be able to write in, where the
// shared one being absent is not wrong on its own since a workflow may name its own.
func TestWorktreesCheck(t *testing.T) {
	const dir = "/home/u/.local/state/komrad/worktrees"

	tests := []struct {
		name       string
		state      map[string]DirState
		want       Status
		wantCode   string
		wantDetail string
		wantFix    string
	}{
		{
			name:       "there and writable",
			state:      map[string]DirState{dir: presentDir(nil, []string{"01a0", "01a1"}, true)},
			want:       StatusOK,
			wantDetail: "2 worktrees",
		},
		{
			name:     "not there at all",
			state:    nil,
			want:     StatusWarn,
			wantCode: "worktrees_dir_absent",
		},
		{
			name:     "there and read-only",
			state:    map[string]DirState{dir: presentDir(nil, nil, false)},
			want:     StatusFail,
			wantCode: "worktrees_dir_unwritable",
			wantFix:  "chmod u+w " + dir,
		},
		{
			name:     "a file where the directory should be",
			state:    map[string]DirState{dir: {Present: true}},
			want:     StatusFail,
			wantCode: "worktrees_dir_not_a_directory",
		},
		{
			name:     "there and unreadable",
			state:    map[string]DirState{dir: {}},
			want:     StatusFail,
			wantCode: "worktrees_dir_unreadable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := dirsEnv(configWithDirs(t, "", dir), tt.state, WorktreeOwners{}, nil)

			result := newWorktreesCheck().Run(t.Context(), env)

			assert.Equal(t, tt.want, result.Status)
			assert.Equal(t, tt.wantCode, codeOf(result.Err))
			if tt.wantDetail != "" {
				assert.Contains(t, result.Detail, tt.wantDetail)
			}
			if tt.wantFix != "" {
				assert.Contains(t, result.Fix, tt.wantFix)
			}
		})
	}
}

// TestOrphanedWorktreesCheck covers the one thing nothing else in komrad notices: a worktree
// is removed when the agent holding it is deleted and never otherwise, so a directory whose
// agent has gone stays on disk with nobody reporting it.
func TestOrphanedWorktreesCheck(t *testing.T) {
	const shared = "/home/u/worktrees"

	tests := []struct {
		name      string
		state     map[string]DirState
		owners    WorktreeOwners
		ownersErr error
		want      Status
		wantCode  string
		wantIn    []string
		wantNotIn []string
	}{
		{
			name:     "a directory with no agent",
			state:    map[string]DirState{shared: presentDir(nil, []string{"01a0", "01a1"}, true)},
			owners:   WorktreeOwners{AgentIDs: []string{"01a0"}},
			want:     StatusWarn,
			wantCode: "worktrees_unowned",
			wantIn:   []string{shared, "01a1"},
			// The agent that does hold a directory is not named.
			wantNotIn: []string{"01a0"},
		},
		{
			name:   "every directory has one",
			state:  map[string]DirState{shared: presentDir(nil, []string{"01a0", "01a1"}, true)},
			owners: WorktreeOwners{AgentIDs: []string{"01a0", "01a1"}},
			want:   StatusOK,
			wantIn: []string{"2 worktrees"},
		},
		{
			name:   "an adopted agent running in one komrad did not name",
			state:  map[string]DirState{shared: presentDir(nil, []string{"borrowed"}, true)},
			owners: WorktreeOwners{AgentIDs: []string{"01a0"}, AgentDirs: []string{filepath.Join(shared, "borrowed")}},
			want:   StatusOK,
		},
		{
			name:      "the database is not reachable",
			state:     map[string]DirState{shared: presentDir(nil, []string{"01a1"}, true)},
			ownersErr: errors.New("dial tcp 127.0.0.1:5460: connect: connection refused"),
			want:      StatusWarn,
			wantCode:  "worktree_owners_unreadable",
		},
		{
			name:   "nothing has been provisioned yet",
			state:  nil,
			owners: WorktreeOwners{},
			want:   StatusOK,
			wantIn: []string{"no worktrees"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := dirsEnv(configWithDirs(t, "", shared), tt.state, tt.owners, tt.ownersErr)

			result := newOrphanedWorktreesCheck().Run(t.Context(), env)

			assert.Equal(t, tt.want, result.Status)
			assert.Equal(t, tt.wantCode, codeOf(result.Err))

			reported := result.Detail
			if result.Err != nil {
				reported = result.Err.Error()
			}
			for _, want := range tt.wantIn {
				assert.Contains(t, reported, want)
			}
			for _, unwanted := range tt.wantNotIn {
				assert.NotContains(t, reported, unwanted)
			}
		})
	}
}

// TestOrphanedWorktreesCheckReadsTheWorkflowsOwnDirectories covers the half of the question
// the config cannot answer: a workflow may cut its worktrees somewhere of its own, and an
// orphan there is as unowned as one in the shared directory.
func TestOrphanedWorktreesCheckReadsTheWorkflowsOwnDirectories(t *testing.T) {
	const shared = "/home/u/worktrees"
	const own = "/repos/komrad/trees"

	env := dirsEnv(configWithDirs(t, "", shared), map[string]DirState{
		shared: presentDir(nil, []string{"01a0"}, true),
		own:    presentDir(nil, []string{"01a9"}, true),
	}, WorktreeOwners{AgentIDs: []string{"01a0"}, WorkflowDirs: []string{own}}, nil)

	result := newOrphanedWorktreesCheck().Run(t.Context(), env)

	assert.Equal(t, StatusWarn, result.Status)
	assert.Equal(t, "worktrees_unowned", codeOf(result.Err))
	assert.Contains(t, result.Err.Error(), "1 worktree in "+own+": 01a9")
	assert.NotContains(t, result.Err.Error(), "01a0", "the shared directory's one worktree has its agent")
}

// TestOrphanedWorktreesCheckNamesOnlyAFewOrphans covers that a directory full of leftovers stays
// one readable line.
func TestOrphanedWorktreesCheckNamesOnlyAFewOrphans(t *testing.T) {
	const shared = "/home/u/worktrees"

	left := []string{"01a0", "01a1", "01a2", "01a3", "01a4"}
	env := dirsEnv(configWithDirs(t, "", shared),
		map[string]DirState{shared: presentDir(nil, left, true)}, WorktreeOwners{}, nil)

	result := newOrphanedWorktreesCheck().Run(t.Context(), env)

	assert.Equal(t, StatusWarn, result.Status)
	assert.Contains(t, result.Err.Error(), "5 worktrees in "+shared)
	assert.Contains(t, result.Err.Error(), "and 2 more")
	assert.NotContains(t, result.Err.Error(), left[len(left)-1])
}

// TestCollectWorktreeDirsDedupes covers a workflow naming the shared directory outright,
// which would otherwise have every orphan in it reported twice.
func TestCollectWorktreeDirsDedupes(t *testing.T) {
	const shared = "/home/u/worktrees"

	dirs := collectWorktreeDirs("", shared, []string{shared, "  ", "/repos/trees"})

	assert.Equal(t, []worktreeDir{
		{shown: shared, resolved: shared},
		{shown: "/repos/trees", resolved: "/repos/trees"},
	}, dirs)
}
