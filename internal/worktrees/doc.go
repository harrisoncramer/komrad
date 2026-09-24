// Package worktrees reads the directories weave cut for its agents and removes the ones no
// agent answers for.
//
// The listing runs no git. It reads the .git file and the gitdir's HEAD of each directory and
// joins the size stored for it. A directory belongs to the agent row named by its basename, and
// the other rows whose cwd is the directory are the agents working in it. Inspect asks git about
// one directory and walks its size, which it records. The Measurer records every directory no
// delete names and walks a few of them a pass, so every directory gets a size in time.
// Remove takes a directory no row answers for, or one whose row is stuck in failed_to_cleanup
// because git can no longer read the registration, and leaves alone anything an agent works in
// or under.
package worktrees
