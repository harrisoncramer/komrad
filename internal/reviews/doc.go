// Package reviews holds the rules for reviewing an agent's work. It owns snapshots, threads and
// comments, the reviews reviewers release, and the person's replies on the forge.
//
// A snapshot freezes a task's checkout as a commit, and a thread pins a conversation to a place
// in one. Only a comment's author may edit it, and only the person marks threads read and files
// viewed. A reviewer releases its review as one file, and the writer gets mail with its path.
// Threads follow the work to newer snapshots through the history package.
//
// A comment from the forge waits until the person accepts it as their own. A reply the person
// posts to the forge is recorded first, so one komrad stopped partway through posting can be
// retried, discarded or settled later.
package reviews
