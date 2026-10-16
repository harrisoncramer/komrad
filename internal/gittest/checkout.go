package gittest

import (
	"context"
	"fmt"

	"komrad/internal/git"
)

// Checkout answers every question about a checkout with what the test put in it, remembering
// which base callers asked it to resolve, which repositories they asked it to prune and how often
// they asked it to refresh the index.
type Checkout struct {
	Changes      git.Changes
	State        git.TreeState
	Ahead        int
	Behind       int
	Prediction   git.Prediction
	PredictErr   error
	BaseTip      string
	Held         bool
	Unresolved   bool
	Commits      []git.Commit
	DiffStat     git.DiffStat
	DiffErr      error
	Excluded     []string
	Remote       git.RemoteStanding
	RemoteLanded bool
	HoldAsked    string
	BaseRead     string
	BaseReads    int
	Fetched      string
	Fetches      int
	Pruned       []string
	Unlocked     []string
	UnlockErr    error
	Registered   []git.Worktree
	ListErr      error
	Origin       string
	Refreshes    int
}

// ReadChanges counts a refresh and answers Changes.
func (g *Checkout) ReadChanges(context.Context, string) (git.Changes, error) {
	g.Refreshes++

	return g.Changes, nil
}

// PeekChanges answers Changes.
func (g *Checkout) PeekChanges(context.Context, string) (git.Changes, error) {
	return g.Changes, nil
}

// ReadTreeState counts a refresh and answers State.
func (g *Checkout) ReadTreeState(context.Context, string) (git.TreeState, error) {
	g.Refreshes++

	return g.State, nil
}

// IsSequenceInProgress answers whether State is part-way through.
func (g *Checkout) IsSequenceInProgress(context.Context, string) (bool, error) {
	return g.State.InProgress, nil
}

// ReadBase records the base and answers its origin ref at BaseTip, refusing a blank or unresolved one.
func (g *Checkout) ReadBase(_ context.Context, _, base string) (*git.Base, error) {
	g.BaseRead = base
	g.BaseReads++
	if g.Unresolved || base == "" {
		return nil, fmt.Errorf("%w: the clone has no origin/%s", git.ErrBaseMissing, base)
	}

	return &git.Base{Ref: "origin/" + base, Tip: g.BaseTip}, nil
}

// FetchBase records the base and answers its origin ref.
func (g *Checkout) FetchBase(_ context.Context, _, base string) (string, string, error) {
	g.Fetched = base
	g.Fetches++

	return "origin/" + base, "abc", nil
}

// CountAheadBehind answers Ahead and Behind.
func (g *Checkout) CountAheadBehind(context.Context, string, string) (int, int, error) {
	return g.Ahead, g.Behind, nil
}

// PredictRebase answers Prediction and PredictErr.
func (g *Checkout) PredictRebase(context.Context, string, string) (git.Prediction, error) {
	return g.Prediction, g.PredictErr
}

// IsBranchHeldElsewhere answers Held.
func (g *Checkout) IsBranchHeldElsewhere(context.Context, string, string) (bool, error) {
	return g.Held, nil
}

// ListCommitsSince answers Commits.
func (g *Checkout) ListCommitsSince(context.Context, string, string, int) ([]git.Commit, error) {
	return g.Commits, nil
}

// ReadDiffStat records the paths it was told to leave out and answers DiffStat and DiffErr.
func (g *Checkout) ReadDiffStat(_ context.Context, params git.ReadDiffStatParams) (*git.DiffStat, error) {
	g.Excluded = params.Exclude
	if g.DiffErr != nil {
		return nil, g.DiffErr
	}

	stat := g.DiffStat

	return &stat, nil
}

// DoesBaseHoldRemote records the base it was asked about and answers RemoteLanded.
func (g *Checkout) DoesBaseHoldRemote(_ context.Context, params git.DoesBaseHoldRemoteParams) (bool, error) {
	g.HoldAsked = params.Base

	return g.RemoteLanded, nil
}

// CountAgainstRemote answers Remote.
func (g *Checkout) CountAgainstRemote(context.Context, string, string) (*git.RemoteStanding, error) {
	remote := g.Remote

	return &remote, nil
}

// ListWorktrees answers Registered and ListErr.
func (g *Checkout) ListWorktrees(context.Context, string) ([]git.Worktree, error) {
	return g.Registered, g.ListErr
}

// ReadRepositoryURL answers Origin.
func (g *Checkout) ReadRepositoryURL(context.Context, string) string {
	return g.Origin
}

// UnlockWorktree records the directory and answers UnlockErr.
func (g *Checkout) UnlockWorktree(_ context.Context, _, dir string) error {
	g.Unlocked = append(g.Unlocked, dir)

	return g.UnlockErr
}

// PruneWorktrees records the repository.
func (g *Checkout) PruneWorktrees(_ context.Context, repo string) error {
	g.Pruned = append(g.Pruned, repo)

	return nil
}
