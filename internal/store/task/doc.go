// Package task stores tasks. Beside each task's slug, goal and notes it keeps the task's
// checkout and branch head, its parent and fork point, and whether it is hidden or archived.
// The package also claims the tasks a delete takes and records when their worktrees are removed.
package task
