package checkouts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"komrad/internal/events"
	"komrad/internal/fail"
	"komrad/internal/git"
	"komrad/internal/logging"
	agentstore "komrad/internal/store/agent"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

var (
	// ErrRebaseAdopted says the branch belongs to whoever made it.
	ErrRebaseAdopted = fail.Precondition("this agent is on a branch komrad did not cut, so its history is not komrad's to rewrite")

	// ErrRebaseDirty says the checkout holds tracked changes.
	ErrRebaseDirty = fail.Precondition("the worktree holds tracked changes, so commit or discard them first")

	// ErrRebaseInProgress says something already owns the index.
	ErrRebaseInProgress = fail.Precondition("the worktree is already part-way through a merge or a rebase, so finish or abort that first")

	// ErrRebaseDetached says the checkout is on no branch, so a rebase would move nothing.
	ErrRebaseDetached = fail.Precondition("the worktree is on no branch, so a rebase would move nothing")

	// ErrRebaseShallow says the clone cannot answer what the branch and the base share.
	ErrRebaseShallow = fail.Precondition("the clone is shallow, so what this branch shares with the base cannot be read")

	// ErrRebaseHiddenPaths says the index is holding a path's content back on purpose.
	ErrRebaseHiddenPaths = fail.Precondition("these paths are marked assume-unchanged or skip-worktree, so their contents cannot be read back if the move goes wrong")
)

// RebasePlan is what a rebase would do.
type RebasePlan struct {
	// BaseRef is what the branch would move onto, and BaseSHA the commit it named when read.
	BaseRef string `json:"base_ref"`
	BaseSHA string `json:"base_sha"`
	// Commits is how many of the agent's own commits would be replayed.
	Commits int    `json:"commits"`
	Branch  string `json:"branch"`
	// Cutoff is the newest of the agent's own commits the base already holds whole, which is
	// where the replay starts, empty when the base holds none of them.
	Cutoff string `json:"cutoff"`
	// Landed is how many of its commits the base already holds, and Replay how many would move.
	Landed int `json:"landed"`
	Replay int `json:"replay"`
	// Merges is how many merge commits a rebase would discard.
	Merges int `json:"merges"`
	// ConflictPaths are the paths a merge of the base into the branch could not settle.
	ConflictPaths []string `json:"conflict_paths"`
	// UpToDate says the branch already holds the base.
	UpToDate bool `json:"up_to_date"`
}

// RebaseResult is what a rebase did.
type RebaseResult struct {
	RebasePlan `json:"plan"`
	// Conflicted says the rebase stopped part-way and is waiting for the agent.
	Conflicted bool `json:"conflicted"`
	// Moved says the branch reached the base without replaying anything, which is what a branch
	// whose work has entirely landed gets.
	Moved bool `json:"moved"`
	// ParkedRef holds the checkout as it stood before any of this, which is what an undo reads.
	ParkedRef string `json:"parked_ref"`
	// Interrupted says a turn was cut so the rebase could run.
	Interrupted bool `json:"interrupted"`
}

// PlanRebase reads what a rebase would do without touching anything.
func (s *Service) PlanRebase(ctx context.Context, agentID string) (*RebasePlan, error) {
	target, err := s.rebaseEligible(ctx, agentID)
	if err != nil {
		return nil, err
	}

	return s.plan(ctx, *target)
}

// rebaseEligible answers the agent when a rebase is its to have, which a branch komrad did not
// cut never is.
func (s *Service) rebaseEligible(ctx context.Context, agentID string) (*checkoutTarget, error) {
	target, err := s.eligible(ctx, agentID)
	if err != nil {
		return nil, err
	}

	if target.agent.AdoptedBranch != "" {
		return nil, fmt.Errorf("%w: %s", ErrRebaseAdopted, target.agent.AdoptedBranch)
	}

	return target, nil
}

// readTreeState reads the checkout and refuses one a rebase may not start on.
func (s *Service) readTreeState(ctx context.Context, agent agentstore.Agent) (git.TreeState, error) {
	state, err := s.rebaser.ReadTreeState(ctx, agent.Cwd)
	if err != nil {
		return git.TreeState{}, err
	}
	if err := refuseTreeState(state); err != nil {
		return git.TreeState{}, err
	}

	return state, nil
}

// plan reads the checkout and the base and says what a rebase would do to them.
func (s *Service) plan(ctx context.Context, target checkoutTarget) (*RebasePlan, error) {
	agent, flow := *target.agent, target.workflow
	state, err := s.readTreeState(ctx, agent)
	if err != nil {
		return nil, err
	}

	ref, sha, err := s.rebaser.FetchBase(ctx, agent.Cwd, flow.Worktree.BaseBranch)
	if err != nil {
		return nil, err
	}

	commits, err := s.rebaser.CountCommitsToReplay(ctx, agent.Cwd, ref)
	if err != nil {
		return nil, err
	}

	prediction, err := s.rebaser.PredictRebase(ctx, agent.Cwd, ref)
	if err != nil {
		return nil, err
	}

	return &RebasePlan{
		BaseRef:       ref,
		BaseSHA:       sha,
		Commits:       commits,
		Branch:        state.Branch,
		Cutoff:        prediction.Cutoff,
		Landed:        prediction.Landed,
		Replay:        prediction.Replay,
		Merges:        prediction.Merges,
		ConflictPaths: prediction.ConflictPaths,
		UpToDate:      prediction.Contained,
	}, nil
}

