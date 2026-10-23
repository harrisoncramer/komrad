// Package gitmenu builds the git menu a task's band nests, and the merge menu nested in it. The git
// menu rebases and pushes a task's worktree, aborts a rebase or merge left in progress, picks the
// fork point, marks the pull request a draft or ready, asks for a re-review, and opens or copies
// the pull request's link. The merge menu merges the pull request or turns auto merge on. A write
// that moves git plans first, asks the person to confirm, and then reports what came of it.
//
// The package has no screen. The root nests these menus wherever a band asks for git, hands the git
// results back to the package, and applies the requests its entries return. It never imports the
// root or another domain.
package gitmenu
