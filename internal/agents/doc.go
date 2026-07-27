// Package agents runs background Claude agents. It holds three services over the
// store and tmux: AgentService for an agent's lifecycle, WorkflowService for the
// buckets agents are grouped into, and Launcher for the command line that starts
// one. Reconciler and TranscriptSyncer are the background workers serve runs on a
// timer. Each declares only the store and tmux methods it actually uses.
package agents
