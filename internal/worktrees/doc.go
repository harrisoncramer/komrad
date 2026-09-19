// Package worktrees reads the directories weave cut for its agents, measures what they hold,
// and removes the ones no agent answers for.
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
// # A size is walked on the get path
//
// Sizing a directory walks every file under it, which runs to seconds for a big checkout and
// to minutes across a few hundred, so the listing never sizes anything. Inspect walks the one
// directory it is asked about, and nothing is stored, so a size is always as of the call.
package worktrees
