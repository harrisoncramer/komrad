package agents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"weave/internal/git"
	"weave/internal/logging"
	agentstore "weave/internal/store/agent"
	reviewstore "weave/internal/store/review"
)

// Explainer: everything the reset acts on is read in the plan and carried, rather than read
// again beside the step that uses it. A second read can fail where the first succeeded, and both
// of these decide what is destroyed: the drafts decide whether a person's review blocks the
// reset, and the keep list decides which files survive the clean. Answering nothing on a failed
// read means deleting exactly what the read was there to protect.

// Explainer: the unreleased reviews are read once, in the plan, and carried to the teardown
// rather than read again beside it. A second read could fail where the first succeeded, and the
// reset would then delete drafts it had already decided were safe to delete, with nobody left
// to tell that they had gone.

// Explainer: a reset discards the agent's work rather than replaying it, so the ordering is the
// whole safety argument. The park is written before anything is touched and a park that fails
// stops the reset, because the park is the only way back to a tracked change or a commit the
// reset is about to drop. Nothing untracked or ignored is removed at all, since a park cannot
// hold an ignored file and removing one would be the single step here with no recovery.

// ErrResetPark says the copy that makes a reset survivable could not be written.
var ErrResetPark = errors.New("the worktree could not be parked, so nothing was reset")

// ErrResetOpenReview says somebody is part-way through writing a review the teardown would take.
var ErrResetOpenReview = errors.New("somebody is part-way through writing a review of this agent, and a reset would delete it along with its comments, so release or discard that review first")

// ErrResetShadowed says the base tracks a path this worktree holds as an ignored file.
var ErrResetShadowed = errors.New("the base tracks these paths, which this worktree holds as ignored files that a reset would overwrite and no copy could give back")

// roundReadDeleter is the slice of the review store a reset reads and clears.
type roundReadDeleter interface {
	ListRounds(ctx context.Context, agentID string) ([]reviewstore.Round, error)
	ListReviews(ctx context.Context, agentID string) ([]reviewstore.Review, error)
	DeleteRounds(ctx context.Context, agentID string) error
}

// ResetPlan is what a reset would discard.
type ResetPlan struct {
	// BaseRef is what the branch would be put back to, and BaseSHA the commit it named.
	BaseRef string
	BaseSHA string
	Branch  string
	// Commits is how many of the agent's own commits would leave the branch.
	Commits int
	// TrackedChanges says the worktree holds edits a reset would discard.
	TrackedChanges bool
	// Drafts are the unreleased reviews the round teardown would take, read here so that the
	// reset acts on the same reading it was refused against.
	Drafts []reviewstore.Review
	// Keep are the paths the workflow furnishes, which the clean has to spare because nothing
	// puts them back, read here for the same reason the drafts are.
	Keep []string
}

// ResetResult is what a reset did.
type ResetResult struct {
	ResetPlan
	// ParkedRef holds the worktree as it stood before any of this.
	ParkedRef string
	// Rounds is how many review rounds were torn down with the work they described.
	Rounds int
	// Kept are the furnishings the clean spared, which the agent has to be told about or it
	// will read "nothing but the base is on disk" and recreate a file that is already there.
	Kept []string
	// StaleReviewHistory says the reset stands but the review history could not be cleared, so
	// what the review screen shows describes work that is gone.
	StaleReviewHistory bool
	// ClearedPullRequest says a recorded pull request was dropped.
	ClearedPullRequest bool
}

// PlanReset reads what a reset would discard without touching anything.
func (s *RefreshService) PlanReset(ctx context.Context, agentID string) (*ResetPlan, error) {
	agent, err := s.eligible(ctx, agentID)
	if err != nil {
		return nil, err
	}

	state, err := git.ReadTreeState(ctx, agent.Cwd)
	if err != nil {
		return nil, err
	}
	if err := refuseEitherWay(state); err != nil {
		return nil, err
	}

	// Read once and refused when it fails, since a read that quietly answered nothing would
	// delete every furnishing the workflow puts there and nothing puts back.
	flow, err := s.workflows.Get(ctx, agent.WorkflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to read the workflow of agent %q: %w", agent.Name, err)
	}

	base := flow.Worktree.BaseBranch
	keep := append(append([]string{}, flow.Worktree.CopyPaths...), flow.Worktree.SymlinkPaths...)

	ref, sha, err := git.FetchBase(ctx, agent.Cwd, base)
	if err != nil {
		return nil, err
	}

	commits, err := git.CountCommitsToReplay(ctx, agent.Cwd, ref)
	if err != nil {
		return nil, err
	}

	// The rounds go with the reset, and the cascade takes an unreleased review and every thread
	// under it, none of which the park holds.
	drafts, err := s.readDrafts(ctx, agentID)
	if err != nil {
		return nil, err
	}

	// A park is built with git add -A and so holds no ignored file, and a reset writes every
	// path the base tracks, so a path that is both is the one thing here with no way back.
	shadowed, err := git.FindShadowedPaths(ctx, agent.Cwd, ref)
	if err != nil {
		return nil, err
	}
	if len(shadowed) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrResetShadowed, strings.Join(shadowed, ", "))
	}

	return &ResetPlan{
		BaseRef:        ref,
		BaseSHA:        sha,
		Branch:         state.Branch,
		Commits:        commits,
		TrackedChanges: state.TrackedChanges,
		Drafts:         drafts,
		Keep:           keep,
	}, nil
}

