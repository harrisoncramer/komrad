package worktrees

import (
	"context"
	"errors"
	"time"

	"weave/internal/git"
	"weave/internal/store"
	workflowstore "weave/internal/store/workflow"
	worktreestore "weave/internal/store/worktree"
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
	// Landed is how many of the branch's own commits the base already holds whole.
	Landed             int
	SequenceInProgress bool
	// KeepsBranch says deleting the agent would leave the branch behind.
	KeepsBranch bool
	Commits     []git.Commit
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
}

type workflowReader interface {
	GetIncludingArchived(ctx context.Context, id string) (*workflowstore.Workflow, error)
}

type sizeRecorder interface {
	RecordSize(ctx context.Context, size worktreestore.Size) error
}

type NewInspectorParams struct {
	Lister    *Lister
	Git       gitReader
	Workflows workflowReader
	Sizes     sizeRecorder
	// Now is the clock, time.Now when unset.
	Now func() time.Time
}

// Inspector reads one directory through git.
type Inspector struct {
	lister    *Lister
	git       gitReader
	workflows workflowReader
	sizes     sizeRecorder
	now       func() time.Time
}

// NewInspector builds an inspector over the lister, git and the size store.
func NewInspector(params NewInspectorParams) *Inspector {
	now := params.Now
	if now == nil {
		now = time.Now
	}

	return &Inspector{
		lister:    params.Lister,
		git:       params.Git,
		workflows: params.Workflows,
		sizes:     params.Sizes,
		now:       now,
	}
}

type InspectParams struct {
	Dir string
	// Fetch asks origin for the base before measuring against it.
	Fetch bool
}

// Inspect reads a directory the listing holds.
func (i *Inspector) Inspect(ctx context.Context, params InspectParams) (*Inspection, error) {
	worktree, err := i.lister.Get(ctx, params.Dir)
	if err != nil {
		return nil, err
	}

	dir := worktree.Dir
	inspection := &Inspection{Worktree: *worktree}
	if inspection.Bytes, inspection.Entries, err = measure(ctx, dir); err != nil {
		return nil, err
	}

	size := worktreestore.Size{
		Dir:        dir,
		Bytes:      inspection.Bytes,
		MeasuredAt: i.now(),
	}
	if err := i.sizes.RecordSize(ctx, size); err != nil {
		return nil, err
	}
	inspection.Worktree.Size = &size

	if !inspection.Worktree.Readable {
		return inspection, nil
	}

	base, err := i.baseOf(ctx, inspection.Worktree)
	if err != nil {
		return nil, err
	}

	ref, err := i.nameBase(ctx, dir, base, params.Fetch)
	if errors.Is(err, git.ErrBaseMissing) {
		inspection.BaseErr = err

		return inspection, nil
	}
	if err != nil {
		return nil, err
	}
	inspection.BaseRef = ref

	if err := i.readStanding(ctx, inspection); err != nil {
		return nil, err
	}

	return inspection, nil
}

// nameBase is the ref the branch is measured against, fetched from origin first when asked.
func (i *Inspector) nameBase(ctx context.Context, dir, base string, fetch bool) (string, error) {
	if !fetch {
		return i.git.ResolveBase(ctx, dir, base)
	}

	ref, _, err := i.git.FetchBase(ctx, dir, base)

	return ref, err
}

// baseOf is the base branch the directory's workflow names, empty when no row names one.
func (i *Inspector) baseOf(ctx context.Context, worktree Worktree) (string, error) {
	if worktree.Agent == nil || worktree.Agent.WorkflowID == "" {
		return "", nil
	}

	workflow, err := i.workflows.GetIncludingArchived(ctx, worktree.Agent.WorkflowID)
	if errors.Is(err, store.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}

	return workflow.Worktree.BaseBranch, nil
}

func (i *Inspector) readStanding(ctx context.Context, inspection *Inspection) error {
	dir, ref := inspection.Worktree.Dir, inspection.BaseRef

	state, err := i.git.ReadTreeState(ctx, dir)
	if err != nil {
		return err
	}
	inspection.SequenceInProgress = state.InProgress

	if inspection.Changes, err = i.git.ReadChanges(ctx, dir); err != nil {
		return err
	}

	if inspection.Ahead, inspection.Behind, err = i.git.CountAheadBehind(ctx, dir, ref); err != nil {
		return err
	}

	prediction, err := i.git.PredictRebase(ctx, dir, ref)
	if err != nil && !errors.Is(err, git.ErrBaseUnrelated) {
		return err
	}
	inspection.Landed = prediction.Landed

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
