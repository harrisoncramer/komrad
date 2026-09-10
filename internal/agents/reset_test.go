package agents

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"weave/internal/git"
	agentstore "weave/internal/store/agent"
	mailstore "weave/internal/store/mail"
	reviewstore "weave/internal/store/review"
	workflowstore "weave/internal/store/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildResetWorktree is an agent worktree with work of every kind in it: a commit of its own,
// an uncommitted edit, an untracked file and an ignored one.
func buildResetWorktree(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	origin := filepath.Join(root, "origin")
	gitRun(t, root, "init", "--quiet", "-b", "main", origin)
	gitRun(t, origin, "config", "user.email", "t@example.com")
	gitRun(t, origin, "config", "user.name", "t")
	writeTree(t, origin, "tracked.txt", "base\n")
	writeTree(t, origin, ".gitignore", "secrets.env\n")
	require.NoError(t, os.MkdirAll(filepath.Join(origin, "vendor"), 0o755))
	writeTree(t, origin, "vendor/README", "tracked, so git cannot collapse this directory\n")
	gitRun(t, origin, "add", "-A")
	gitRun(t, origin, "commit", "--quiet", "-m", "base")

	clone := filepath.Join(root, "clone")
	gitRun(t, root, "clone", "--quiet", origin, clone)
	gitRun(t, clone, "config", "user.email", "t@example.com")
	gitRun(t, clone, "config", "user.name", "t")

	tree := filepath.Join(root, "tree")
	gitRun(t, clone, "worktree", "add", "--quiet", "--no-track", "-b", "agent/a1", tree, "origin/main")

	writeTree(t, tree, "attempt.txt", "a bad attempt\n")
	gitRun(t, tree, "add", "attempt.txt")
	gitRun(t, tree, "commit", "--quiet", "-m", "an attempt worth abandoning")
	writeTree(t, tree, "tracked.txt", "edited and not committed\n")
	writeTree(t, tree, "scratch.txt", "untracked\n")
	writeTree(t, tree, "secrets.env", "SECRET=hunter2\n")

	// The base moves on after the worktree was cut, so a reset that landed on the fork point
	// rather than on the fetched base would be caught rather than passing either way.
	writeTree(t, origin, "moved.txt", "the base moved on\n")
	gitRun(t, origin, "add", "moved.txt")
	gitRun(t, origin, "commit", "--quiet", "-m", "the base moved on")

	return tree
}

func resetService(t *testing.T, tree string, rounds roundReadDeleter) *RefreshService {
	t.Helper()

	agents := &recordingRebaseAgents{agent: agentstore.Agent{
		ID: "a1", Name: "auth", WorkflowID: "w1", Cwd: tree, State: agentstore.StateIdle,
	}}

	return NewRefreshService(NewRefreshServiceParams{
		Agents:    agents,
		Workflows: fakeRebaseWorkflows{flow: worktreeFlow()},
		Rounds:    rounds,
	})
}

// TestResetKeepsWhatNothingCouldRecover covers the one thing a park cannot hold, since it is
// built with git add -A and so never sees an ignored file.
func TestResetKeepsWhatNothingCouldRecover(t *testing.T) {
	t.Parallel()

	tree := buildResetWorktree(t)
	service := resetService(t, tree, nil)

	result, err := service.Reset(t.Context(), "a1")
	require.NoError(t, err)
	require.NotEmpty(t, result.ParkedRef)

	assert.Equal(t, "SECRET=hunter2\n", readTree(t, tree, "secrets.env"), "the ignored file is still there")

	inPark, err := git.Run(t.Context(), tree, "cat-file", "-e", result.ParkedRef+":secrets.env")
	require.Error(t, err, "the park cannot hold an ignored file, which is why it must not be removed")
	assert.Empty(t, inPark)
}

// TestResetClearsTheUntrackedAttempt covers the files an abandoned attempt is mostly made of,
// which the park does hold and which would otherwise still compile into the package.
func TestResetClearsTheUntrackedAttempt(t *testing.T) {
	t.Parallel()

	tree := buildResetWorktree(t)
	service := resetService(t, tree, nil)

	result, err := service.Reset(t.Context(), "a1")
	require.NoError(t, err)

	assert.NoFileExists(t, filepath.Join(tree, "scratch.txt"), "the untracked file is gone from disk")

	kept, err := git.Run(t.Context(), tree, "show", result.ParkedRef+":scratch.txt")
	require.NoError(t, err, "and is recoverable from the park, which is what makes removing it safe")
	assert.Equal(t, "untracked", kept)
}

