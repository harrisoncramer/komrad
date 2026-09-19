package worktrees

import (
	"context"
	"errors"
	"path/filepath"
	"slices"

	"weave/internal/git"
	"weave/internal/store"
	workflowstore "weave/internal/store/workflow"
)

// commitLimit is how many of a branch's commits an inspection lists.
const commitLimit = 50

// Inspection is one directory read through git.
type Inspection struct {
	Worktree Worktree
	// Bytes is what the directory holds, and Entries its top-level entries, biggest first.
	Bytes   int64
	Entries []Entry
	Changes git.Changes
	// BaseRef is what the branch is measured against, empty when git cannot read the directory
	// or cannot name a base for it.
	BaseRef string
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
	ResolveBase(ctx context.Context, dir, base string) (string, bool)
	FetchBase(ctx context.Context, dir, base string) (ref, sha string, err error)
	CountAheadBehind(ctx context.Context, dir, ref string) (ahead, behind int, err error)
	PredictRebase(ctx context.Context, dir, ref string) (git.Prediction, error)
	IsBranchHeldElsewhere(ctx context.Context, dir, branch string) (bool, error)
	ListCommitsSince(ctx context.Context, dir, ref string, limit int) ([]git.Commit, error)
}

type workflowReader interface {
	GetIncludingArchived(ctx context.Context, id string) (*workflowstore.Workflow, error)
}

// Inspector reads one directory through git.
type Inspector struct {
	lister    *Lister
	git       gitReader
	workflows workflowReader
}

type NewInspectorParams struct {
	Lister    *Lister
	Git       gitReader
	Workflows workflowReader
}

// NewInspector builds an inspector over the lister and git.
func NewInspector(params NewInspectorParams) *Inspector {
	return &Inspector{lister: params.Lister, git: params.Git, workflows: params.Workflows}
}

// Inspect reads a directory the listing holds, fetching the base first when asked to.
func (i *Inspector) Inspect(ctx context.Context, dir string, fetch bool) (Inspection, error) {
	listing, err := i.lister.List(ctx)
	if err != nil {
		return Inspection{}, err
	}

	dir = filepath.Clean(dir)
	at := slices.IndexFunc(listing.Worktrees, func(w Worktree) bool { return w.Dir == dir })
	if at < 0 {
		return Inspection{}, store.ErrNotFound.Saying("%s is not under the worktree directories", dir)
	}

	inspection := Inspection{Worktree: listing.Worktrees[at]}
	if inspection.Bytes, inspection.Entries, err = measure(ctx, dir); err != nil {
		return Inspection{}, err
	}
	if !inspection.Worktree.Readable {
		return inspection, nil
	}

	base, err := i.baseOf(ctx, inspection.Worktree)
	if err != nil {
		return Inspection{}, err
	}

	ref, ok, err := i.nameBase(ctx, dir, base, fetch)
	if err != nil {
		return Inspection{}, err
	}
	if !ok {
		return inspection, nil
	}
	inspection.BaseRef = ref

	if err := i.readStanding(ctx, &inspection); err != nil {
		return Inspection{}, err
	}

	return inspection, nil
}

// nameBase is the ref the branch is measured against, and whether git could name one.
func (i *Inspector) nameBase(ctx context.Context, dir, base string, fetch bool) (string, bool, error) {
	if !fetch {
		ref, ok := i.git.ResolveBase(ctx, dir, base)

		return ref, ok, nil
	}

	ref, _, err := i.git.FetchBase(ctx, dir, base)
	if errors.Is(err, git.ErrBaseUnresolved) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}

	return ref, true, nil
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
