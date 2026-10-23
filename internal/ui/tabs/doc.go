// Package tabs holds the rows of stops over a list. The kinds row switches between the kinds a list
// shows, and the tab strip picks the column a list sorts by and the way it points. The package also
// sorts a list's rows by the column the strip chose.
//
// The root and the agents, checks, documents, logs, schedules and worktrees screens import tabs. It
// imports only the paint package under internal/ui.
package tabs