// TestResetLandsOnTheFetchedBase covers the base having moved since the worktree was cut, which
// is the half of the operation a fixture with a still origin cannot see.
func TestResetLandsOnTheFetchedBase(t *testing.T) {
	t.Parallel()

	tree := buildResetWorktree(t)
	service := resetService(t, tree, nil)

	_, err := service.Reset(t.Context(), "a1")
	require.NoError(t, err)

	assert.Equal(t, "the base moved on\n", readTree(t, tree, "moved.txt"),
		"the reset landed on the fetched base rather than on the fork point")
}

// TestResetDiscardsTheWorkAndParksIt covers the tracked half, where everything that goes has to
// be reachable from the park afterwards.
func TestResetDiscardsTheWorkAndParksIt(t *testing.T) {
	t.Parallel()

	tree := buildResetWorktree(t)
	before, err := git.Run(t.Context(), tree, "rev-parse", "HEAD")
	require.NoError(t, err)

	service := resetService(t, tree, nil)

	result, err := service.Reset(t.Context(), "a1")
	require.NoError(t, err)
	require.Equal(t, 1, result.Commits, "the fixture did not produce the commit this is about")
	require.True(t, result.TrackedChanges, "nor the uncommitted edit")

	assert.Equal(t, "base\n", readTree(t, tree, "tracked.txt"), "the uncommitted edit is gone")
	assert.NoFileExists(t, filepath.Join(tree, "attempt.txt"), "and so is the commit's file")

	parent, err := git.Run(t.Context(), tree, "rev-parse", result.ParkedRef+"^")
	require.NoError(t, err)
	assert.Equal(t, before, parent, "the discarded commit is reachable from the park")

	kept, err := git.Run(t.Context(), tree, "show", result.ParkedRef+":tracked.txt")
	require.NoError(t, err)
	assert.Equal(t, "edited and not committed", kept, "and so is the uncommitted edit")
}

// TestResetDoesNotRunWhenTheParkFails pins the ordering the whole safety argument rests on.
func TestResetDoesNotRunWhenTheParkFails(t *testing.T) {
	t.Parallel()

	tree := buildResetWorktree(t)
	before, err := git.Run(t.Context(), tree, "rev-parse", "HEAD")
	require.NoError(t, err)

	// A ref standing at the parent of the name the park wants makes every name under it
	// uncreatable, since refs are paths.
	gitRun(t, tree, "update-ref", "refs/weave/discarded/a1", before)

	service := resetService(t, tree, nil)

	_, err = service.Reset(t.Context(), "a1")
	require.ErrorIs(t, err, ErrResetPark)

	after, err := git.Run(t.Context(), tree, "rev-parse", "HEAD")
	require.NoError(t, err)
	assert.Equal(t, before, after, "nothing was reset")
	assert.Equal(t, "edited and not committed\n", readTree(t, tree, "tracked.txt"), "and nothing was discarded")
}

// TestResetTearsDownTheReviewHistory covers the rounds going, since they survive a reset on
// their own and would describe work that no longer exists.
func TestResetTearsDownTheReviewHistory(t *testing.T) {
	t.Parallel()

	tree := buildResetWorktree(t)
	snapshot, err := git.Run(t.Context(), tree, "rev-parse", "HEAD")
	require.NoError(t, err)
	gitRun(t, tree, "update-ref", roundRef("a1", snapshot), snapshot)

	rounds := &fakeRounds{rounds: []reviewstore.Round{{AgentID: "a1", SnapshotSHA: snapshot}}}
	service := resetService(t, tree, rounds)

	result, err := service.Reset(t.Context(), "a1")
	require.NoError(t, err)

	assert.Equal(t, 1, result.Rounds)
	assert.True(t, rounds.deleted, "the rows went with the refs")

	_, err = git.Run(t.Context(), tree, "rev-parse", "--verify", roundRef("a1", snapshot))
	require.Error(t, err, "the snapshot ref was released rather than left pinning an abandoned worktree")
}