// buildDiscardRef names where a reset keeps what it destroyed, in a namespace of its own so
// that a later refresh does not collect the only copy of work nothing else holds.
func buildDiscardRef(agentID string) string {
	return fmt.Sprintf("%s%d", discardRefPrefix(agentID), time.Now().UnixNano())
}

// discardRefPrefix is the namespace a reset's copies are held under, swept only when the agent
// itself is destroyed.
func discardRefPrefix(agentID string) string {
	return "refs/weave/discarded/" + agentID + "/"
}

// readDrafts are the unreleased reviews the teardown would take, refusing when one is a person's.
func (s *RefreshService) readDrafts(ctx context.Context, agentID string) ([]reviewstore.Review, error) {
	if s.rounds == nil {
		return nil, nil
	}

	reviews, err := s.rounds.ListReviews(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to read the reviews of agent %q: %w", agentID, err)
	}

	drafts := []reviewstore.Review{}
	for _, review := range reviews {
		if review.ReleasedAt != nil {
			continue
		}
		if review.AuthorKind == reviewstore.AuthorHuman {
			return nil, ErrResetOpenReview
		}
		drafts = append(drafts, review)
	}

	return drafts, nil
}

// tellDraftingReviewers says so to the agents whose drafts the teardown took, since an agent
// that is not told keeps writing into a batch that no longer exists.
func (s *RefreshService) tellDraftingReviewers(ctx context.Context, agent agentstore.Agent, reviews []reviewstore.Review) {
	if s.notifier == nil {
		return
	}

	for _, review := range reviews {
		// Guards the dereference rather than filtering anything reachable: an unreleased
		// human review refuses the reset before this runs.
		if review.AuthorAgentID == nil {
			continue
		}

		body := fmt.Sprintf("The worktree of %s was reset, so the work you were reviewing has been discarded and "+
			"the review you had not released went with it.\n\nNothing you wrote reached %s. Do not carry on writing "+
			"into it. Wait to be told whether the next attempt wants reviewing.", agent.Name, agent.Name)
		if _, err := s.notifier.SendSystemMail(ctx, *review.AuthorAgentID, body); err != nil {
			logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to tell a reviewer its draft went with a reset",
				append([]slog.Attr{slog.String(logging.AgentID, *review.AuthorAgentID)}, logging.Err(err)...)...)
		}
	}
}

// Reset puts the agent's branch and tracked files back to the base, discarding its work.
func (s *RefreshService) Reset(ctx context.Context, agentID string) (*ResetResult, error) {
	agent, err := s.eligible(ctx, agentID)
	if err != nil {
		return nil, err
	}

	unlock, taken := s.locks.TryLock(agent.Cwd)
	if !taken {
		return nil, fmt.Errorf("%w: something else is working in %s", ErrRefreshBusy, agent.Cwd)
	}
	defer unlock()

	plan, err := s.PlanReset(ctx, agentID)
	if err != nil {
		return nil, err
	}

	if s.interrupter != nil && agent.Pane != "" {
		if err := s.interrupter.Interrupt(ctx, agentID); err != nil {
			return nil, fmt.Errorf("failed to interrupt agent %q before resetting it: %w", agent.Name, err)
		}
	}

	result := &ResetResult{ResetPlan: *plan, Kept: plan.Keep}

	// Parked before anything is destroyed, and a park that fails stops the reset rather than
	// being logged past, since it is the only way back to what is about to go.
	result.ParkedRef = buildDiscardRef(agentID)
	if _, err := git.Park(ctx, agent.Cwd, result.ParkedRef); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrResetPark, err)
	}

	if err := git.ResetToBase(ctx, git.ResetToBaseParams{
		Dir:  agent.Cwd,
		Ref:  plan.BaseRef,
		Keep: plan.Keep,
	}); err != nil {
		return nil, err
	}

	rounds, roundsErr := s.tearDownRounds(ctx, *agent)
	result.Rounds = rounds
	if roundsErr != nil {
		result.StaleReviewHistory = true
	}
	result.ClearedPullRequest = s.clearPullRequest(ctx, *agent)

	if roundsErr == nil {
		s.tellDraftingReviewers(ctx, *agent, plan.Drafts)
	}

	// releaseOlderParks is deliberately not called, since sweeping the replay namespace here
	// would delete undo copies a rebase had told the agent were kept.
	s.tellReset(ctx, *agent, *result)

	return result, nil
}

