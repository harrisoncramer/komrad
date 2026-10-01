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
	Checkout
	Ahead  int
	Behind int
	// DiffStat is nil where there is no base, or one the branch shares no history with.
	DiffStat *git.DiffStat
	Remote   git.RemoteStanding
	// RemoteLanded says the base already holds every commit origin's copy of the branch has that
	// the branch does not.
	RemoteLanded bool
	// KeepsBranch says deleting the agent would leave the branch behind.
	KeepsBranch bool
	Commits     []git.Commit
}

type gitReader interface {
	checkoutGit
	CountAheadBehind(ctx context.Context, dir, ref string) (ahead, behind int, err error)
	IsBranchHeldElsewhere(ctx context.Context, dir, branch string) (bool, error)
	ListCommitsSince(ctx context.Context, dir, ref string, limit int) ([]git.Commit, error)
	CountAgainstRemote(ctx context.Context, dir, branch string) (*git.RemoteStanding, error)
	DoesBaseHoldRemote(ctx context.Context, params git.DoesBaseHoldRemoteParams) (bool, error)
}

type NewInspectorParams struct {
	Lister    *Lister
	Git       gitReader
	Workflows *workflowstore.WorkflowStore
	Worktrees *worktreestore.WorktreeStore
	// Now is the clock, time.Now when unset.
	Now func() time.Time
}

// Inspector reads one directory through git.
type Inspector struct {
	lister    *Lister
	git       gitReader
	checkouts *CheckoutReader
	workflows *workflowstore.WorkflowStore
	worktrees *worktreestore.WorktreeStore
	now       func() time.Time
}

// NewInspector builds an inspector over the lister, git and the worktree store.
func NewInspector(params NewInspectorParams) *Inspector {
	now := params.Now
	if now == nil {
		now = time.Now
	}

	return &Inspector{
		lister: params.Lister,
		git:    params.Git,
		checkouts: NewCheckoutReader(NewCheckoutReaderParams{
			Git: params.Git,
			Now: now,
		}),
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

	standing := i.checkouts.standingOf(standingOfParams{
		Dir:      dir,
		Readable: inspection.Worktree.Readable,
		Checkout: inspection.Checkout,
	})
	if err := i.worktrees.RecordStanding(ctx, standing); err != nil {
		return nil, err
	}

	return inspection, nil
}

// ReadStanding reads where a directory stands against its base without asking origin or taking
// the index lock, or why git could not say.
func (i *Inspector) ReadStanding(ctx context.Context, worktree Worktree) worktreestore.Standing {
	params := ReadStandingParams{
		Dir:      worktree.Dir,
		Readable: worktree.Readable,
	}
	if worktree.Readable {
		config, err := i.getWorktreeConfig(ctx, worktree)
		if err != nil {
			return i.checkouts.standingOf(standingOfParams{
				Dir:      worktree.Dir,
				Readable: true,
				Err:      err,
			})
		}
		params.Base = config.BaseBranch
	}

	return i.checkouts.ReadStanding(ctx, params)
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

	exclude, err := i.readSettled(ctx, inspection, fetch)
	if err != nil {
		return err
	}
	if !inspection.Worktree.Readable || inspection.BaseRef == "" {
		return nil
	}

	return i.readRest(ctx, inspection, exclude)
}

// readSettled reads the part of the standing a directory's delete turns on and answers the paths
// the workflow leaves out of a diff.
func (i *Inspector) readSettled(ctx context.Context, inspection *Inspection, fetch bool) ([]string, error) {
	if !inspection.Worktree.Readable {
		return nil, nil
	}

	config, err := i.getWorktreeConfig(ctx, inspection.Worktree)
	if err != nil {
		return nil, err
	}

	err = i.checkouts.read(ctx, &inspection.Checkout, readParams{
		Dir:   inspection.Worktree.Dir,
		Base:  config.BaseBranch,
		Fetch: fetch,
	})
	if err != nil {
		return nil, err
	}

	return config.DiffExclude, nil
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

func (i *Inspector) readRest(ctx context.Context, inspection *Inspection, exclude []string) error {
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
		Exclude: exclude,
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
