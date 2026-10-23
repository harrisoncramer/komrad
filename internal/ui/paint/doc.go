// Package paint holds the panel's rendering primitives. These are the palette and its tones, the
// styles, markdown rendering, the arithmetic that fits a list of rows into a pane, and the spans a
// search hit marks.
//
// The package holds no panel state and knows nothing about an agent, a workflow or a schedule. No
// function in it takes a model or reads the store. A screen decides what to render, and paint
// decides how it looks.
//
// Every package under internal/ui may import paint, and so do a few packages outside the panel,
// such as the cli and setup packages. Paint imports nothing under internal/ui.
package paint
