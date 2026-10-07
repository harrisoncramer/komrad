// Package paint is the panel's rendering primitives: colours, styles, markdown rendering, the
// arithmetic that fits a list into a pane, and the match markers a search hit carries.
//
// It holds no panel state and knows nothing about an agent, a workflow, or a schedule. Nothing
// here takes a model and nothing here reads the store, so what a screen renders is the screen's
// business and how it is rendered is this package's.
package paint
