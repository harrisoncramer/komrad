// Package diffview flattens an agent's diff into rows the panel can render one at a time. It pins
// each review thread to the line it was written against, and traces that line across snapshots so a
// thread stays put as the diff moves. It highlights the syntax of each file and builds the file
// tree with a count of threads on each file.
//
// The package also holds the pane that walks those rows, with its cursor, search, expanded threads
// and sideways pan.
//
// The review and documents screens and the key tables import diffview. It imports only the shared
// packages below it, and never the root or a domain.
package diffview
