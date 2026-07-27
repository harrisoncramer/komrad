// Package agents runs background Claude agents. It holds three services over the
// store and tmux. AgentService owns an agent's lifecycle, WorkflowService the
// buckets agents are grouped into, and Launcher the command line that starts one.
// Reconciler and TranscriptSyncer are the background workers serve runs on a
// timer. Each declares only the store and tmux methods it uses.
package agents
