package ui

import (
	"context"

	"komrad/internal/git"
)

// roundComparison is how an older round reads against a newer one once their bases are settled.
type roundComparison struct {
	// FromSHA is the older side of the diff, the older snapshot or its replay onto the newer base.
	FromSHA string
	// Comparable is false when the two rounds stand on different bases and no replay settled.
	Comparable bool
	// Rewritten says the older base is not reachable from the newer one.
	Rewritten bool
	// Replayed says the older side was carried onto the newer base.
	Replayed bool
	// Conflicts is the paths the newer base rewrote under the older round.
	Conflicts []string
}

// compareRoundsParams is the older round's snapshot, both bases, and where to read the repository.
type compareRoundsParams struct {
	Git      ReviewGit
	Dir      string
	FromSHA  string
	FromBase string
	ToBase   string
}

// compareRounds settles the older side of a diff between two rounds, carrying it onto the newer
// base when a rebase lies between them.
func compareRounds(ctx context.Context, params compareRoundsParams) roundComparison {
	if params.FromBase == params.ToBase {
		return roundComparison{
			FromSHA:    params.FromSHA,
			Comparable: true,
		}
	}

	reachable, err := params.Git.IsAncestor(ctx, params.Dir, params.FromBase, params.ToBase)
	compared := roundComparison{
		FromSHA:   params.FromSHA,
		Rewritten: err == nil && !reachable,
	}

	// A git too old to replay, or a commit gone, leaves the rounds apart.
	replayed, err := params.Git.Replay(ctx, git.ReplayParams{
		Dir:         params.Dir,
		FromBase:    params.FromBase,
		OntoBase:    params.ToBase,
		SnapshotSHA: params.FromSHA,
	})
	if err != nil {
		return compared
	}

	if len(replayed.Conflicts) > 0 {
		compared.Conflicts = replayed.Conflicts

		return compared
	}

	compared.FromSHA = replayed.Tree
	compared.Comparable = true
	compared.Replayed = true

	return compared
}
