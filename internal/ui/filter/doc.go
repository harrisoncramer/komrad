// Package filter holds the search line a screen narrows or walks its rows by. In one mode the line
// hides the rows it doesn't match, and in the other it jumps between the rows it does. The line
// waits for typing to rest before it compiles a pattern, and it reads the qualifiers of the list's
// grammar. It queues the text of a says qualifier for the root to send to komrad, and keeps the
// hits that come back.
//
// The package also renders the line and the rows over a block it searches.
//
// The root, the key tables and the screens that search import filter. It imports only the shared
// packages below it, and never the root or a domain.
package filter
