package agents

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"weave/internal/git"

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
		held            string
		wantErrIs       error
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
			name:            "a branch whose only checkout was deleted without telling git",
			held:            "a worktree that is gone",
			wantOwnWorktree: true,
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
			repo := fakeCheckout(t)

			// What git reports about the repository's checkouts, the main one first.
			reported := []git.Worktree{{Dir: repo}}

			var elsewhere string
			switch tt.held {
			case "another worktree":
				elsewhere = filepath.Join(t.TempDir(), "theirs")
				require.NoError(t, os.MkdirAll(elsewhere, 0o755))
				reported = append(reported, git.Worktree{Dir: elsewhere, Branch: "someones/work"})
			case "a worktree that is gone":
				// Registered with git, and no longer on disk, which is what deleting one by hand
				// leaves behind.
				stale := filepath.Join(t.TempDir(), "stale")
				reported = append(reported, git.Worktree{Dir: stale, Branch: "someones/work"})
			case "the repo":
				reported = []git.Worktree{{Dir: repo, Branch: "someones/work"}}
			}

			manager := newManager(tmux, st, "").withWorktrees(fakeWorktrees(reported))

			created, err := manager.adopt.Adopt(t.Context(), AdoptParams{
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
				assert.Nil(t, stored.ProvisionEnabled, "the workflow's own worktree is the one it gets")

				return
			}

			assert.True(t, isSameDir(elsewhere, stored.Cwd), "the agent is launched into the checkout that already holds the branch")
			require.NotNil(t, stored.ProvisionEnabled)
			assert.False(t, *stored.ProvisionEnabled, "nothing is provisioned for a checkout somebody else made")
			assert.Empty(t, deprovisionerFor(t.Context(), st.Workflows, stored), "so nothing tears it down either")
		})
	}
}

// fakeWorktrees is what git reports about a repository's checkouts, the main one first.
type fakeWorktrees []git.Worktree

func (f fakeWorktrees) ListWorktrees(context.Context, string) ([]git.Worktree, error) {
	return f, nil
}

// TestAdoptRefusesADirectoryAnotherAgentHolds pins that two agents never share a worktree.
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

			repo := fakeCheckout(t)
			workflow := seedWorktreeWorkflow(t, st, repo)

			sitting := filepath.Join(t.TempDir(), "sitting")
			require.NoError(t, os.MkdirAll(sitting, 0o755))
			manager.withWorktrees(fakeWorktrees{{Dir: repo}, {Dir: sitting, Branch: tt.branch}})

			require.NoError(t, st.Create(t.Context(), agentstore.Agent{
				Name: "sitting", Cwd: sitting, State: agentstore.StateIdle,
				WorkflowID: workflow, AdoptedBranch: tt.adoptedBranch,
			}))
			if tt.archived {
				require.NoError(t, st.SoftDelete(t.Context(), "sitting"))
			}

			_, err := manager.adopt.Adopt(t.Context(), AdoptParams{Ref: tt.branch, WorkflowID: workflow})

			require.ErrorIs(t, err, ErrAdoptBranchInUse)
			require.ErrorContains(t, err, "sitting", "the refusal names the agent already in the directory")
		})
	}
}

// TestAdoptTakesOverFromAnAgentWhoseWorktreeIsGone pins that a branch is not held for an
// agent with no directory left, which would leave no way onto the work.
func TestAdoptTakesOverFromAnAgentWhoseWorktreeIsGone(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	manager := newManager(tmux, st, "")

	workflow := seedWorktreeWorkflow(t, st, fakeCheckout(t))

	first, err := manager.adopt.Adopt(t.Context(), AdoptParams{Ref: "someones/work", WorkflowID: workflow})
	require.NoError(t, err)
	require.NoError(t, st.Agents.SetWorktreeRemovedByID(t.Context(), first.ID, true))

	second, err := manager.adopt.Adopt(t.Context(), AdoptParams{Ref: "someones/work", WorkflowID: workflow})
	require.NoError(t, err)

	assert.NotEqual(t, first.ID, second.ID, "the work is taken on by a new agent rather than handed to one with nowhere to run")
	assert.Equal(t, "someones/work", agentByID(t, st, second.ID).AdoptedBranch)
}

