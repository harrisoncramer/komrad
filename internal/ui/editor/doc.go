// Package editor opens a file or a directory in the person's editor. The editor runs either in the
// panel's own terminal or as content on the stage. When the engine is another machine, the package
// opens the files there through the engine's ssh host, and says why when the person's editor can't
// reach them.
//
// The root and the agents, documents and review screens import editor. It imports only the layout
// package under internal/ui, and never the root or a domain.
package editor
