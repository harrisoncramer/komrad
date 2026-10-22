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
	taskstore "komrad/internal/store/task"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

var (
	// errRebaseAdopted says the branch belongs to whoever made it.
	errRebaseAdopted = fail.Precondition("this task is on a branch komrad did not cut, so its history is not komrad's to rewrite")

	// ErrRebaseDirty says the checkout holds tracked changes.
	ErrRebaseDirty = fail.Precondition("the worktree holds tracked changes, so commit or discard them first")

	// errRebaseInProgress says something already owns the index.
	errRebaseInProgress = fail.Precondition("the worktree is already part-way through a merge or a rebase, so finish or abort that first")

	// errRebaseDetached says the checkout is on no branch, so a rebase would move nothing.
	errRebaseDetached = fail.Precondition("the worktree is on no branch, so a rebase would move nothing")

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
	// OntoBase says the rebase re-targeted a child onto the workflow's base branch, away from its parent.
	OntoBase bool `json:"onto_base"`
}

// PlanRebase reads what a rebase would do without touching anything.
func (s *Service) PlanRebase(ctx context.Context, params RebaseParams) (*RebasePlan, error) {
	target, err := s.rebaseEligible(ctx, params)
	if err != nil {
		return nil, err
	}

	return s.planRebase(ctx, *target)
}

// rebaseEligible answers the task when a rebase is its to have, which a branch komrad did not
// cut never is.
func (s *Service) rebaseEligible(ctx context.Context, params RebaseParams) (*checkoutTarget, error) {
	target, err := s.eligible(ctx, params.Task)
	if err != nil {
		return nil, err
	}
	target.ontoBase = params.OntoBase

	if adopted := target.task.Checkout.AdoptedBranch; adopted != "" {
		return nil, fmt.Errorf("%w: %s", errRebaseAdopted, adopted)
	}

	return target, nil
}

// readRebaseTreeState reads the checkout and refuses one a rebase may not start on.
func (s *Service) readRebaseTreeState(ctx context.Context, dir string) (git.TreeState, error) {
	state, err := s.git.ReadTreeState(ctx, dir)
	if err != nil {
		return git.TreeState{}, err
	}
	if err := refuseTreeState(state); err != nil {
		return git.TreeState{}, err
	}

	return state, nil
}

// planRebase reads the checkout and the base and says what a rebase would do to them.
func (s *Service) planRebase(ctx context.Context, target checkoutTarget) (*RebasePlan, error) {
	dir := target.dir()
	state, err := s.readRebaseTreeState(ctx, dir)
	if err != nil {
		return nil, err
	}

	base, err := s.readRebaseBase(ctx, target)
	if err != nil {
		return nil, err
	}

	ref, sha, err := s.git.FetchBase(ctx, dir, base)
	if err != nil {
		return nil, err
	}

	commits, err := s.git.CountCommitsToReplay(ctx, dir, ref)
	if err != nil {
		return nil, err
	}

	prediction, err := s.git.PredictRebase(ctx, dir, ref)
	if err != nil {
		return nil, err
	}

	cutoff := prediction.Cutoff
	if parent := target.task.Parent; parent != nil {
		cutoff, err = s.readForkPointCutoff(ctx, dir, parent.ForkPoint, prediction.Cutoff)
		if err != nil {
			return nil, err
		}
	}

	return &RebasePlan{
		BaseRef:       ref,
		BaseSHA:       sha,
		Commits:       commits,
		Branch:        state.Branch,
		Cutoff:        cutoff,
		Landed:        prediction.Landed,
		Replay:        prediction.Replay,
		Merges:        prediction.Merges,
		ConflictPaths: prediction.ConflictPaths,
		UpToDate:      prediction.Contained,
	}, nil
}

// readRebaseBase is the branch a task rebases onto: its parent's branch while it has a parent, and the workflow's base otherwise.
func (s *Service) readRebaseBase(ctx context.Context, target checkoutTarget) (string, error) {
	if target.task.Parent == nil || target.ontoBase {
		return target.workflow.Worktree.BaseBranch, nil
	}

	parent, err := s.tasks.Get(ctx, target.task.Parent.TaskID)
	if err != nil {
		return "", err
	}

	return parent.GetBranch(), nil
}

// RebaseParams names the task to rebase and where it moves.
type RebaseParams struct {
	// Task is the task to rebase, by id or slug.
	Task string
	// OntoBase re-targets a child onto the workflow's base branch, rebasing only its own commits, and clears its parent.
	OntoBase bool
}

