// Package render holds what the handlers' resource renderers have in common.
package render

// LoadOption sets one field on a renderer's own options struct, so a renderer takes how much to
// draw in its own vocabulary rather than in the request's.
type LoadOption[T any] func(*T)

// SummaryHead is how much of a long free-text field a summary listing returns.
const SummaryHead = 200

// PromptHeaded is an options struct that takes how much of a prompt to draw.
type PromptHeaded[T any] interface {
	*T
	SetPromptHead(limit int)
}

// WithPromptHead draws the opening of the prompt rather than the whole of it.
func WithPromptHead[T any, PT PromptHeaded[T]](limit int) LoadOption[T] {
	return func(o *T) { PT(o).SetPromptHead(limit) }
}
