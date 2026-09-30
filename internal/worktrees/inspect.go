package worktrees

import (
	"context"
	"errors"
	"fmt"
	"time"

	"komrad/internal/git"
	"komrad/internal/store"
	workflowstore "komrad/internal/store/workflow"
	worktreestore "komrad/internal/store/worktree"
)

// commitLimit is how many of a branch's commits an inspection lists.
const commitLimit = 50

// Inspection is one directory read through git.
type Inspection struct {
	Worktree Worktree
	// Bytes is the disk the directory takes, and Entries its top-level entries, biggest first.
	Bytes   int64
	Entries []Entry
	Changes git.Changes
	// BaseRef is what the branch is measured against, empty when git cannot read the directory
	// or the clone does not hold the base.
	BaseRef string
	// BaseErr is why the base could not be named, when it could not.
	BaseErr error
	Ahead   int
	Behind  int
	// DiffStat is nil where there is no base, or one the branch shares no history with.
	DiffStat *git.DiffStat
	Remote   git.RemoteStanding
	// RemoteLanded says the base already holds every commit origin's copy of the branch has that
	// the branch does not.
	RemoteLanded bool
	// Landed is how many of the branch's own commits the base already holds whole.
	Landed int
	// Unlanded is how many of the branch's own commits the base does not yet hold.
	Unlanded int
	// Unrelated says the branch shares no history with the base.
	Unrelated          bool
	SequenceInProgress bool
	// KeepsBranch says deleting the agent would leave the branch behind.
	KeepsBranch bool
	Commits     []git.Commit
	diffExclude []string
}

type gitReader interface {
	ReadChanges(ctx context.Context, dir string) (git.Changes, error)
	ReadTreeState(ctx context.Context, dir string) (git.TreeState, error)
	ResolveBase(ctx context.Context, dir, base string) (string, error)
	FetchBase(ctx context.Context, dir, base string) (ref, sha string, err error)
	CountAheadBehind(ctx context.Context, dir, ref string) (ahead, behind int, err error)
	PredictRebase(ctx context.Context, dir, ref string) (git.Prediction, error)
	IsBranchHeldElsewhere(ctx context.Context, dir, branch string) (bool, error)
	ListCommitsSince(ctx context.Context, dir, ref string, limit int) ([]git.Commit, error)
	ReadDiffStat(ctx context.Context, params git.ReadDiffStatParams) (*git.DiffStat, error)
	CountAgainstRemote(ctx context.Context, dir, branch string) (*git.RemoteStanding, error)
	DoesBaseHoldRemote(ctx context.Context, params git.DoesBaseHoldRemoteParams) (bool, error)
}

type workflowReader interface {
	GetIncludingArchived(ctx context.Context, id string) (*workflowstore.Workflow, error)
}

type worktreeRecorder interface {
	RecordSize(ctx context.Context, size worktreestore.Size) error
	RecordStanding(ctx context.Context, standing worktreestore.Standing) error
}

type NewInspectorParams struct {
	Lister    *Lister
	Git       gitReader
	Workflows workflowReader
	Worktrees worktreeRecorder
	// Now is the clock, time.Now when unset.
	Now func() time.Time
}

// Inspector reads one directory through git.
type Inspector struct {
	lister    *Lister
	git       gitReader
	workflows workflowReader
	worktrees worktreeRecorder
	now       func() time.Time
}

// NewInspector builds an inspector over the lister, git and the worktree store.
func NewInspector(params NewInspectorParams) *Inspector {
	now := params.Now
	if now == nil {
		now = time.Now
	}

	return &Inspector{
		lister:    params.Lister,
		git:       params.Git,
		workflows: params.Workflows,
		worktrees: params.Worktrees,
		now:       now,
	}
}

type InspectParams struct {
	Dir string
	// Fetch asks origin for the base before measuring against it.
	Fetch bool
}

// Inspect reads a directory the listing holds, recording its size and where it stands.
func (i *Inspector) Inspect(ctx context.Context, params InspectParams) (*Inspection, error) {
	worktree, err := i.lister.Get(ctx, params.Dir)
	if err != nil {
		return nil, err
	}

	dir := worktree.Dir
	inspection := &Inspection{Worktree: *worktree}
	measured, err := measure(ctx, dir)
	if err != nil {
		return nil, err
	}
	inspection.Bytes, inspection.Entries = measured.bytes, measured.entries

	size := worktreestore.Size{
		Dir:        dir,
		Bytes:      measured.bytes,
		Share:      measured.share,
		MeasuredAt: i.now(),
	}
	if err := i.worktrees.RecordSize(ctx, size); err != nil {
		return nil, err
	}
	inspection.Worktree.Size = &size

	if err := i.readAll(ctx, inspection, params.Fetch); err != nil {
		return nil, err
	}

	if err := i.worktrees.RecordStanding(ctx, i.standingOf(inspection, nil)); err != nil {
		return nil, err
	}

	return inspection, nil
}

// ReadStanding reads where a directory stands against its base without asking origin, or why
// git could not say.
func (i *Inspector) ReadStanding(ctx context.Context, worktree Worktree) worktreestore.Standing {
	inspection := &Inspection{Worktree: worktree}

	return i.standingOf(inspection, i.readSettled(ctx, inspection, false))
}

