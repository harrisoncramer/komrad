// Package keys holds the panel's key tables. A table lists the actions a screen answers in the
// order a key is offered to them, and builds the screen's legend from the same order. The package
// also holds the actions every list shares, the keys that walk a diff, and the fixed keys every
// screen, menu and form goes back, quits and opens a row on.
//
// The root checks the person's keybindings against every table and rejects a binding a screen could
// not dispatch. Each domain keeps its own table beside its screen. The root and every screen domain
// import keys. It imports only the shared packages below it, and never the root or a domain.
package keys