// TestAdoptTakesOverAWorktreeRegistrationWithNoDirectory pins that a checkout deleted by hand
// does not hold the branch against an adoption.
func TestAdoptTakesOverAWorktreeRegistrationWithNoDirectory(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	manager := newManager(tmux, st, "")

	repo := fakeCheckout(t)
	workflow := seedWorktreeWorkflow(t, st, repo)

	// Git keeps the registration of a checkout deleted by hand, so it still names the branch.
	sitting := filepath.Join(t.TempDir(), "sitting")
	manager.withWorktrees(fakeWorktrees{{Dir: repo}, {Dir: sitting, Branch: "someones/work"}})

	require.NoError(t, st.Create(t.Context(), agentstore.Agent{
		Name: "sitting", Cwd: sitting, State: agentstore.StateIdle,
		WorkflowID: workflow, AdoptedBranch: "someones/work",
	}))
	require.NoError(t, st.SoftDelete(t.Context(), "sitting"))

	created, err := manager.adopt.Adopt(t.Context(), AdoptParams{Ref: "someones/work", WorkflowID: workflow})
	require.NoError(t, err)

	stored := agentByID(t, st, created.ID)
	assert.Equal(t, "someones/work", stored.AdoptedBranch)
	assert.Nil(t, stored.ProvisionEnabled, "a worktree of its own is made rather than the directory that is not there")
}

// TestAdoptRefusesWithoutAWorktreeWorkflow pins that adoption is refused where agents share
// one checkout, which would move a branch under somebody working in it.
func TestAdoptRefusesWithoutAWorktreeWorkflow(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	manager := newManager(tmux, st, "")

	_, err := manager.adopt.Adopt(t.Context(), AdoptParams{
		Ref:        "someones/work",
		WorkflowID: seedWorkflow(t, st),
	})

	require.ErrorIs(t, err, ErrAdoptNeedsWorktree)
}

// TestAdoptTwiceIsRefused pins asking again being a refusal naming the agent that has the
// branch, since a success that started nothing reads as the call having done nothing at all.
func TestAdoptTwiceIsRefused(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	manager := newManager(tmux, st, "")

	workflow := seedWorktreeWorkflow(t, st, fakeCheckout(t))

	first, err := manager.adopt.Adopt(t.Context(), AdoptParams{Ref: "someones/work", WorkflowID: workflow})
	require.NoError(t, err)

	_, err = manager.adopt.Adopt(t.Context(), AdoptParams{Ref: "someones/work", WorkflowID: workflow})
	require.ErrorIs(t, err, ErrAdoptAlreadyAdopted)
	assert.Contains(t, err.Error(), first.Name, "the refusal names the agent holding the branch")

	agents, err := st.List(t.Context())
	require.NoError(t, err)
	assert.Len(t, agents, 1, "and no second one was started")
}

// TestAdoptWithTheSameIdempotencyKeyIsAnsweredNotRefused pins the one repeat that means "again,
// safely" rather than a second agent on the branch.
func TestAdoptWithTheSameIdempotencyKeyIsAnsweredNotRefused(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	manager := newManager(tmux, st, "")

	workflow := seedWorktreeWorkflow(t, st, fakeCheckout(t))

	params := AdoptParams{
		Ref:            "someones/work",
		WorkflowID:     workflow,
		IdempotencyKey: "one-adopt",
	}

	first, err := manager.adopt.Adopt(t.Context(), params)
	require.NoError(t, err)

	second, err := manager.adopt.Adopt(t.Context(), params)
	require.NoError(t, err, "a retry carrying the key that launched it is not a second adoption")
	assert.Equal(t, first.ID, second.ID)

	_, err = manager.adopt.Adopt(t.Context(), AdoptParams{
		Ref:            "someones/work",
		WorkflowID:     workflow,
		IdempotencyKey: "another-adopt",
	})
	require.ErrorIs(t, err, ErrAdoptAlreadyAdopted, "another key is another caller, so the branch is taken")
}

// TestAdoptAfterASetupFailureIsNotRefusedByTheCorpse pins a create that never launched leaving
// the branch adoptable, since a refusal is only useful if the retry it invites can succeed.
func TestAdoptAfterASetupFailureIsNotRefusedByTheCorpse(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	manager := newManager(tmux, st, "")

	workflow := seedWorktreeWorkflow(t, st, fakeCheckout(t))

	first, err := manager.adopt.Adopt(t.Context(), AdoptParams{Ref: "someones/work", WorkflowID: workflow})
	require.NoError(t, err)
	require.NoError(t, st.Agents.SetStateByID(t.Context(), first.ID, agentstore.StateFailedToCreate))

	second, err := manager.adopt.Adopt(t.Context(), AdoptParams{Ref: "someones/work", WorkflowID: workflow})
	require.NoError(t, err, "the corpse does not hold the branch against a retry")
	assert.NotEqual(t, first.ID, second.ID)
}