// standingOf is what an inspection says of where the directory stands.
func (i *Inspector) standingOf(inspection *Inspection, err error) worktreestore.Standing {
	standing := worktreestore.Standing{
		Dir:        inspection.Worktree.Dir,
		ReadAt:     i.now(),
		Dirty:      len(inspection.Changes.Modified)+len(inspection.Changes.Untracked) > 0,
		Sequencing: inspection.SequenceInProgress,
		Unlanded:   inspection.Unlanded,
	}

	switch {
	case err != nil:
		standing.Error = storableText(err.Error())
	case !inspection.Worktree.Readable:
		standing.Error = "git cannot read the directory"
	case inspection.BaseErr != nil:
		standing.Error = storableText(inspection.BaseErr.Error())
	case inspection.Unrelated:
		standing.Error = "the branch shares no history with " + inspection.BaseRef
	}

	return standing
}

func (i *Inspector) readAll(ctx context.Context, inspection *Inspection, fetch bool) error {
	dir := inspection.Worktree.Dir
	if inspection.Worktree.Readable && inspection.Worktree.Branch != "" {
		remote, err := i.git.CountAgainstRemote(ctx, dir, inspection.Worktree.Branch)
		if err != nil {
			return fmt.Errorf("failed to read where %q stands against origin: %w", dir, err)
		}

		inspection.Remote = *remote
	}

	if err := i.readSettled(ctx, inspection, fetch); err != nil {
		return err
	}
	if !inspection.Worktree.Readable || inspection.BaseRef == "" {
		return nil
	}

	return i.readRest(ctx, inspection)
}

// readSettled names the base and reads the part of the standing a directory's delete turns on,
// leaving the base empty when the clone does not hold it.
func (i *Inspector) readSettled(ctx context.Context, inspection *Inspection, fetch bool) error {
	if !inspection.Worktree.Readable {
		return nil
	}

	dir := inspection.Worktree.Dir
	config, err := i.getWorktreeConfig(ctx, inspection.Worktree)
	if err != nil {
		return err
	}

	ref, err := i.nameBase(ctx, dir, config.BaseBranch, fetch)
	if errors.Is(err, git.ErrBaseMissing) {
		inspection.BaseErr = err

		return nil
	}
	if err != nil {
		return err
	}
	inspection.BaseRef = ref
	inspection.diffExclude = config.DiffExclude

	state, err := i.git.ReadTreeState(ctx, dir)
	if err != nil {
		return err
	}
	inspection.SequenceInProgress = state.InProgress

	if inspection.Changes, err = i.git.ReadChanges(ctx, dir); err != nil {
		return err
	}

	prediction, err := i.git.PredictRebase(ctx, dir, ref)
	if errors.Is(err, git.ErrBaseUnrelated) {
		inspection.Unrelated = true

		return nil
	}
	if err != nil {
		return err
	}
	inspection.Landed, inspection.Unlanded = prediction.Landed, prediction.Replay

	return nil
}

// nameBase is the ref the branch is measured against, fetched from origin first when asked.
func (i *Inspector) nameBase(ctx context.Context, dir, base string, fetch bool) (string, error) {
	if !fetch {
		return i.git.ResolveBase(ctx, dir, base)
	}

	ref, _, err := i.git.FetchBase(ctx, dir, base)

	return ref, err
}

// getWorktreeConfig is the worktree settings the directory's workflow names, empty when no row names one.
func (i *Inspector) getWorktreeConfig(ctx context.Context, worktree Worktree) (workflowstore.WorktreeConfig, error) {
	if worktree.Agent == nil || worktree.Agent.WorkflowID == "" {
		return workflowstore.WorktreeConfig{}, nil
	}

	workflow, err := i.workflows.GetIncludingArchived(ctx, worktree.Agent.WorkflowID)
	if errors.Is(err, store.ErrNotFound) {
		return workflowstore.WorktreeConfig{}, nil
	}
	if err != nil {
		return workflowstore.WorktreeConfig{}, err
	}

	return workflow.Worktree, nil
}

func (i *Inspector) readRest(ctx context.Context, inspection *Inspection) error {
	dir, ref := inspection.Worktree.Dir, inspection.BaseRef

	var err error
	if inspection.Ahead, inspection.Behind, err = i.git.CountAheadBehind(ctx, dir, ref); err != nil {
		return err
	}

	if inspection.Remote.Unpulled > 0 {
		inspection.RemoteLanded, err = i.git.DoesBaseHoldRemote(ctx, git.DoesBaseHoldRemoteParams{
			Dir:    dir,
			Branch: inspection.Worktree.Branch,
			Base:   ref,
		})
		if err != nil {
			return fmt.Errorf("failed to read whether %s holds what origin has of %q: %w", ref, dir, err)
		}
	}

	stat, err := i.git.ReadDiffStat(ctx, git.ReadDiffStatParams{
		Dir:     dir,
		Ref:     ref,
		Exclude: inspection.diffExclude,
	})
	if err != nil && !errors.Is(err, git.ErrBaseUnrelated) {
		return fmt.Errorf("failed to measure %q against %s: %w", dir, ref, err)
	}

	inspection.DiffStat = stat

	if inspection.Commits, err = i.git.ListCommitsSince(ctx, dir, ref, commitLimit); err != nil {
		return err
	}

	inspection.KeepsBranch, err = i.keepsBranch(ctx, inspection.Worktree)

	return err
}

// keepsBranch says whether an adopted branch, or one no other ref reaches, would be kept.
func (i *Inspector) keepsBranch(ctx context.Context, worktree Worktree) (bool, error) {
	if worktree.Branch == "" {
		return false, nil
	}
	if worktree.Agent != nil && worktree.Agent.Adopted {
		return true, nil
	}

	held, err := i.git.IsBranchHeldElsewhere(ctx, worktree.Dir, worktree.Branch)

	return !held, err
}
