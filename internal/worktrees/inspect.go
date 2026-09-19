package worktrees

import (
	"cmp"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"

	"weave/internal/git"
	"weave/internal/store"
	workflowstore "weave/internal/store/workflow"
)

// commitLimit is how many of a branch's commits an inspection lists.
const commitLimit = 50

// Entry is one top-level entry of a directory with what it holds.
type Entry struct {
	Path  string
	Bytes int64
	Dir   bool
}

// Inspection is one directory read through git.
type Inspection struct {
	Worktree Worktree
	// Entries are the top-level entries, biggest first.
	Entries []Entry
	Changes git.Changes
	// BaseRef is what the branch is measured against, empty when git cannot read the directory.
	BaseRef string
	Ahead   int
	Behind  int
	// Landed is how many of the branch's own commits the base already holds whole.
	Landed             int
	SequenceInProgress bool
	// KeepsBranch says deleting the agent would leave the branch behind, which cleanup does
	// for an adopted branch and for one whose commits are on no other ref.
	KeepsBranch bool
	Commits     []git.Commit
}

type gitReader interface {
	ReadChanges(ctx context.Context, dir string) (git.Changes, error)
	ReadTreeState(ctx context.Context, dir string) (git.TreeState, error)
	ResolveBase(ctx context.Context, dir, base string) string
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
	inspection.Entries, err = readEntries(ctx, dir)
	if err != nil {
		return Inspection{}, err
	}
	if !inspection.Worktree.Readable {
		return inspection, nil
	}

	base, err := i.baseOf(ctx, inspection.Worktree)
	if err != nil {
		return Inspection{}, err
	}
	if fetch {
		if inspection.BaseRef, _, err = i.git.FetchBase(ctx, dir, base); err != nil {
			return Inspection{}, err
		}
	} else {
		inspection.BaseRef = i.git.ResolveBase(ctx, dir, base)
	}

	if err := i.readStanding(ctx, &inspection); err != nil {
		return Inspection{}, err
	}

	return inspection, nil
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

// keepsBranch answers the cleanup script's own question: an adopted branch is always kept,
// and any other is kept when no other ref reaches its tip.
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

// readEntries is a directory's top-level entries with what each holds, biggest first.
func readEntries(ctx context.Context, dir string) ([]Entry, error) {
	listed, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	entries := make([]Entry, 0, len(listed))
	for _, item := range listed {
		entry := Entry{Path: item.Name(), Dir: item.IsDir()}
		if item.IsDir() {
			entry.Bytes, err = measure(ctx, filepath.Join(dir, item.Name()))
			if err != nil {
				return nil, err
			}
		} else if info, err := item.Info(); err == nil && info.Mode().IsRegular() {
			entry.Bytes = info.Size()
		}

		entries = append(entries, entry)
	}

	slices.SortStableFunc(entries, func(a, b Entry) int { return cmp.Compare(b.Bytes, a.Bytes) })

	return entries, nil
}
