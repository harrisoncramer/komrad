// Package review stores reviews and the snapshots they are pinned to. It keeps the snapshots of
// a task's worktree and which of them have landed, the reviews on each snapshot, the threads on
// lines of the diff with the comments in them, and the files the person has marked viewed.
//
// The package also holds the threads komrad mirrors from the forge, and each reply komrad posts
// back to the forge from the time it is written until it is sent or fails.
package review
