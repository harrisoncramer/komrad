// Package layout owns the panes the panel renders across, and turns the regions the panel asks for
// into splits on komrad's tmux server.
//
// The panel is one pane among several. The panel renders the sidebar itself, and beside it sit the
// stage that pages a file or a diff, the info pane, and the guest pane an attached agent takes
// over. This package decides where those go and which of them are open, so a screen asks for a
// region and never for a pane id. When the agents run on an engine this tmux server can't reach,
// the package opens a shell or an agent's terminal there instead.
//
// The root, the editor, the screen package and the review screen import layout. It imports nothing
// under internal/ui.
package layout
