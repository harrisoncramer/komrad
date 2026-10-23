// Package worktrees lists the directories under komrad's worktree directories, measures them, and
// removes the ones no task or agent needs. The panel's worktrees screen reads and removes through
// it.
//
// The [Lister] runs no git. It reads each directory's .git file and HEAD and joins what komrad
// recorded about it, which is the task whose checkout it is and the agents working in it. The
// [Inspector] asks git about one directory, and the [Measurer] walks a few directories a pass so
// each gets a size in time. The [Remover] refuses any directory whose task is live, archived or
// being deleted, one an agent works in or under, and one holding another worktree, and names the
// reason for each.
package worktrees
