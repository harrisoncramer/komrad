package render

import (
	worktreestore "komrade/internal/store/worktree"
	"komrade/lib/dates"
	"komrade/lib/xptr"
	worktreev1 "komrade/proto/gen/worktree/v1"
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
		}
	}

	return worktree
}
