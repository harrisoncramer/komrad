// Package loader is the spinner the panel draws while something is loading.
package loader

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"komrad/lib/xslice"
)

// Style names a set of frames a loader cycles through.
type Style string

const (
	Braille Style = "braille"
	Arcs    Style = "arcs"
	ASCII   Style = "ascii"
)

// Interval is how long each frame of a loader stays on screen.
const Interval = 90 * time.Millisecond

var frames = map[Style][]string{
	Braille: {"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
	Arcs:    {"◜", "◝", "◞", "◟"},
	ASCII:   {"|", "/", "-", `\`},
}

// Styles is every style a loader may be drawn in.
func Styles() []Style {
	return []Style{Braille, Arcs, ASCII}
}

// Parse is the style a config names, refusing one the panel cannot draw.
func Parse(name string) (Style, error) {
	style := Style(strings.TrimSpace(name))
	if slices.Contains(Styles(), style) {
		return style, nil
	}

	names := xslice.Map(Styles(), func(s Style) string { return string(s) })

	return "", fmt.Errorf("%q is not one of %s", name, strings.Join(names, ", "))
}

// Frames is every frame the style cycles through, in order.
func (s Style) Frames() []string {
	return frames[s]
}

// Frame is the frame the style draws at the given step.
func (s Style) Frame(step int) string {
	drawn := s.Frames()
	if len(drawn) == 0 {
		return ""
	}

	return drawn[((step%len(drawn))+len(drawn))%len(drawn)]
}