func readTree(t *testing.T, dir, name string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(dir, name))
	require.NoError(t, err)

	return string(body)
}

type fakeRounds struct {
	rounds    []reviewstore.Round
	reviews   []reviewstore.Review
	listErr   error
	deleted   bool
	deleteErr error
}

func (f *fakeRounds) ListRounds(context.Context, string) ([]reviewstore.Round, error) {
	return f.rounds, nil
}

func (f *fakeRounds) ListReviews(context.Context, string) ([]reviewstore.Review, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}

	return f.reviews, nil
}

func (f *fakeRounds) DeleteRounds(context.Context, string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleted = true

	// The rows cascade, taking the reviews with them, without which no test could tell whether
	// the drafts were read before the teardown or after it.
	f.rounds = nil
	f.reviews = nil

	return nil
}

// TestResetIsReachableOnADirtyTree covers the case a reset exists for, which refusing the plan
// on a dirty tree would put out of reach.
func TestResetIsReachableOnADirtyTree(t *testing.T) {
	t.Parallel()

	tree := buildResetWorktree(t)
	service := resetService(t, tree, nil)

	plan, err := service.PlanRebase(t.Context(), "a1")
	require.NoError(t, err, "the plan is readable even though a replay could not run")
	assert.True(t, plan.TrackedChanges, "and says so, for the confirmation to name")

	_, err = service.Rebase(t.Context(), "a1")
	require.ErrorIs(t, err, ErrRebaseDirty, "the replay still refuses")

	_, err = service.Reset(t.Context(), "a1")
	require.NoError(t, err, "and the discard does not")
}

// TestResetRefusesWhatItCannotPark covers a path the index is holding back, whose content a
// park cannot read and so cannot give back.
func TestResetRefusesWhatItCannotPark(t *testing.T) {
	t.Parallel()

	tree := buildResetWorktree(t)
	gitRun(t, tree, "update-index", "--assume-unchanged", "tracked.txt")

	service := resetService(t, tree, nil)

	_, err := service.Reset(t.Context(), "a1")
	require.ErrorIs(t, err, ErrRefreshHiddenPaths)
}

// TestResetRefusesAnIgnoredFileTheBaseTracks covers the one loss a park cannot undo, since the
// park is built with git add -A and a reset writes every path the base tracks.
func TestResetRefusesAnIgnoredFileTheBaseTracks(t *testing.T) {
	t.Parallel()

	tree := buildResetWorktree(t)
	origin := filepath.Join(filepath.Dir(tree), "origin")

	// The base starts tracking the very path this worktree holds as an ignored file.
	writeTree(t, origin, ".gitignore", "")
	writeTree(t, origin, "secrets.env", "SHARED=from-the-base\n")
	gitRun(t, origin, "add", "-A")
	gitRun(t, origin, "commit", "--quiet", "-m", "the base now tracks it")

	service := resetService(t, tree, nil)

	_, err := service.Reset(t.Context(), "a1")
	require.ErrorIs(t, err, ErrResetShadowed)
	require.ErrorContains(t, err, "secrets.env", "the refusal names the path it is about")

	assert.Equal(t, "SECRET=hunter2\n", readTree(t, tree, "secrets.env"),
		"and the local file is untouched, since a reset would have overwritten it with no way back")
}

// TestResetDropsTheRowsBeforeTheRefs covers the order the review screen depends on, since a
// released snapshot under a surviving row reads as a bad object rather than an empty diff.
func TestResetDropsTheRowsBeforeTheRefs(t *testing.T) {
	t.Parallel()

	tree := buildResetWorktree(t)
	snapshot, err := git.Run(t.Context(), tree, "rev-parse", "HEAD")
	require.NoError(t, err)
	gitRun(t, tree, "update-ref", roundRef("a1", snapshot), snapshot)

	rounds := &fakeRounds{
		rounds:    []reviewstore.Round{{AgentID: "a1", SnapshotSHA: snapshot}},
		deleteErr: errors.New("the rows could not be deleted"),
	}
	service := resetService(t, tree, rounds)

	result, err := service.Reset(t.Context(), "a1")
	require.NoError(t, err, "the reset itself still stands")
	assert.Zero(t, result.Rounds, "and reports that no rounds went")

	_, err = git.Run(t.Context(), tree, "rev-parse", "--verify", roundRef("a1", snapshot))
	require.NoError(t, err, "the snapshot is still held, since its row survived to name it")
}

