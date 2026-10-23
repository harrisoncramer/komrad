// Package screen is the contract between the root and the screens. It names the panel's screens and
// the row one opens on, and it holds every request a screen hands the root. A request asks the root
// to open a screen, send a write, put a pane on the stage, copy text or post a message.
//
// The package also holds the inputs a screen reads on each update, such as the listing, the room it
// has and the clock.
//
// The root and every screen domain import screen. It imports only the column, layout, legend, list
// and panel packages, and never the root or a domain.
package screen
