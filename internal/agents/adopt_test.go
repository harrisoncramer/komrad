package agents

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakePullRequests struct {
	pr         *PullRequest
	repository string
	resolveErr error
}

func (f fakePullRequests) Resolve(context.Context, string, string) (*PullRequest, error) {
	if f.resolveErr != nil {
		return nil, f.resolveErr
	}

	return f.pr, nil
}

func (f fakePullRequests) Repository(context.Context, string) (string, error) {
	return f.repository, nil
}

// TestAdoptPlacesTheAgent covers where an adoption lands, which turns on git letting only
// one checkout hold a branch.
func TestAdoptPlacesTheAgent(t *testing.T) {
	tests := []struct {
		name string
		// held says which checkout has the branch when adoption runs: none, one made
		// alongside the repo, or the repo itself.
		held      string
		wantErrIs error

		wantOwnWorktree bool
	}{
		{
			name:            "a branch nothing has checked out",
			held:            "nobody",
			wantOwnWorktree: true,
		},
		{
			name: "a branch some other checkout already holds",
			held: "another worktree",
		},
		{
			name:      "a branch the workflow's own checkout is sitting on",
			held:      "the repo",
			wantErrIs: ErrAdoptBranchInUse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := newFakeTmux()
			st := newStore(t)
			manager := newManager(tmux, st, "")

			repo := initRepo(t)
			runGit(t, repo, "branch", "someones/work")

			var elsewhere string
			switch tt.held {
			case "another worktree":
				elsewhere = filepath.Join(t.TempDir(), "theirs")
				runGit(t, repo, "worktree", "add", "--quiet", elsewhere, "someones/work")
			case "the repo":
				runGit(t, repo, "checkout", "--quiet", "someones/work")
			}

			created, err := manager.Adopt(t.Context(), AdoptParams{
				Ref:        "someones/work",
				Prompt:     "look at it",
				WorkflowID: seedWorktreeWorkflow(t, st, repo),
			})

			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)

				return
			}

			require.NoError(t, err)
			require.NotNil(t, created)

			stored := agentByID(t, st, created.ID)
			assert.Equal(t, "someones/work", stored.AdoptedBranch, "the branch it was put on is recorded as asked for")
			assert.Contains(t, stored.Prompt, "someones/work", "the agent is told what it walked into")
			assert.Contains(t, stored.Prompt, "look at it", "and still asked to do what it was asked to do")

			if tt.wantOwnWorktree {
				assert.Nil(t, stored.SetupAction, "the workflow's own setup action makes the worktree")
				assert.Nil(t, stored.CleanupAction)

				return
			}

			assert.True(t, sameDir(elsewhere, stored.Cwd), "the agent is launched into the checkout that already holds the branch")
			require.NotNil(t, stored.SetupAction)
			assert.Empty(t, *stored.SetupAction, "nothing is provisioned for a checkout somebody else made")
			require.NotNil(t, stored.CleanupAction)
			assert.Empty(t, *stored.CleanupAction, "and nothing tears it down either")
		})
	}
}

// TestAdoptRefusesADirectoryAnotherAgentHolds pins that two agents never share a worktree,
// since deleting either would remove the tree out from under the other.
func TestAdoptRefusesADirectoryAnotherAgentHolds(t *testing.T) {
	tests := []struct {
		name string
		// branch is what the sitting agent's worktree is on, and what the adoption asks for.
		branch string
		// adoptedBranch is what that agent recorded, blank for one that cut its own branch.
		adoptedBranch string
		archived      bool
	}{
		{
			name:   "a branch a weave agent cut for itself",
			branch: "agent/01a04396-15f9-7d80-be65-c197f03c8b52",
		},
		{
			name:          "a branch an archived agent adopted, whose worktree is still on disk",
			branch:        "someones/work",
			adoptedBranch: "someones/work",
			archived:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := newFakeTmux()
			st := newStore(t)
			manager := newManager(tmux, st, "")

			repo := initRepo(t)
			workflow := seedWorktreeWorkflow(t, st, repo)

			sitting := filepath.Join(t.TempDir(), "sitting")
			runGit(t, repo, "worktree", "add", "--quiet", "-b", tt.branch, sitting)

			require.NoError(t, st.Create(t.Context(), agentstore.Agent{
				Name: "sitting", Cwd: sitting, State: agentstore.StateIdle,
				WorkflowID: workflow, AdoptedBranch: tt.adoptedBranch,
			}))
			if tt.archived {
				require.NoError(t, st.SoftDelete(t.Context(), "sitting"))
			}

			_, err := manager.Adopt(t.Context(), AdoptParams{Ref: tt.branch, WorkflowID: workflow})

			require.ErrorIs(t, err, ErrAdoptBranchInUse)
			assert.ErrorContains(t, err, "sitting", "the refusal names the agent already in the directory")
		})
	}
}