// TestResetRefusesAnOpenHumanReview covers a draft nothing holds a copy of, since the round
// teardown cascades through the reviews and the threads written under them.
func TestResetRefusesAnOpenHumanReview(t *testing.T) {
	t.Parallel()

	tree := buildResetWorktree(t)
	service := resetService(t, tree, &fakeRounds{
		reviews: []reviewstore.Review{{AgentID: "a1", AuthorKind: reviewstore.AuthorHuman}},
	})

	_, err := service.Reset(t.Context(), "a1")
	require.ErrorIs(t, err, ErrResetOpenReview)

	assert.Equal(t, "edited and not committed\n", readTree(t, tree, "tracked.txt"), "and nothing was discarded")
}

// TestResetAllowsAReleasedReview is the other side of that refusal, since a released review is
// not a draft anybody is holding.
func TestResetAllowsAReleasedReview(t *testing.T) {
	t.Parallel()

	released := time.Now()
	tree := buildResetWorktree(t)
	service := resetService(t, tree, &fakeRounds{
		reviews: []reviewstore.Review{{
			AgentID:    "a1",
			AuthorKind: reviewstore.AuthorHuman,
			ReleasedAt: &released,
		}},
	})

	_, err := service.Reset(t.Context(), "a1")
	require.NoError(t, err)
}

// TestResetParksWhereNothingCollectsIt covers the park being the only copy of destroyed work,
// which a later refresh of the same agent must not take away.
func TestResetParksWhereNothingCollectsIt(t *testing.T) {
	t.Parallel()

	tree := buildResetWorktree(t)
	service := resetService(t, tree, nil)

	result, err := service.Reset(t.Context(), "a1")
	require.NoError(t, err)
	require.Contains(t, result.ParkedRef, "refs/weave/discarded/",
		"a reset park lives apart from a rebase undo, which is collected")

	// A later refresh collects its own parks, and must leave this one standing.
	require.NoError(t, git.ReleaseRefsUnder(t.Context(), tree, "refs/weave/prerefresh/a1/", ""))

	_, err = git.Run(t.Context(), tree, "rev-parse", "--verify", result.ParkedRef)
	require.NoError(t, err, "the only copy of the discarded work is still there")
}

// TestResetSaysWhenTheReviewHistorySurvived covers the failure that would otherwise report as
// plain success, since a count of zero means both no rounds and a teardown that broke.
func TestResetSaysWhenTheReviewHistorySurvived(t *testing.T) {
	t.Parallel()

	tree := buildResetWorktree(t)
	snapshot, err := git.Run(t.Context(), tree, "rev-parse", "HEAD")
	require.NoError(t, err)
	gitRun(t, tree, "update-ref", roundRef("a1", snapshot), snapshot)

	service := resetService(t, tree, &fakeRounds{
		rounds:    []reviewstore.Round{{AgentID: "a1", SnapshotSHA: snapshot}},
		deleteErr: errors.New("the rows could not be deleted"),
	})

	result, err := service.Reset(t.Context(), "a1")
	require.NoError(t, err, "the git half is done, so unwinding it would be worse")

	assert.True(t, result.StaleReviewHistory, "and the failure is reported rather than read as no rounds")
	assert.Zero(t, result.Rounds)
	assert.Contains(t, resetMail(*result), "could not be cleared", "the agent is told not to answer it")

	_, err = git.Run(t.Context(), tree, "rev-parse", "--verify", roundRef("a1", snapshot))
	require.NoError(t, err, "the snapshot is still held, since its row survived to name it")
}

// TestResetMailSaysWhatWentAndWhatStayed covers the wording an agent acts on, which an earlier
// version of the rebase mail got wrong by asserting something the code had not measured.
func TestResetMailSaysWhatWentAndWhatStayed(t *testing.T) {
	t.Parallel()

	body := resetMail(ResetResult{
		ResetPlan: ResetPlan{
			BaseRef: "origin/main",
			BaseSHA: "19802cfc353a",
			Commits: 2,
		},
		ParkedRef: "refs/weave/discarded/a1/1",
	})

	assert.Contains(t, body, "2 commits", "it says what left the branch")
	assert.Contains(t, body, "never committed have been removed", "and that its own new files went too")
	assert.Contains(t, body, "ignoring", "while naming what was left alone")
	assert.Contains(t, body, "refs/weave/discarded/a1/1", "and where the only copy is")
	assert.NotContains(t, body, "Untracked and ignored files were left where they are",
		"which is what it used to claim, and it was false")
}

