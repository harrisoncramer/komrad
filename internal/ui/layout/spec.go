package layout

import (
	"math"

	"weave/config"
)

// Spec is how much of the window each region takes, resolved from the user's config once so
// nothing further down has to know what an omitted share means.
type Spec struct {
	// nav is the share of the window's width the nav region takes.
	nav float64
	// bottom is the share of the main region's height the bottom region takes.
	bottom float64
}

// NewSpec resolves a configured layout into the geometry the frame lays panes out at, falling
// back to the defaults for a share no window could be divided at.
func NewSpec(layout config.Layout) Spec {
	spec := Spec{nav: layout.Split, bottom: layout.SecondarySplit}

	if spec.nav <= 0 || spec.nav >= 1 {
		spec.nav = config.DefaultSplit
	}
	if spec.bottom <= 0 || spec.bottom >= 1 {
		spec.bottom = config.DefaultSecondarySplit
	}

	return spec
}

// getMainPercent is what tmux is told when the main region is split off the nav one. A split
// sizes the pane it creates, so the nav region's share reaches tmux as the remainder of it.
func (s Spec) getMainPercent() int {
	return int(math.Round((1 - s.nav) * 100))
}

// getBottomPercent is the share of the main region the bottom one is split at.
func (s Spec) getBottomPercent() int {
	return int(math.Round(s.bottom * 100))
}