// TestAdoptTakesOverFromAnAgentWhoseWorktreeIsGone pins that a branch is not held for an
// agent with no directory left, which would leave no way onto the work.
func TestAdoptTakesOverFromAnAgentWhoseWorktreeIsGone(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	manager := newManager(tmux, st, "")

	repo := initRepo(t)
	runGit(t, repo, "branch", "someones/work")
	workflow := seedWorktreeWorkflow(t, st, repo)

	first, err := manager.Adopt(t.Context(), AdoptParams{Ref: "someones/work", WorkflowID: workflow})
	require.NoError(t, err)
	require.NoError(t, st.Agents.SetWorktreeRemovedByID(t.Context(), first.ID, true))

	second, err := manager.Adopt(t.Context(), AdoptParams{Ref: "someones/work", WorkflowID: workflow})
	require.NoError(t, err)

	assert.NotEqual(t, first.ID, second.ID, "the work is taken on by a new agent rather than handed to one with nowhere to run")
	assert.Equal(t, "someones/work", agentByID(t, st, second.ID).AdoptedBranch)
}

// TestAdoptRefusesWithoutAWorktreeWorkflow pins that adoption is refused where agents share
// one checkout, which would move a branch under somebody working in it.
func TestAdoptRefusesWithoutAWorktreeWorkflow(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	manager := newManager(tmux, st, "")

	_, err := manager.Adopt(t.Context(), AdoptParams{
		Ref:        "someones/work",
		WorkflowID: seedWorkflow(t, st),
	})

	require.ErrorIs(t, err, ErrAdoptNeedsWorktree)
}

// TestAdoptTwiceHandsBackTheSameAgent pins that asking again returns the agent already on
// the branch, since git cannot check it out twice.
func TestAdoptTwiceHandsBackTheSameAgent(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	manager := newManager(tmux, st, "")

	repo := initRepo(t)
	runGit(t, repo, "branch", "someones/work")
	workflow := seedWorktreeWorkflow(t, st, repo)

	first, err := manager.Adopt(t.Context(), AdoptParams{Ref: "someones/work", WorkflowID: workflow})
	require.NoError(t, err)

	second, err := manager.Adopt(t.Context(), AdoptParams{Ref: "someones/work", WorkflowID: workflow})
	require.NoError(t, err)

	assert.Equal(t, first.ID, second.ID, "the agent already on the branch comes back")

	agents, err := st.List(t.Context())
	require.NoError(t, err)
	assert.Len(t, agents, 1, "and no second one was started")
}

// TestAdoptFromAPullRequest covers adopting by URL, which records the pull request as well
// as the branch so the panel's open-pr key works right away.
func TestAdoptFromAPullRequest(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	manager := newManager(tmux, st, "").WithPullRequests(fakePullRequests{
		repository: "chariot-giving/admin",
		pr: &PullRequest{
			URL:        "https://github.com/chariot-giving/admin/pull/1069",
			Number:     1069,
			Title:      "upload a logo",
			Branch:     "harry/eng-3518-logo-upload",
			BaseBranch: "main",
			Repository: "chariot-giving/admin",
		},
	})

	repo := initRepo(t)

	created, err := manager.Adopt(t.Context(), AdoptParams{
		Ref:        "https://github.com/chariot-giving/admin/pull/1069",
		WorkflowID: seedWorktreeWorkflow(t, st, repo),
	})
	require.NoError(t, err)

	stored := agentByID(t, st, created.ID)
	assert.Equal(t, "harry/eng-3518-logo-upload", stored.AdoptedBranch, "the branch comes from the pull request")
	assert.Equal(t, "pr-1069", stored.Name, "an unnamed adoption is named after the pull request")
	assert.Contains(t, stored.Prompt, "https://github.com/chariot-giving/admin/pull/1069")
	assert.Equal(t, "https://github.com/chariot-giving/admin/pull/1069", prMetadata(stored), "the pull request is recorded as the agent starts")
}