// recordingMail notes who was told what, since a reset has to reach the reviewer whose draft it
// took as well as the agent whose work it discarded.
type recordingMail struct {
	sent map[string]string
}

func (r *recordingMail) SendSystemMail(_ context.Context, toAgentID, body string) (*mailstore.Mail, error) {
	if r.sent == nil {
		r.sent = map[string]string{}
	}
	r.sent[toAgentID] = body

	return &mailstore.Mail{}, nil
}

// TestResetTellsAnAgentReviewerItsDraftWent covers a child reviewer part-way through writing
// feedback, whose batch the round teardown deletes without refusing.
func TestResetTellsAnAgentReviewerItsDraftWent(t *testing.T) {
	t.Parallel()

	tree := buildResetWorktree(t)
	snapshot, err := git.Run(t.Context(), tree, "rev-parse", "HEAD")
	require.NoError(t, err)

	reviewer := "reviewer-1"
	finished := "reviewer-0"
	released := time.Now()
	mail := &recordingMail{}
	service := NewRefreshService(NewRefreshServiceParams{
		Agents: &recordingRebaseAgents{agent: agentstore.Agent{
			ID: "a1", Name: "auth", WorkflowID: "w1", Cwd: tree, State: agentstore.StateIdle,
		}},
		Workflows: fakeRebaseWorkflows{flow: worktreeFlow()},
		Notifier:  mail,
		Rounds: &fakeRounds{
			rounds: []reviewstore.Round{{AgentID: "a1", SnapshotSHA: snapshot}},
			reviews: []reviewstore.Review{
				{
					AgentID:       "a1",
					AuthorKind:    reviewstore.AuthorAgent,
					AuthorAgentID: &finished,
					ReleasedAt:    &released,
				},
				{
					AgentID:       "a1",
					AuthorKind:    reviewstore.AuthorAgent,
					AuthorAgentID: &reviewer,
				},
			},
		},
	})

	_, err = service.Reset(t.Context(), "a1")
	require.NoError(t, err, "an agent's draft does not block a reset, since it reviews work being discarded on purpose")

	require.Contains(t, mail.sent, reviewer, "but the reviewer is told rather than left writing into a batch that is gone")
	assert.Contains(t, mail.sent[reviewer], "was reset")
	assert.Contains(t, mail.sent[reviewer], "Do not carry on writing")
	assert.Contains(t, mail.sent, "a1", "and the agent whose work went is told too")
	assert.NotContains(t, mail.sent, finished,
		"a reviewer that had already released is not told, since it had already delivered and has nothing left to write into")
}

// TestResetSweepsTheRoundNamespace covers a snapshot ref left behind by an earlier interrupted
// reset, which no row names any more and which nothing driven by rows could ever collect.
func TestResetSweepsTheRoundNamespace(t *testing.T) {
	t.Parallel()

	tree := buildResetWorktree(t)
	head, err := git.Run(t.Context(), tree, "rev-parse", "HEAD")
	require.NoError(t, err)

	// One ref an earlier run left with no row behind it, and one the rows do name.
	orphan := roundRef("a1", "0000000000000000000000000000000000000001")
	gitRun(t, tree, "update-ref", orphan, head)
	named := roundRef("a1", head)
	gitRun(t, tree, "update-ref", named, head)

	service := resetService(t, tree, &fakeRounds{rounds: []reviewstore.Round{{AgentID: "a1", SnapshotSHA: head}}})

	_, err = service.Reset(t.Context(), "a1")
	require.NoError(t, err)

	for _, ref := range []string{orphan, named} {
		_, err := git.Run(t.Context(), tree, "rev-parse", "--verify", ref)
		require.Error(t, err, "swept by prefix, so %s went whether or not a row named it", ref)
	}
}

