// Package position moves a place in one commit to the place it has become in a later one. Reviews
// use it to draw a comment written against an old snapshot on a newer snapshot of the same work.
//
// Both snapshots are commits komrad still holds, so git's diff between them says where each line
// went. [Transform] settles renames and copies first, so the line walk never has to know a file
// moved, and a file copied to two paths turns one position into two results. The walk then
// carries a running shift down the file. A change before a position moves it, and lines added
// inside a position make it longer instead of moving its start.
//
// A [Range] holds its start line but not its end, so an empty range is a point between two lines.
// Lines count from one, and a position is never finer than a line. When a later commit rewrites a
// position's lines, the caller's policy decides the result. [FileOnConflict] keeps the file for a
// comment, and [OmitOnConflict] drops the position for anything that would mislead.
//
// The design follows Gerrit's GitPositionTransformer, and like it, the package knows nothing of
// comments or of the rest of komrad.
package position
