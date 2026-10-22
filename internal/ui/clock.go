package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

type wallClock struct{}

func (wallClock) Now() time.Time {
	return time.Now()
}

func (wallClock) After(wait time.Duration, fire func(time.Time) tea.Msg) tea.Cmd {
	return tea.Tick(wait, fire)
}
