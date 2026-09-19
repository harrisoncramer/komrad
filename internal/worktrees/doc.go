// Package worktrees reads the directories weave cut for its agents and removes the ones no
// agent answers for.
//
// The listing is polled every second, so it runs no git and reads only the .git file and the
// gitdir's HEAD of each directory. A directory belongs to the agent row named by its basename,
// and the rows whose cwd is the directory are the children working in it. Everything git has to
// be asked, and the size, is read for one directory at a time by Inspect. Remove takes only a
// directory no row answers for, which is what a deleted row or a test left behind.
package worktrees
