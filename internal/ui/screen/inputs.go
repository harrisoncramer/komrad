package screen

import (
	"komrad/internal/assemble"
	"komrad/internal/ui/panel"
)

// Inputs is what the root hands a screen each time it asks the screen something.
type Inputs struct {
	Listing panel.Listing
	Room    Room
	// WorktreeInspections is what the server answered for each worktree directory.
	WorktreeInspections map[string]panel.WorktreeInspection
	// IsActive is whether this screen is the one up.
	IsActive bool
}

// ListingChanged tells every screen the listing changed, carrying the listing it replaced.
type ListingChanged struct {
	PreviousListing panel.Listing
	Changed         assemble.Changed
}

// WithPreviousListing is the inputs as they stood before the listing changed.
func (r ListingChanged) WithPreviousListing(in Inputs) Inputs {
	in.Listing = r.PreviousListing

	return in
}
