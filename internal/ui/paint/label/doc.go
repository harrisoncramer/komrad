// Package label turns a count, a time, an age, a path or a byte size into the short text a row or a
// record shows, and puts a dash where a value is blank.
//
// The package holds no state and imports nothing under internal/ui. The panel's shared packages and
// screens import it, and so do the doctor and view packages outside the panel.
package label
