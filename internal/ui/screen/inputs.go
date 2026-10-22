package screen

import "komrad/internal/ui/panel"

// Inputs is what the root hands a screen each time it asks the screen something.
type Inputs struct {
	Listing panel.Listing
	Holds   Holds
	// Inspections is what the server answered for each worktree directory.
	Inspections map[string]panel.WorktreeInspection
	// Up is whether this screen is the one up.
	Up bool
}

// Holds is the files a reader edited over each paused agent's worktree, keyed by agent.
type Holds map[string][]string
