// Package tmux wraps the tmux server komrad runs, so the rest of komrad names sessions, windows
// and panes instead of building shell arguments.
//
// Every agent lives in a pane on that server, and the panel is a client attached to it. Komrad
// creates an agent's window, captures the frame the reconciler reads, types into the pane, moves
// panes for the panel and kills them through this package. The socket is a field, so a test can
// point a [Client] at a throwaway server.
package tmux
