// Package pullrequests keeps komrad's copy of each pull request in step with its forge, and
// changes pull requests through it.
//
// The [PullRequestSyncer] reads the repositories live agents work in, faster for a while after
// komrad changes something on a host. The [CommentSyncer] rereads the forge's review threads on
// the open pull requests agents are linked to. Each write records what moved as a [Change], and
// the change's event tells the agents on the branch. The [Writer] merges, sets draft and
// auto-merge, and asks for a re-review, and the [Lister] reads the stored pull requests, their
// checks and the checks' logs.
package pullrequests
