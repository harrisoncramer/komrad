package render

import (
	worktreestore "komrad/internal/store/worktree"
	"komrad/lib/dates"
	"komrad/lib/xptr"
	worktreev1 "komrad/proto/gen/worktree/v1"
)

// WatchedWorktree draws one worktree directory as the watch carries it.
func WatchedWorktree(stored worktreestore.Worktree) *worktreev1.WatchedWorktree {
	worktree := &worktreev1.WatchedWorktree{
		Dir:        stored.Dir,
		RepoDir:    stored.RepoDir,
		Branch:     stored.Branch,
		Readable:   stored.Readable,
		ModifiedAt: dates.Stamp(xptr.Deref(stored.ModifiedAt)),
	}
	if size := stored.Size; size != nil {
		worktree.MeasuredAt = dates.StampPtr(&size.MeasuredAt)
		worktree.SizeError = size.Error
		if size.Error == "" {
			worktree.SizeBytes = &size.Bytes
			worktree.ShareBytes = &size.Share
		}
	}

	return worktree
}
