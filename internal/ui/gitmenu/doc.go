// Package gitmenu builds the git menu a task's band nests, and the forge menu nested in it. The git
// menu rebases and pushes a task's worktree, aborts a rebase or merge left in progress, and picks
// the fork point. The forge menu opens or copies the pull request's link, marks it a draft or
// ready, asks for a re-review, merges it, and turns auto-merge on or off. Each write that changes
// git first makes a plan, asks the person to confirm, and then reports what came of it.
//
// The package doesn't have a screen. The root nests these menus wherever a band asks for git,
// hands the git results back to the package, and applies the requests its entries return. It never
// imports the root or another domain.
package gitmenu
