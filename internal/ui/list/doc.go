// Package list keeps a cursor over a list of rows, the rows a person selects in it, and the pane
// the list renders in. It also holds the grammar a list's search reads and the hits that come back
// for a says qualifier.
//
// The root renders each screen's list from the parameters a screen hands it. The root, the screen
// package, the filter, the key tables and the screen domains import list. It imports only the
// cursor, legend and paint packages, and never the root or a domain.
package list
