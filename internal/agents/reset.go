package agents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"weave/internal/git"
	"weave/internal/logging"
	agentstore "weave/internal/store/agent"
	reviewstore "weave/internal/store/review"
)

// Explainer: a reset discards the agent's work rather than replaying it, so the ordering is the
// whole safety argument. The park is written before anything is touched and a park that fails
// stops the reset, because the park is the only way back to a tracked change or a commit the
// reset is about to drop. Nothing untracked or ignored is removed at all, since a park cannot
// hold an ignored file and removing one would be the single step here with no recovery.

// ErrResetPark says the copy that makes a reset survivable could not be written.
var ErrResetPark = errors.New("the worktree could not be parked, so nothing was reset")

// roundReadDeleter is the slice of the review store a reset reads and clears.
type roundReadDeleter interface {
	ListRounds(ctx context.Context, agentID string) ([]reviewstore.Round, error)
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
}

// ResetResult is what a reset did.
type ResetResult struct {
	ResetPlan
	// ParkedRef holds the worktree as it stood before any of this.
	ParkedRef string
	// Rounds is how many review rounds were torn down with the work they described.
	Rounds int
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

	base := ""
	if flow, err := s.workflows.Get(ctx, agent.WorkflowID); err == nil {
		base = flow.Worktree.BaseBranch
	}

	ref, sha, err := git.FetchBase(ctx, agent.Cwd, base)
	if err != nil {
		return nil, err
	}

	commits, err := git.CountCommitsToReplay(ctx, agent.Cwd, ref)
	if err != nil {
		return nil, err
	}

	return &ResetPlan{
		BaseRef:        ref,
		BaseSHA:        sha,
		Branch:         state.Branch,
		Commits:        commits,
		TrackedChanges: state.TrackedChanges,
	}, nil
}

// Reset puts the agent's branch and tracked files back to the base, discarding its work.
func (s *RefreshService) Reset(ctx context.Context, agentID string) (*ResetResult, error) {
	agent, err := s.eligible(ctx, agentID)
	if err != nil {
		return nil, err
	}

	unlock, taken := s.locks.TryLock(agent.Cwd)
	if !taken {
		return nil, fmt.Errorf("%w: something else is working in %s", ErrRebaseBusy, agent.Cwd)
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

	result := &ResetResult{ResetPlan: *plan}

	// Parked before anything is destroyed, and a park that fails stops the reset rather than
	// being logged past, since it is the only way back to what is about to go.
	result.ParkedRef = buildParkRef(agentID)
	if _, err := git.Park(ctx, agent.Cwd, result.ParkedRef); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrResetPark, err)
	}

	if err := git.ResetToBase(ctx, agent.Cwd, plan.BaseRef); err != nil {
		return nil, err
	}

	result.Rounds = s.tearDownRounds(ctx, *agent)
	result.ClearedPullRequest = s.clearPullRequest(ctx, *agent)

	s.releaseOlderParks(ctx, *agent, result.ParkedRef)
	s.tellReset(ctx, *agent, *result)

	return result, nil
}

// tearDownRounds drops the review history, which describes work the reset has just discarded,
// and answers with how many rounds went.
func (s *RefreshService) tearDownRounds(ctx context.Context, agent agentstore.Agent) int {
	if s.rounds == nil {
		return 0
	}

	rounds, err := s.rounds.ListRounds(ctx, agent.ID)
	if err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to read the rounds of a reset agent",
			append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)

		return 0
	}

	// Released before the rows go, since the row is what names the ref to release.
	for _, round := range rounds {
		if err := git.ReleaseRef(ctx, agent.Cwd, roundRef(agent.ID, round.SnapshotSHA)); err != nil {
			logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to release the snapshot of a round",
				append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)
		}
	}

	if err := s.rounds.DeleteRounds(ctx, agent.ID); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to delete the rounds of a reset agent",
			append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)

		return 0
	}

	return len(rounds)
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
	if result.ClearedPullRequest {
		body.WriteString("\n\nYour recorded pull request has been cleared. It no longer tracks anything this branch carries.")
	}

	body.WriteString("\n\nNothing you had was deleted outright. A copy of the worktree from before this ran is kept at " +
		result.ParkedRef + ". It holds your commits and anything you had not committed. " +
		"Untracked and ignored files were left where they are.")

	return body.String()
}