// TestAdoptFromAPullRequest covers adopting by URL, which records the pull request as well
// as the branch so the panel's open-pr key works right away.
func TestAdoptFromAPullRequest(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	manager := newManager(tmux, st, "").withPullRequests(fakePullRequests{
		repository: "acmecorp/admin",
		pr: &PullRequest{
			URL:        "https://github.com/acmecorp/admin/pull/1069",
			Number:     1069,
			Title:      "upload a logo",
			Branch:     "harry/eng-3518-logo-upload",
			BaseBranch: "main",
			Repository: "acmecorp/admin",
		},
	})

	repo := fakeCheckout(t)

	created, err := manager.adopt.Adopt(t.Context(), AdoptParams{
		Ref:        "https://github.com/acmecorp/admin/pull/1069",
		WorkflowID: seedWorktreeWorkflow(t, st, repo),
	})
	require.NoError(t, err)

	stored := agentByID(t, st, created.ID)
	assert.Equal(t, "harry/eng-3518-logo-upload", stored.AdoptedBranch, "the branch comes from the pull request")
	assert.Equal(t, "pr-1069", stored.Name, "an unnamed adoption is named after the pull request")
	assert.Contains(t, stored.Prompt, "https://github.com/acmecorp/admin/pull/1069")
	assert.Equal(t, "https://github.com/acmecorp/admin/pull/1069", prMetadata(stored), "the pull request is recorded as the agent starts")
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
				repository: "acmecorp/admin",
				pr: &PullRequest{
					URL:        "https://github.com/acmecorp/monorepo/pull/12",
					Branch:     "harry/elsewhere",
					Repository: "acmecorp/monorepo",
				},
			},
			wantErrIs: ErrAdoptRepoMismatch,
		},
		{
			name: "a pull request from a fork",
			resolver: fakePullRequests{
				repository: "acmecorp/admin",
				pr: &PullRequest{
					URL:             "https://github.com/acmecorp/admin/pull/13",
					Branch:          "patch-1",
					Repository:      "acmecorp/admin",
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
				manager = manager.withPullRequests(tt.resolver)
			}

			_, err := manager.adopt.Adopt(t.Context(), AdoptParams{
				Ref:        "https://github.com/acmecorp/admin/pull/1069",
				WorkflowID: seedWorktreeWorkflow(t, st, fakeCheckout(t)),
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
	manager := newManager(tmux, st, "").withPullRequests(fakePullRequests{resolveErr: errors.New("gh: not logged in")})

	_, err := manager.adopt.Adopt(t.Context(), AdoptParams{
		Ref:        "1069",
		WorkflowID: seedWorktreeWorkflow(t, st, fakeCheckout(t)),
	})

	require.ErrorContains(t, err, "gh: not logged in")
}

// TestLooksLikePullRequest pins which refs read as a pull request and which are branch names.
func TestLooksLikePullRequest(t *testing.T) {
	tests := []struct {
		ref  string
		want bool
	}{
		{ref: "https://github.com/acmecorp/admin/pull/1069", want: true},
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
			assert.Equal(t, tt.want, isPullRequestRef(tt.ref))
		})
	}
}

// TestRepositoryFromRemote covers the remotes a checkout is cloned with.
func TestRepositoryFromRemote(t *testing.T) {
	tests := []struct {
		remote string
		want   string
	}{
		{remote: "git@github.com:acmecorp/admin.git", want: "acmecorp/admin"},
		{remote: "https://github.com/acmecorp/admin.git", want: "acmecorp/admin"},
		{remote: "https://github.com/acmecorp/admin", want: "acmecorp/admin"},
		{remote: "ssh://git@github.com/acmecorp/admin.git", want: "acmecorp/admin"},
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
		Name:     "workflow-" + uuid.NewString(),
		Cwd:      repo,
		Worktree: workflowstore.WorktreeConfig{Enabled: true},
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
