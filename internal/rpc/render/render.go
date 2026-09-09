// Package render holds what the handlers' resource renderers have in common, which is how a
// caller says how much of each record it wants drawn.
package render

// LoadOption sets one field on a renderer's own options struct, so a renderer takes how much to
// draw in its own vocabulary rather than in the request's.
type LoadOption[T any] func(*T)

// SummaryHead is how much of a long free-text field a summary listing returns.
const SummaryHead = 200
