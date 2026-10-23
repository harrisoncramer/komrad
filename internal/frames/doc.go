// Package frames records the pane captures behind each agent's reconciled state, when the
// config turns recording on.
//
// A pane redraws over the frame a wrong state was read from, so a misread can't be debugged
// without a recording. Each capture is written exactly as the harness read it, so it can be
// replayed through the frame reader that misjudged it.
package frames
