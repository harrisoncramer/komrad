// Package worktree serves WorktreeService. It lists the worktrees komrad created, reads one
// through git, removes the ones agents and tasks don't answer for, and queues the delete of the
// ones a caller names.
package worktree
