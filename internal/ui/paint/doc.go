// Package paint is the panel's drawing primitives: colours, styles, markdown rendering, the
// arithmetic that fits a list into a pane, and the match markers a search hit carries.
//
// It holds no panel state and knows nothing about an agent, a workflow, or a schedule. Nothing
// here takes a model and nothing here reads the store, so what a screen draws is the screen's
// business and how it is drawn is this package's.
package paint
