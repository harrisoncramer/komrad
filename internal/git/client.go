package git

import (
	"context"

	"weave/internal/position"
)

// Client is git as a dependency a caller can take.
type Client struct{}

// NewClient returns a client that runs git in whichever directory a call names.
func NewClient() *Client {
	return &Client{}
}

// ReadTreeState reports what a checkout is holding.
func (*Client) ReadTreeState(ctx context.Context, dir string) (TreeState, error) {
	return ReadTreeState(ctx, dir)
}

// FetchBase brings the base branch up to date and names what it resolved to.
func (*Client) FetchBase(ctx context.Context, dir, base string) (ref, sha string, err error) {
	return FetchBase(ctx, dir, base)
}

// CountCommitsToReplay is how many of a branch's own commits sit above the base.
func (*Client) CountCommitsToReplay(ctx context.Context, dir, ref string) (int, error) {
	return CountCommitsToReplay(ctx, dir, ref)
}

// PredictRebase reads what a rebase would do without moving anything.
func (*Client) PredictRebase(ctx context.Context, dir, ref string) (Prediction, error) {
	return PredictRebase(ctx, dir, ref)
}

// Park holds a checkout as it stands under a ref of its own.
func (*Client) Park(ctx context.Context, dir, ref string) (string, error) {
	return Park(ctx, dir, ref)
}

// ReleaseRefsUnder drops every ref under a prefix apart from the one to keep.
func (*Client) ReleaseRefsUnder(ctx context.Context, dir, prefix, keep string) error {
	return ReleaseRefsUnder(ctx, dir, prefix, keep)
}

// Rebase replays a branch's commits onto its base.
func (*Client) Rebase(ctx context.Context, dir, ref string) error {
	return Rebase(ctx, dir, ref)
}

// RebaseOnto replays only the commits that follow a cutoff.
func (*Client) RebaseOnto(ctx context.Context, dir, ref, cutoff string) error {
	return RebaseOnto(ctx, dir, ref, cutoff)
}

// TakeSnapshot freezes a worktree as a commit nothing else can reach.
func (*Client) TakeSnapshot(ctx context.Context, params TakeSnapshotParams) (*Snapshot, error) {
	return TakeSnapshot(ctx, params)
}

// HoldRef points a ref at a commit so nothing collects it.
func (*Client) HoldRef(ctx context.Context, dir, ref, sha string) error {
	return HoldRef(ctx, dir, ref, sha)
}

// ReleaseRef drops a ref, leaving what it held to be collected.
func (*Client) ReleaseRef(ctx context.Context, dir, ref string) error {
	return ReleaseRef(ctx, dir, ref)
}

// ListWorktrees reports every checkout of a repository, the main one first.
func (*Client) ListWorktrees(ctx context.Context, repo string) ([]Worktree, error) {
	return ListWorktrees(ctx, repo)
}

// Materialise checks a commit out as a worktree of its own.
func (*Client) Materialise(ctx context.Context, dir, sha, at string) error {
	return Materialise(ctx, dir, sha, at)
}

// WorktreeHead is the commit a materialised checkout stands on.
func (*Client) WorktreeHead(ctx context.Context, at string) (string, bool) {
	return WorktreeHead(ctx, at)
}

// IsDirty reports whether a checkout holds anything the commit it stands on does not.
func (*Client) IsDirty(ctx context.Context, at string) (bool, error) {
	return IsDirty(ctx, at)
}

// Reap removes a checkout and the registration that names it.
func (*Client) Reap(ctx context.Context, dir, at string) error {
	return Reap(ctx, dir, at)
}

// Edits are the runs of lines a checkout holds that its commit does not.
func (*Client) Edits(ctx context.Context, dir string) ([]Edit, error) {
	return Edits(ctx, dir)
}

// ReadFileLines reads one file as a commit holds it.
func (*Client) ReadFileLines(ctx context.Context, dir, sha, path string) ([]string, error) {
	return ReadFileLines(ctx, dir, sha, path)
}

// CountChangedFiles is how many files differ between two commits.
func (*Client) CountChangedFiles(ctx context.Context, params CountChangedFilesParams) (int, error) {
	return CountChangedFiles(ctx, params)
}

// MapRanges follows line ranges from one commit to another.
func (*Client) MapRanges(ctx context.Context, params MapRangesParams) ([]position.Mapping, error) {
	return MapRanges(ctx, params)
}

// ProbeTree is the tree a checkout would freeze to right now.
func (*Client) ProbeTree(ctx context.Context, dir string) (string, error) {
	return ProbeTree(ctx, dir)
}

// GetTree is the tree a commit holds.
func (*Client) GetTree(ctx context.Context, dir, sha string) (string, error) {
	return GetTree(ctx, dir, sha)
}
