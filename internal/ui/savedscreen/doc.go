// Package savedscreen fits a captured pane screen to a terminal of another size. When the screen is
// too tall, it collapses the longest run of blank lines first.
//
// The root uses it to render the saved screen of an agent that has stopped, and the pane picker
// uses its tab expansion. It imports nothing under internal/ui.
package savedscreen
