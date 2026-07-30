// Package agents runs the background agents. It holds three services over the
// store and tmux. AgentService owns an agent's lifecycle, WorkflowService the
// buckets agents are grouped into, and Launcher the command line that starts one.
// Reconciler and TranscriptSyncer are the background workers serve runs on a
// timer. Each declares only the store, tmux, and harness methods it uses.
//
// Nothing here knows which CLI an agent runs. What its command line looks like,
// what its live UI says it is doing, and what its conversation log holds all sit
// behind the interfaces in internal/harness.
package agents
