// Package worktrees reads the directories weave cut for its agents, counts what they hold, and
// removes the ones no agent answers for.
//
// # A listing runs no git
//
// The panel polls the listing every second, so it reads only what a directory says about
// itself: the .git file naming the gitdir, the gitdir's HEAD naming the branch, and whether
// that gitdir is still there. Everything git has to be asked, the uncommitted paths, the
// standing against the base, the commits, is read for one directory at a time by Inspect.
//
// # Who owns a directory
//
// Setup names a worktree after the agent it was cut for, so a directory belongs to the row
// whose id is its basename. A child works in its parent's directory and holds none of its own,
// so the rows whose cwd is the directory are its children. A directory nobody answers for is
// one whose row was deleted while cleanup did not run, or one a test left behind, and those
// are the only ones Remove will take.
//
// # Sizes are counted behind the listing
//
// A count is a walk of every file, fifteen seconds across a few hundred directories, so it is
// never done on the listing's path. The counter runs in the background, writes each number
// with when it was taken, and the listing reads those back.
package worktrees
