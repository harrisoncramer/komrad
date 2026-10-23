// Package cursor keeps a cursor inside a list of lines and finds the window of lines around it. It
// clamps, steps and lands the cursor, and places the window so the cursor stays in view even when
// one line takes several rows.
//
// The package works on indexes and heights alone. It knows nothing about rows, screens or styles,
// and it imports nothing under internal/ui. The list, the reader, the diff view, the log view and
// the screens build on it.
package cursor
