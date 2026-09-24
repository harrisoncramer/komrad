package gittest

import (
	"context"
	"fmt"

	"weave/internal/git"
)

// Checkout answers every question about a checkout with what the test put in it, remembering
// which base it was asked to resolve and which repositories it was asked to prune.
type Checkout struct {
	Changes    git.Changes
	State      git.TreeState
	Ahead      int
	Behind     int
	Prediction git.Prediction
	PredictErr error
	Held       bool
	Unresolved bool
	Commits    []git.Commit
	Resolved   string
	Resolves   int
	Fetched    string
	Fetches    int
	Pruned     []string
	Registered []git.Worktree
	ListErr    error
}

func (g *Checkout) ReadChanges(context.Context, string) (git.Changes, error) {
	return g.Changes, nil
}

func (g *Checkout) ReadTreeState(context.Context, string) (git.TreeState, error) {
	return g.State, nil
}

func (g *Checkout) ResolveBase(_ context.Context, _, base string) (string, error) {
	g.Resolved = base
	g.Resolves++
	if g.Unresolved || base == "" {
		return "", fmt.Errorf("%w: the clone has no origin/%s", git.ErrBaseMissing, base)
	}

	return "origin/" + base, nil
}

func (g *Checkout) FetchBase(_ context.Context, _, base string) (string, string, error) {
	g.Fetched = base
	g.Fetches++

	return "origin/" + base, "abc", nil
}

func (g *Checkout) CountAheadBehind(context.Context, string, string) (int, int, error) {
	return g.Ahead, g.Behind, nil
}

func (g *Checkout) PredictRebase(context.Context, string, string) (git.Prediction, error) {
	return g.Prediction, g.PredictErr
}

func (g *Checkout) IsBranchHeldElsewhere(context.Context, string, string) (bool, error) {
	return g.Held, nil
}

func (g *Checkout) ListCommitsSince(context.Context, string, string, int) ([]git.Commit, error) {
	return g.Commits, nil
}

func (g *Checkout) ListWorktrees(context.Context, string) ([]git.Worktree, error) {
	return g.Registered, g.ListErr
}

func (g *Checkout) PruneWorktrees(_ context.Context, repo string) error {
	g.Pruned = append(g.Pruned, repo)

	return nil
}
