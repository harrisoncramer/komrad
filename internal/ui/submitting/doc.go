// Package submitting renders the writes the panel has sent and is waiting on. It renders the notice
// under a held form, with a spinner, the time the write has taken, and a hint to keep working. It
// also renders the newest write still out as a badge in the sidebar's corner.
//
// The root imports submitting. It imports only the paint package under internal/ui.
package submitting