// Rebase moves the agent's branch onto the latest base in place.
func (s *Service) Rebase(ctx context.Context, agentID string) (*RebaseResult, error) {
	target, err := s.rebaseEligible(ctx, agentID)
	if err != nil {
		return nil, err
	}
	agent := target.agent

	unlock, err := s.lockTree(agent.Cwd)
	if err != nil {
		return nil, err
	}
	defer unlock()

	plan, err := s.plan(ctx, *target)
	if err != nil {
		return nil, err
	}
	if plan.UpToDate {
		return &RebaseResult{RebasePlan: *plan}, nil
	}

	interrupted, err := s.settle(ctx, *agent)
	if err != nil {
		return nil, err
	}

	result, err := s.rebase(ctx, *target)
	if err != nil {
		if interrupted {
			s.tellCut(ctx, *agent, err)
		}

		return nil, err
	}
	result.Interrupted = interrupted

	s.tell(ctx, *agent, *result)

	return result, nil
}

// rebase plans the move and makes it.
func (s *Service) rebase(ctx context.Context, target checkoutTarget) (*RebaseResult, error) {
	agent := *target.agent
	if _, err := s.read(ctx, agent.ID); err != nil {
		return nil, err
	}

	plan, err := s.plan(ctx, target)
	if err != nil {
		return nil, err
	}

	result := &RebaseResult{RebasePlan: *plan}

	result.ParkedRef = buildParkRef(parkRebase, agent.ID)
	if _, err := s.rebaser.Park(ctx, agent.Cwd, result.ParkedRef); err != nil {
		return nil, err
	}
	defer s.checkoutSync.Wake()

	if err := s.move(ctx, agent.Cwd, result); err != nil {
		return nil, err
	}

	s.releaseOlderParks(ctx, agent, result.ParkedRef)

	if result.Conflicted {
		s.recordStopped(ctx, agent)
	}

	return result, nil
}

// releaseOlderParks drops the parks this agent left behind before this one.
func (s *Service) releaseOlderParks(ctx context.Context, agent agentstore.Agent, keep string) {
	prefix := fmt.Sprintf("refs/komrad/%s/%s/", parkRebase, agent.ID)
	if err := s.rebaser.ReleaseRefsUnder(ctx, agent.Cwd, prefix, keep); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to release the older parks of a rebased agent",
			append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)
	}
}

// move brings the branch onto the base, replaying its commits from the cutoff when the base
// already holds some of them.
func (s *Service) move(ctx context.Context, dir string, result *RebaseResult) error {
	replay := func() error { return s.rebaser.Rebase(ctx, dir, result.BaseRef) }
	if result.Cutoff != "" {
		replay = func() error { return s.rebaser.RebaseOnto(ctx, dir, result.BaseRef, result.Cutoff) }
	}

	err := replay()
	switch {
	case errors.Is(err, git.ErrRebaseConflicted):
		result.Conflicted = true
	case err != nil:
		return err
	}

	result.Moved = result.Landed > 0 && result.Replay == 0 && !result.Conflicted

	return nil
}

// recordStopped puts the stopped rebase on the agent's row.
func (s *Service) recordStopped(ctx context.Context, agent agentstore.Agent) {
	if err := s.agents.SetSequenceByID(ctx, agent.ID, agentstore.SequenceRebase); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to record that an agent's rebase stopped part-way",
			append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)
	}
}

// tell says what happened in the agent's mailbox.
func (s *Service) tell(ctx context.Context, agent agentstore.Agent, result RebaseResult) {
	if err := s.record(ctx, agent.ID, rebaseOutcome{Result: &result}); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to tell an agent its worktree was rebased",
			append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)
	}
}

// tellCut says in the agent's mailbox that its turn was cut for a rebase that then failed.
func (s *Service) tellCut(ctx context.Context, agent agentstore.Agent, failure error) {
	if err := s.record(ctx, agent.ID, rebaseOutcome{Failure: failure.Error()}); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to tell an agent its turn was cut for nothing",
			append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)
	}
}

// cutMail is what an agent is told when it was stopped for a rebase that did not go through.
func cutMail(failure string) string {
	return fmt.Sprintf("Your turn was interrupted so that your worktree could be rebased onto its base, "+
		"but the rebase failed before it finished: %s\n\n"+
		"Check git status before carrying on, then pick up where you were.", failure)
}

// rebaseMail is what the agent is told, which turns on what became of its branch rather than on
// what was asked for.
func rebaseMail(result RebaseResult) string {
	var body string
	switch {
	case result.Conflicted:
		body = conflictedMail(result)
	case result.Moved:
		body = movedMail(result)
	default:
		body = replayedMail(result)
	}

	return interruptedNote(result) + body + carryOnNote(result)
}