// tearDownRounds drops the review history, which describes work the reset has just discarded,
// and answers with how many rounds went.
func (s *RefreshService) tearDownRounds(ctx context.Context, agent agentstore.Agent) (int, error) {
	if s.rounds == nil {
		return 0, nil
	}

	rounds, err := s.rounds.ListRounds(ctx, agent.ID)
	if err != nil {
		return 0, fmt.Errorf("failed to read the rounds of agent %q: %w", agent.ID, err)
	}
	// The rows go first, so a delete that fails leaves both the rows and the refs they name,
	// which is a state somebody can read and try again from.
	if len(rounds) > 0 {
		if err := s.rounds.DeleteRounds(ctx, agent.ID); err != nil {
			return 0, fmt.Errorf("failed to delete the rounds of agent %q: %w", agent.ID, err)
		}
	}

	// Swept by prefix rather than by row, so a release that fails cannot leave a ref no row
	// names and nothing can ever collect, and so an earlier interrupted reset is cleared too.
	if err := git.ReleaseRefsUnder(ctx, agent.Cwd, roundRefPrefix(agent.ID), ""); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to release the snapshots of a reset agent",
			append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)
	}

	return len(rounds), nil
}

// tellReset says what happened in the agent's mailbox.
func (s *RefreshService) tellReset(ctx context.Context, agent agentstore.Agent, result ResetResult) {
	if s.notifier == nil {
		return
	}

	if _, err := s.notifier.SendSystemMail(ctx, agent.ID, resetMail(result)); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to tell an agent its worktree was reset",
			append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)
	}
}

// resetMail is what the agent is told, which has to be read by something that will act on it.
func resetMail(result ResetResult) string {
	var body strings.Builder

	fmt.Fprintf(&body, "Your worktree was reset to %s (%s).\n\n", result.BaseRef, short(result.BaseSHA))
	body.WriteString("Somebody decided the work in it was not worth carrying forward. " +
		"Your tracked files now match the base exactly.")
	if result.Commits > 0 {
		fmt.Fprintf(&body, " The %d commits you had made are no longer on the branch.", result.Commits)
	}
	body.WriteString("\n\nRead your files again rather than trusting what you remember, because they have changed under you. " +
		"Your conversation is intact, so you still know what you were asked to do. Start again from here.")

	if result.Rounds > 0 {
		fmt.Fprintf(&body, "\n\nThe %d review rounds taken of you have been torn down, along with their comments. "+
			"They described work that no longer exists.", result.Rounds)
	}
	if result.StaleReviewHistory {
		body.WriteString("\n\nThe review rounds taken of you could not be cleared, so whatever the review screen " +
			"shows of you describes work that is gone. Say so rather than answering it.")
	}
	if result.ClearedPullRequest {
		body.WriteString("\n\nYour recorded pull request has been cleared. It no longer tracks anything this branch carries.")
	}

	body.WriteString("\n\nThe files you had created but never committed have been removed too. " +
		"Files git was ignoring, such as configuration and build output, were left alone")
	if len(result.Kept) > 0 {
		fmt.Fprintf(&body, ", and so were the paths your workflow furnishes: %s", strings.Join(result.Kept, ", "))
	}
	body.WriteString(". Everything else on disk is the base.")
	body.WriteString("\n\nNothing was deleted outright. A copy of the worktree from before this ran is kept at " +
		result.ParkedRef + ". It holds your commits, anything you had not committed, and the files you had created. " +
		"Nothing collects it, so it will be there when you look.")

	return body.String()
}
