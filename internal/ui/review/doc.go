// Package review holds the screen that reads one agent's diff beside its file tree and the threads
// written on it. A person writes comments and replies on lines of the diff, replies to the forge's
// comments, and accepts the forge comments komrad holds back. The screen reads the diff between any
// two snapshots, shows the pull request in a pane of its own, and opens a thread in a popup.
//
// The screen can also open the person's editor over the agent's worktree on the stage, and tells
// the agent which files the person opened once the editor closes.
//
// The root is the host. It opens the screen and applies the requests it returns. The review screen
// reaches the host only through those requests, and it never imports the root or another domain.
package review
