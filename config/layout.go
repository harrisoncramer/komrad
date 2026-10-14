package config

import (
	"fmt"
	"math"
	"slices"
	"strings"
)

// The layout the panel lays itself out at when the config names none. The info pane opens
// tall enough to read a log in without pushing the agent above it off the screen.
const (
	defaultSidebarSide   = sidebarSideLeft
	DefaultSidebarWidth  = 47
	defaultInfoSide      = infoSideBelow
	DefaultInfoHeight    = 0.4
	defaultDetailsHeight = 9
	defaultTriageRows    = 3
)

// The places each region of the panel's layout can sit.
const (
	sidebarSideLeft  = "left"
	SidebarSideRight = "right"
	infoSideBelow    = "below"
	InfoSideAbove    = "above"
)

// Layout is how the panel divides its window between its own list, the details under that
// list, the stage beside them, and the info pane that opens under the stage. A size below
// one is read as a share of what it divides, and one at or above it as a count of cells.
type Layout struct {
	// Which side of the window the panel's sidebar sits on, "left" or "right".
	SidebarSide string `mapstructure:"sidebar_side"`
	// The width, in columns, the panel's sidebar opens at. Dragging the divider afterwards moves it, and the panel follows.
	SidebarWidth int `mapstructure:"sidebar_width"`
	// Which side of the stage the info pane opens on, "below" or "above". That pane is where the server log and an editor are drawn.
	InfoSide string `mapstructure:"info_side"`
	// The height the info pane opens at, either a share of the stage's height below one or a whole number of rows.
	InfoHeight float64 `mapstructure:"info_height"`
	// The number of lines the details under the list draw, either a share of the sidebar's height below one or a whole number of lines. The marks the hovered row is carrying are spelled out in that same room, ahead of the fields. The chrome around them is added on top, and the two together never take more than half the sidebar's height. Once the details hold the keys they grow past this to show the whole record, as far as the sidebar allows.
	DetailsHeight float64 `mapstructure:"details_height"`
	// The number of tasks the triage zone at the top of the agents list draws, the agents most in need of you, the most urgent first. A task is drawn as its writer with its reviewers under it, and the zone never draws fewer rows than it has slots. Zero turns the zone off.
	TriageRows int `mapstructure:"triage_rows"`
}

// DefaultLayout is the layout used when the config omits it.
func DefaultLayout() Layout {
	return Layout{
		SidebarSide:   defaultSidebarSide,
		SidebarWidth:  DefaultSidebarWidth,
		InfoSide:      defaultInfoSide,
		InfoHeight:    DefaultInfoHeight,
		DetailsHeight: defaultDetailsHeight,
		TriageRows:    defaultTriageRows,
	}
}

func (l *Layout) normalize() {
	if l.SidebarWidth == 0 {
		l.SidebarWidth = DefaultSidebarWidth
	}
	if l.InfoHeight == 0 {
		l.InfoHeight = DefaultInfoHeight
	}
	if l.DetailsHeight == 0 {
		l.DetailsHeight = defaultDetailsHeight
	}
	if strings.TrimSpace(l.SidebarSide) == "" {
		l.SidebarSide = defaultSidebarSide
	}
	if strings.TrimSpace(l.InfoSide) == "" {
		l.InfoSide = defaultInfoSide
	}
}

// validate rejects a layout tmux could not lay the panes out at. Zero is how an omitted
// size reads, so it falls back to the default rather than being called a mistake.
func (l Layout) validate() error {
	if err := nonNegative("layout.sidebar_width", l.SidebarWidth, "a width"); err != nil {
		return err
	}
	if err := nonNegative("layout.triage_rows", l.TriageRows, "a number of tasks"); err != nil {
		return err
	}
	if err := validatePaneSize("layout.info_height", l.InfoHeight); err != nil {
		return err
	}
	if err := validatePaneSize("layout.details_height", l.DetailsHeight); err != nil {
		return err
	}
	if err := validateSide("layout.sidebar_side", l.SidebarSide, sidebarSideLeft, SidebarSideRight); err != nil {
		return err
	}

	return validateSide("layout.info_side", l.InfoSide, infoSideBelow, InfoSideAbove)
}

// validatePaneSize rejects a size that is neither a share of what it divides nor a whole
// number of cells, since tmux is given one or the other and 1.5 columns is neither.
func validatePaneSize(key string, size float64) error {
	switch {
	case size < 0:
		return fmt.Errorf("%s: %v is not a size", key, size)
	case size >= 1 && size != math.Trunc(size):
		return fmt.Errorf("%s: %v is neither a share below one nor a whole number of cells", key, size)
	}

	return nil
}

// validateSide rejects a side that names neither of the two a region can sit on.
func validateSide(key, side string, allowed ...string) error {
	trimmed := strings.TrimSpace(side)
	if trimmed == "" || slices.Contains(allowed, trimmed) {
		return nil
	}

	return fmt.Errorf("%s: %q is not one of %s", key, side, strings.Join(allowed, ", "))
}

// Diff is how the panel draws a diff.
type Diff struct {
	// Wrap a line wider than the pane onto the lines under it rather than cutting it off. The wrap
	// key turns this off and on while reading.
	Wrap bool `mapstructure:"wrap"`
}