// Rebase moves the task's branch onto the latest base in place.
func (s *Service) Rebase(ctx context.Context, params RebaseParams) (*RebaseResult, error) {
	target, err := s.rebaseEligible(ctx, params)
	if err != nil {
		return nil, err
	}

	unlock, err := s.lockTree(target.dir())
	if err != nil {
		return nil, err
	}
	defer unlock()

	plan, err := s.planRebase(ctx, *target)
	if err != nil {
		return nil, err
	}
	if plan.UpToDate {
		return &RebaseResult{RebasePlan: *plan}, nil
	}

	if err := refuseWriterNotAtRest(*target); err != nil {
		return nil, err
	}

	result, err := s.rebase(ctx, *target)
	if err != nil {
		return nil, err
	}

	s.tellRebased(ctx, *target, *result)

	return result, nil
}

// rebase plans the move and makes it.
func (s *Service) rebase(ctx context.Context, target checkoutTarget) (*RebaseResult, error) {
	if err := s.recheckWriter(ctx, target); err != nil {
		return nil, err
	}

	plan, err := s.planRebase(ctx, target)
	if err != nil {
		return nil, err
	}

	result := &RebaseResult{RebasePlan: *plan, OntoBase: target.ontoBase && target.task.Parent != nil}

	result.ParkedRef = buildParkRef(parkRebase, target.task.ID)
	if _, err := s.git.Park(ctx, target.dir(), result.ParkedRef); err != nil {
		return nil, err
	}
	defer s.checkoutSync.Wake()

	if err := s.moveRebase(ctx, target.dir(), result); err != nil {
		return nil, err
	}

	if result.OntoBase && !result.Conflicted {
		if err := s.leaveParent(ctx, *target.task); err != nil {
			return nil, err
		}
	}

	s.releaseOlderParks(ctx, *target.task, result.ParkedRef)

	if result.Conflicted {
		s.recordRebaseStopped(ctx, *target.task)
	}

	return result, nil
}

// releaseOlderParks drops the parks this task's rebases left behind before this one.
func (s *Service) releaseOlderParks(ctx context.Context, task taskstore.Task, keep string) {
	prefix := fmt.Sprintf("refs/komrad/%s/%s/", parkRebase, task.ID)
	if err := s.git.ReleaseRefsUnder(ctx, task.Checkout.Cwd, prefix, keep); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to release the older parks of a rebased task",
			append([]slog.Attr{slog.String(logging.TaskID, task.ID)}, logging.Err(err)...)...)
	}
}

// moveRebase brings the branch onto the base, replaying its commits from the cutoff when the base
// already holds some of them.
func (s *Service) moveRebase(ctx context.Context, dir string, result *RebaseResult) error {
	replay := func() error { return s.git.Rebase(ctx, dir, result.BaseRef) }
	if result.Cutoff != "" {
		replay = func() error { return s.git.RebaseOnto(ctx, dir, result.BaseRef, result.Cutoff) }
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

// recordRebaseStopped puts the stopped rebase on the task.
func (s *Service) recordRebaseStopped(ctx context.Context, task taskstore.Task) {
	if err := s.tasks.SetSequence(ctx, task.ID, taskstore.SequenceRebase); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to record that a task's rebase stopped part-way",
			append([]slog.Attr{slog.String(logging.TaskID, task.ID)}, logging.Err(err)...)...)
	}
}

// tellRebased records what happened for the task's writer to be told.
func (s *Service) tellRebased(ctx context.Context, target checkoutTarget, result RebaseResult) {
	if err := s.recordRebase(ctx, target, rebaseOutcome{Result: result}); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to record that a task's worktree was rebased",
			append([]slog.Attr{slog.String(logging.TaskID, target.task.ID)}, logging.Err(err)...)...)
	}
}

// refuseTreeState is what the checkout itself has to say before a rebase may start.
func refuseTreeState(state git.TreeState) error {
	switch {
	case state.InProgress:
		return errRebaseInProgress
	case state.Branch == "":
		return errRebaseDetached
	case state.Shallow:
		return ErrRebaseShallow
	case len(state.HiddenPaths) > 0:
		return fmt.Errorf("%w: %s", ErrRebaseHiddenPaths, strings.Join(state.HiddenPaths, ", "))
	case state.TrackedChanges:
		return ErrRebaseDirty
	}

	return nil
}

func (s *Service) recordRebase(ctx context.Context, target checkoutTarget, outcome rebaseOutcome) error {
	raw, err := json.Marshal(outcome)
	if err != nil {
		return fmt.Errorf("failed to encode the rebase of task %q: %w", target.task.Slug, err)
	}

	event, err := events.NewEvent(rebased{TaskID: target.task.ID})
	if err != nil {
		return err
	}

	return s.events.InsertManyTx(ctx, func(tx pgx.Tx) ([]river.JobArgs, error) {
		if err := taskstore.NewTaskStore(tx).SetLastRebase(ctx, target.task.ID, raw); err != nil {
			return nil, err
		}

		return []river.JobArgs{*event}, nil
	})
}
