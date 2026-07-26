// Package agents runs background Claude chats. It holds three services over the
// store and tmux: SessionService for a chat's lifecycle, WorkflowService for the
// buckets chats are grouped into, and Launcher for the command line that starts
// one. Reconciler and TranscriptSyncer are the background workers serve runs on a
// timer. Each declares only the store and tmux methods it actually uses.
package agents
