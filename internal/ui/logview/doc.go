// Package logview holds log lines and renders the newest of them, or the ones around a cursor. It
// caps the lines it keeps, wraps long ones to the pane, and renders a line of komrad's own server
// log in a compact form.
//
// The logs screen imports logview. It imports only the cursor and paint packages, and never the
// root or a domain.
package logview