// TestResetLeavesTheReplayParksAlone covers the undo copies a rebase promised the agent were
// kept, which a reset must not collect as a side effect of an unrelated key.
func TestResetLeavesTheReplayParksAlone(t *testing.T) {
	t.Parallel()

	tree := buildResetWorktree(t)
	head, err := git.Run(t.Context(), tree, "rev-parse", "HEAD")
	require.NoError(t, err)

	replayPark := "refs/weave/prerefresh/a1/1"
	gitRun(t, tree, "update-ref", replayPark, head)

	service := resetService(t, tree, nil)

	_, err = service.Reset(t.Context(), "a1")
	require.NoError(t, err)

	_, err = git.Run(t.Context(), tree, "rev-parse", "--verify", replayPark)
	require.NoError(t, err, "a rebase said this was kept, and a reset is not the key that takes it")
}

// TestResetRefusesWhenItCannotReadTheReviews covers the read that decides whether anybody has a
// draft, since proceeding on a failure would delete drafts with nobody to tell.
func TestResetRefusesWhenItCannotReadTheReviews(t *testing.T) {
	t.Parallel()

	tree := buildResetWorktree(t)
	service := resetService(t, tree, &fakeRounds{listErr: errors.New("the reviews could not be read")})

	_, err := service.Reset(t.Context(), "a1")
	require.ErrorContains(t, err, "could not be read")

	assert.Equal(t, "edited and not committed\n", readTree(t, tree, "tracked.txt"), "and nothing was discarded")
}

// TestResetMailNamesWhatTheWorkflowFurnishes covers the paths the clean spares on purpose, which
// an agent told only that the base is on disk would recreate over.
func TestResetMailNamesWhatTheWorkflowFurnishes(t *testing.T) {
	t.Parallel()

	body := resetMail(ResetResult{
		ResetPlan: ResetPlan{
			BaseRef: "origin/main",
			BaseSHA: "19802cfc353a",
			Keep:    []string{".env", "go.work.sum"},
		},
		ParkedRef: "refs/weave/discarded/a1/1",
		Kept:      []string{".env", "go.work.sum"},
	})

	assert.Contains(t, body, ".env, go.work.sum", "the furnishings are named rather than implied")
	assert.NotContains(t, body, "is the base and nothing else", "which is what it used to claim, and it was false")
}

// TestResetRefusesWhenItCannotReadTheWorkflow covers the read that decides which files survive
// the clean, since answering nothing would delete every furnishing and report success.
func TestResetRefusesWhenItCannotReadTheWorkflow(t *testing.T) {
	t.Parallel()

	tree := buildResetWorktree(t)
	writeTree(t, tree, "furnished.txt", "put here by the workflow\n")

	// Reset calls eligible, PlanReset calls it again, and only then does the plan read the
	// workflow for its keep list, so failing any earlier tests the eligibility refusal instead.
	service := NewRefreshService(NewRefreshServiceParams{
		Agents: &recordingRebaseAgents{agent: agentstore.Agent{
			ID: "a1", Name: "auth", WorkflowID: "w1", Cwd: tree, State: agentstore.StateIdle,
		}},
		Workflows: &failingPlanWorkflows{flow: worktreeFlow(), failAfter: 2},
	})

	_, err := service.Reset(t.Context(), "a1")
	require.ErrorContains(t, err, "could not be read")

	assert.Equal(t, "put here by the workflow\n", readTree(t, tree, "furnished.txt"),
		"nothing was cleaned, so no furnishing was lost to a read that failed")
}

// failingPlanWorkflows answers the first failAfter reads and fails the rest.
type failingPlanWorkflows struct {
	flow      workflowstore.Workflow
	reads     int
	failAfter int
}

func (s *failingPlanWorkflows) Get(_ context.Context, _ string) (*workflowstore.Workflow, error) {
	s.reads++
	if s.reads > s.failAfter {
		return nil, errors.New("the workflow could not be read")
	}
	flow := s.flow

	return &flow, nil
}

func (s *failingPlanWorkflows) GetIncludingArchived(ctx context.Context, id string) (*workflowstore.Workflow, error) {
	return s.Get(ctx, id)
}

