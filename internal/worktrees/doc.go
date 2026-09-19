// Package worktrees reads the directories weave cut for its agents and removes the ones no
// agent answers for.
//
// The listing is polled every second, so it runs no git and reads only the .git file and the
// gitdir's HEAD of each directory. A directory belongs to the agent row named by its basename,
// and the other rows whose cwd is the directory are the agents working in it. Everything git has
// to be asked, and the size, is read for one directory at a time by Inspect. Remove takes a
// directory no row answers for, or one whose row is stuck in failed_to_cleanup because git can
// no longer read the registration, and leaves alone anything an agent works in or under.
package worktrees
