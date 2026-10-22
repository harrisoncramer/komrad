package list

import (
	"komrad/internal/view"
)

// Pane is the column that renders a list this frame and where its cursor stands.
type Pane struct {
	Width int
	Caret view.Caret
}
