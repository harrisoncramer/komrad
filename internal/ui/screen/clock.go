package screen

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// Clock is the time the panel reads and the waits it starts.
type Clock interface {
	Now() time.Time
	// After is a command that answers with fire once wait has passed.
	After(wait time.Duration, fire func(time.Time) tea.Msg) tea.Cmd
}