// TestResetSweepsTheRoundNamespaceWithNoRowsLeft covers the state a previous reset leaves when
// it deleted the rows and then failed to release the refs, where nothing names them any more.
func TestResetSweepsTheRoundNamespaceWithNoRowsLeft(t *testing.T) {
	t.Parallel()

	tree := buildResetWorktree(t)
	head, err := git.Run(t.Context(), tree, "rev-parse", "HEAD")
	require.NoError(t, err)

	orphan := roundRef("a1", head)
	gitRun(t, tree, "update-ref", orphan, head)

	// No rows at all, which is what a failed release from an earlier reset leaves behind.
	service := resetService(t, tree, &fakeRounds{})

	_, err = service.Reset(t.Context(), "a1")
	require.NoError(t, err)

	_, err = git.Run(t.Context(), tree, "rev-parse", "--verify", orphan)
	require.Error(t, err, "the sweep runs whether or not a row is left to name anything")
}

// TestResetMailNamesANestedRepository covers the directory the clean spares, which an agent told
// only that the base is on disk would not know was still there.
func TestResetMailNamesANestedRepository(t *testing.T) {
	t.Parallel()

	body := resetMail(ResetResult{
		ResetPlan: ResetPlan{
			BaseRef: "origin/main",
			BaseSHA: "19802cfc353a",
			Nested:  []string{"vendored"},
		},
		ParkedRef: "refs/weave/discarded/a1/1",
	})

	assert.Contains(t, body, "repositories checked out inside your worktree: vendored")
}

// TestReleaseAgentRefsLeavesAWorktreeSomethingIsWorkingIn covers the race that would collect a
// reset's only copy between the park being written and the work being destroyed.
func TestReleaseAgentRefsLeavesAWorktreeSomethingIsWorkingIn(t *testing.T) {
	t.Parallel()

	tree := buildResetWorktree(t)
	head, err := git.Run(t.Context(), tree, "rev-parse", "HEAD")
	require.NoError(t, err)

	discard := buildDiscardRef("a1")
	gitRun(t, tree, "update-ref", discard, head)

	// The refresh holds the tree, which is the state a reset is in between its two steps.
	locks := NewTreeLocks()
	release, taken := locks.TryLock(tree)
	require.True(t, taken)

	service := NewLifecycleService(NewLifecycleServiceParams{Locks: locks})
	service.releaseAgentRefs(t.Context(), agentstore.Agent{
		ID:   "a1",
		Name: "auth",
		Cwd:  tree,
	})

	_, err = git.Run(t.Context(), tree, "rev-parse", "--verify", discard)
	require.NoError(t, err, "the only copy of the work a reset is about to destroy is still there")

	// And once the refresh is done with the tree, the same call collects it.
	release()
	service.releaseAgentRefs(t.Context(), agentstore.Agent{
		ID:   "a1",
		Name: "auth",
		Cwd:  tree,
	})

	_, err = git.Run(t.Context(), tree, "rev-parse", "--verify", discard)
	require.Error(t, err, "so the leak is closed rather than merely deferred")
}

// TestResetSparesARepositoryCheckedOutInside covers the service passing what it found to the
// clean, since detecting a nested repository and then removing it would be worse than neither.
func TestResetSparesARepositoryCheckedOutInside(t *testing.T) {
	t.Parallel()

	tree := buildResetWorktree(t)

	// Inside a directory the base tracks, so git lists its files individually rather than
	// collapsing the directory, which is the shape a clean would gut.
	vendor := filepath.Join(tree, "vendor")
	require.FileExists(t, filepath.Join(vendor, "README"), "the fixture must give the clone a tracked parent")
	gitRun(t, vendor, "init", "--quiet", "-b", "main", ".")
	gitRun(t, vendor, "config", "user.email", "t@example.com")
	gitRun(t, vendor, "config", "user.name", "t")
	writeTree(t, vendor, "lib.txt", "committed inside the clone\n")
	gitRun(t, vendor, "add", "lib.txt")
	gitRun(t, vendor, "commit", "--quiet", "-m", "nested")

	service := resetService(t, tree, nil)

	result, err := service.Reset(t.Context(), "a1")
	require.NoError(t, err)
	require.Equal(t, []string{"vendor"}, result.Nested, "the plan found it")

	assert.Equal(t, "committed inside the clone\n", readTree(t, vendor, "lib.txt"),
		"and the clean spared it, since the park holds no history of the clone")
	assert.NoFileExists(t, filepath.Join(tree, "scratch.txt"), "while the agent's own untracked file still goes")
}
