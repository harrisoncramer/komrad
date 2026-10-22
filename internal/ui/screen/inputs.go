package screen

import "komrad/internal/ui/panel"

// Inputs is what the root hands a screen each time it asks the screen something.
type Inputs struct {
	Listing panel.Listing
	Room    Room
	// Inspections is what the server answered for each worktree directory.
	Inspections map[string]panel.WorktreeInspection
	// Up is whether this screen is the one up.
	Up bool
}

// Relisted tells every screen the listing changed, carrying the listing it replaced.
type Relisted struct {
	Was panel.Listing
}

// Before is the inputs as they stood before the listing changed.
func (r Relisted) Before(in Inputs) Inputs {
	in.Listing = r.Was

	return in
}
