// Package view is the agents list drawn in three steps: a snapshot of what the panel found, a
// pure preparation of that snapshot into resolved glyphs and tones, and a pure rendering of
// those into a string.
//
// Nothing here holds a client, a clock, a terminal or a context. A Snapshot is plain data the
// panel gathers; Prepare makes every decision about which glyph, which tone and which weight a
// row wears; Render spells those decisions out with the palette of the region it is drawn in.
// The split is what lets a change of logic fail a readable assertion and a change of palette
// move only a golden file.
package view
