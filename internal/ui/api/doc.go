// Package api holds the panel's connection to komrad. It keeps a client for each of komrad's
// services, runs each call under its own deadline, and holds the calls the screens make.
//
// The package also builds the writes that more than one screen sends. These archive or delete
// tasks, send a name typed over a row, and hold a write once it lands until the panel shows what it
// did. A write that only one screen sends lives in that screen's package.
//
// The root and every screen domain import api. It imports only the shared packages below it, and
// never the root or a domain.
package api
