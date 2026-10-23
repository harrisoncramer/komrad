// Package worktree serves WorktreeService. It lists the worktrees komrad created, reads one
// through git, removes the ones no agent or task answers for, and queues the delete of the ones a
// caller names.
package worktree
