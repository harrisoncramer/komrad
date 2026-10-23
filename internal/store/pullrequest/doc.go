// Package pullrequest stores the pull requests komrad reads from its forges. Beside each pull
// request it keeps the tasks it was seen with, its merge readiness, the status checks on its head
// with their logs and failures, and the review threads the forge holds on it.
//
// The package also records which repositories the pull request sync has read, and finds the
// children that left their parent while their pull request still targets the parent's branch.
package pullrequest