// interruptedNote says the agent's turn was cut for the rebase.
func interruptedNote(result RebaseResult) string {
	if !result.Interrupted {
		return ""
	}

	return "Your turn was interrupted so that your worktree could be rebased onto its base.\n\n"
}

// carryOnNote tells an agent whose turn was cut to pick it back up.
func carryOnNote(result RebaseResult) string {
	if !result.Interrupted || result.Conflicted {
		return ""
	}

	return "\n\nRead your files again, then pick up where you were."
}

// parkNote says where the checkout as it stood beforehand is kept, which is the only way back
// to a commit the replay dropped or a file that was never committed.
func parkNote(result RebaseResult) string {
	if result.ParkedRef == "" {
		return ""
	}

	return "\n\nA copy of the worktree from before this ran is kept at " + result.ParkedRef +
		". It holds anything you had not committed, and every commit that is no longer on the branch."
}

// conflictedMail is a rebase stopped part-way, which is the agent's to settle and nobody else's.
func conflictedMail(result RebaseResult) string {
	var body strings.Builder

	fmt.Fprintf(&body, "Your worktree was being rebased onto %s (%s). The rebase hit conflicts and stopped part way, so it is still in progress.\n\n",
		result.BaseRef, git.ShortSHA(result.BaseSHA))
	body.WriteString("All your commits are still there. Your branch has not moved. " +
		"Do not resolve the conflicts and do not abort the rebase on your own. " +
		"Say what state you are in and wait to be told what to do. " +
		"The person who pressed the key decides whether this work is worth carrying forward.")

	body.WriteString("\n\nSay also whether you have a pull request open on this branch. " +
		"Finishing the rebase rewrites the commits it was opened from. " +
		"That invalidates an open pull request. It means nothing for one that has already merged.")
	body.WriteString(parkNote(result))

	return body.String()
}

// movedMail is a branch that reached the base without replaying anything, which is what a
// branch whose work has entirely landed gets.
func movedMail(result RebaseResult) string {
	var body strings.Builder

	fmt.Fprintf(&body, "Your branch is now on %s (%s).\n\n", result.BaseRef, git.ShortSHA(result.BaseSHA))
	body.WriteString("Your work had already been merged into the base. " +
		"There was nothing left to replay, so your branch was moved straight onto it.\n\n")
	body.WriteString("Nothing was lost. Your own commits are no longer on the branch and the base's history " +
		"has taken their place. Start your next piece of work from here.")
	body.WriteString("\n\nAny pull request opened from this branch no longer tracks anything it carries.")
	body.WriteString(parkNote(result))

	return body.String()
}

// replayedMail is the ordinary rebase, which moved the agent's files under it.
func replayedMail(result RebaseResult) string {
	var body strings.Builder

	if result.Replay == 0 {
		fmt.Fprintf(&body, "Your worktree is up to date with %s (%s). You had no commits of your own to replay.\n\n",
			result.BaseRef, git.ShortSHA(result.BaseSHA))
	} else {
		fmt.Fprintf(&body, "Your worktree was rebased onto %s (%s), replaying %d of your own commits.\n\n",
			result.BaseRef, git.ShortSHA(result.BaseSHA), result.Replay)
	}
	body.WriteString("Your files may have changed under you. Read them again rather than trusting what you remember. ")
	if result.Landed > 0 {
		fmt.Fprintf(&body, "%d of your commits are no longer on the branch. "+
			"The base already held those changes, so replaying them would have collided with themselves. ", result.Landed)
	}
	body.WriteString("Your branch is still the same branch.")

	body.WriteString("\n\nThe rebase rewrote the commits any pull request on this branch was opened from, " +
		"so do not assume one you opened earlier still tracks your work.")
	body.WriteString(parkNote(result))

	return body.String()
}

// refuseTreeState is what the checkout itself has to say before a rebase may start.
func refuseTreeState(state git.TreeState) error {
	switch {
	case state.InProgress:
		return ErrRebaseInProgress
	case state.Branch == "":
		return ErrRebaseDetached
	case state.Shallow:
		return ErrRebaseShallow
	case len(state.HiddenPaths) > 0:
		return fmt.Errorf("%w: %s", ErrRebaseHiddenPaths, strings.Join(state.HiddenPaths, ", "))
	case state.TrackedChanges:
		return ErrRebaseDirty
	}

	return nil
}

func (s *Service) record(ctx context.Context, agentID string, outcome rebaseOutcome) error {
	raw, err := json.Marshal(outcome)
	if err != nil {
		return fmt.Errorf("failed to encode the rebase of agent %q: %w", agentID, err)
	}

	event, err := events.NewEvent(Rebased{AgentID: agentID})
	if err != nil {
		return err
	}

	return s.events.InsertManyTx(ctx, func(tx pgx.Tx) ([]river.JobArgs, error) {
		if err := agentstore.NewAgentStore(tx).SetLastRebaseByID(ctx, agentID, raw); err != nil {
			return nil, err
		}

		return []river.JobArgs{*event}, nil
	})
}
