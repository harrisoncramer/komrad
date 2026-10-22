package screen

// Crumbs is the trail from a view down to the row or page the cursor is on.
type Crumbs struct {
	Trail []string
	// Tally is said after the trail.
	Tally string
}
