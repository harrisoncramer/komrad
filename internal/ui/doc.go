// Package ui is the panel's host. It runs one bubbletea program in its own tmux session, doesn't
// hold a database connection, and knows only what komrad has told it.
//
// The host builds the model and every screen in it. It opens the watch stream and hands each
// listing to the screens. It offers each key first to the leader, the pane picker and any open form
// or menu, then to the global keys, and last to the screen that is up. A screen answers with
// requests, and the host applies them. The host sends each write to komrad off the update loop,
// holds a form while its write is in flight, and settles the write once a listing shows it landed.
// It puts panes on the stage and renders the record and the legend under the list.
//
// The host also keeps the pieces that every domain uses but none owns. These are the form host and
// its subforms, the doc reader, the stack of open menus, the jump menu, the pane picker, click
// dispatch and the messages list.
//
// Each screen domain lives in a package of its own under internal/ui, and the shared packages sit
// below the domains. The root imports every domain, and paneltest is the only package under
// internal/ui that imports the root. The cli package starts the panel through the root, and the
// root's stories drive it through paneltest.
package ui
