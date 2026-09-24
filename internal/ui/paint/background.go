package paint

import (
	"image/color"
	"sync/atomic"
)

// lightBackground is whether the terminal the panel is drawn in has a light background.
var lightBackground atomic.Bool

// SetDarkBackground records the background the terminal reported.
func SetDarkBackground(dark bool) {
	lightBackground.Store(!dark)
}

// Adaptive is a colour with one value for a light background and another for a dark one.
type Adaptive struct {
	Light color.Color
	Dark  color.Color
}

// RGBA is the value for the background the terminal last reported.
func (a Adaptive) RGBA() (r, g, b, alpha uint32) {
	if lightBackground.Load() {
		return a.Light.RGBA()
	}

	return a.Dark.RGBA()
}
