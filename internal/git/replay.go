package git

import (
	"context"
	"fmt"
)

// ReplayParams says which snapshot to carry onto which base.
type ReplayParams struct {
	// Dir is anywhere inside the repository holding the three commits.
	Dir string
	// FromBase is the commit the snapshot was measured from.
	FromBase string
	// OntoBase is the commit the snapshot is carried onto.
	OntoBase string
	// SnapshotSHA is the commit carried.
	SnapshotSHA string
}

// Replayed is a snapshot as it would stand on another base.
type Replayed struct {
	// Tree is the tree the replay wrote, held by no ref.
	Tree string
	// Conflicts is the paths the two sides rewrote alike, empty when the replay settled.
	Conflicts []string
}

// Replay carries a snapshot's own change onto another base in memory, answering with the tree
// that results and the paths that would not settle.
func (c *Client) Replay(ctx context.Context, params ReplayParams) (*Replayed, error) {
	if params.FromBase == "" || params.OntoBase == "" || params.SnapshotSHA == "" {
		return nil, fmt.Errorf("failed to replay in %q: three commits are needed", params.Dir)
	}

	root, err := getRoot(ctx, params.Dir)
	if err != nil {
		return nil, err
	}

	tree, conflicts, err := readMergedTree(ctx, readMergedTreeParams{
		Dir:       root,
		Left:      params.OntoBase,
		Right:     params.SnapshotSHA,
		MergeBase: params.FromBase,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to replay %s from %s onto %s: %w",
			params.SnapshotSHA, params.FromBase, params.OntoBase, err)
	}

	return &Replayed{Tree: tree, Conflicts: conflicts}, nil
}

// IsAncestor reports whether one commit is reachable from another.
func (c *Client) IsAncestor(ctx context.Context, dir, maybeAncestor, of string) (bool, error) {
	return isAncestor(ctx, dir, maybeAncestor, of)
}
