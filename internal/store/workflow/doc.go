// Package workflow stores workflows. A workflow holds the directory, harness, flags and context
// its agents launch with, the harness and flags of its reviewers, and its worktree settings. It
// also holds the hooks the workflow runs at each moment and the prompts it adds to the events
// komrad sends its agents.
package workflow