// TestAdoptRefusesAPullRequestItCannotWorkOn covers the two references that resolve
// perfectly well and still name a branch this repository does not have.
func TestAdoptRefusesAPullRequestItCannotWorkOn(t *testing.T) {
	tests := []struct {
		name       string
		resolver   fakePullRequests
		noResolver bool
		wantErrIs  error
	}{
		{
			name: "a pull request on another repository",
			resolver: fakePullRequests{
				repository: "chariot-giving/admin",
				pr: &PullRequest{
					URL:        "https://github.com/chariot-giving/chariot/pull/12",
					Branch:     "harry/elsewhere",
					Repository: "chariot-giving/chariot",
				},
			},
			wantErrIs: ErrAdoptRepoMismatch,
		},
		{
			name: "a pull request from a fork",
			resolver: fakePullRequests{
				repository: "chariot-giving/admin",
				pr: &PullRequest{
					URL:             "https://github.com/chariot-giving/admin/pull/13",
					Branch:          "patch-1",
					Repository:      "chariot-giving/admin",
					CrossRepository: true,
				},
			},
			wantErrIs: ErrAdoptForkBranch,
		},
		{
			name:       "a pull request with nothing to read it",
			noResolver: true,
			wantErrIs:  ErrAdoptNoResolver,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := newFakeTmux()
			st := newStore(t)
			manager := newManager(tmux, st, "")
			if !tt.noResolver {
				manager = manager.WithPullRequests(tt.resolver)
			}

			_, err := manager.Adopt(t.Context(), AdoptParams{
				Ref:        "https://github.com/chariot-giving/admin/pull/1069",
				WorkflowID: seedWorktreeWorkflow(t, st, initRepo(t)),
			})

			require.ErrorIs(t, err, tt.wantErrIs)

			agents, listErr := st.List(t.Context())
			require.NoError(t, listErr)
			assert.Empty(t, agents, "a refused adoption starts nothing")
		})
	}
}

// TestAdoptReportsAFailureToRead covers gh being unreachable, unauthenticated, or
// simply not installed, which must read as the lookup failing rather than as a branch by
// that name.
func TestAdoptReportsAFailureToRead(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	manager := newManager(tmux, st, "").WithPullRequests(fakePullRequests{resolveErr: errors.New("gh: not logged in")})

	_, err := manager.Adopt(t.Context(), AdoptParams{
		Ref:        "1069",
		WorkflowID: seedWorktreeWorkflow(t, st, initRepo(t)),
	})

	require.ErrorContains(t, err, "gh: not logged in")
}

func TestLooksLikePullRequest(t *testing.T) {
	tests := []struct {
		ref  string
		want bool
	}{
		{ref: "https://github.com/chariot-giving/admin/pull/1069", want: true},
		{ref: "1069", want: true},
		{ref: "#1069", want: true},
		{ref: "harry/eng-3518-logo-upload"},
		{ref: "main"},
		{ref: "release/2026-08-27"},
		{ref: "1069-fix"},
		{ref: "-1"},
		{ref: "+1"},
	}

	for _, tt := range tests {
		t.Run(tt.ref, func(t *testing.T) {
			assert.Equal(t, tt.want, looksLikePullRequest(tt.ref))
		})
	}
}

// TestRepositoryFromRemote covers the remotes a checkout is cloned with, since what a pull
// request URL is checked against is read out of one of them.
func TestRepositoryFromRemote(t *testing.T) {
	tests := []struct {
		remote string
		want   string
	}{
		{remote: "git@github.com:chariot-giving/admin.git", want: "chariot-giving/admin"},
		{remote: "https://github.com/chariot-giving/admin.git", want: "chariot-giving/admin"},
		{remote: "https://github.com/chariot-giving/admin", want: "chariot-giving/admin"},
		{remote: "ssh://git@github.com/chariot-giving/admin.git", want: "chariot-giving/admin"},
		{remote: "/some/local/clone", want: ""},
		{remote: "file:///some/local/clone", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.remote, func(t *testing.T) {
			assert.Equal(t, tt.want, repositoryFromRemote(tt.remote))
		})
	}
}

func seedWorktreeWorkflow(t *testing.T, st *testStore, repo string) string {
	t.Helper()

	created, err := st.Workflows.Create(t.Context(), workflowstore.Config{
		Name:          "workflow-" + uuid.NewString(),
		Cwd:           repo,
		SetupAction:   "_worktree_setup",
		CleanupAction: "_worktree_cleanup",
		Worktree:      workflowstore.WorktreeConfig{Enabled: true},
	})
	require.NoError(t, err)
	require.NotNil(t, created)

	return created.ID
}

func prMetadata(agent agentstore.Agent) string {
	for _, entry := range agent.Metadata {
		if entry.Key == prMetadataKey {
			return entry.Value
		}
	}

	return ""
}
