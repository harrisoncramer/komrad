package ui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// paneWidths are the columns the sweeps measure at, spanning the width where a screen gives up
// drawing a block beside its list and falls back to the one column.
var paneWidths = []int{20, 30, 40, 60, 61, 62, 80, 200}

// screenChromeFloor is the shortest pane each screen's own chrome fits in, which is where the
// budget arithmetic starts having an answer to give.
var screenChromeFloor = map[screen]int{
	screenAgents:    5,
	screenSchedules: 5,
	screenArchive:   5,
	screenSearch:    6,
	screenMail:      6,
}

// reviewChromeFloor is the shortest pane the review's own chrome fits in, which is taller than
// any list's because its band carries the stat line and the filter as well as a title.
const reviewChromeFloor = 7

// frameHeight is a screen's own block measured the way View passes it on, which trims the
// newline closing its last row rather than counting it as another.
func frameHeight(view string) int {
	return strings.Count(strings.TrimRight(view, "\n"), "\n") + 1
}

// TestNoScreenOutgrowsItsPane sweeps width and height together against every list screen, the
// pane tests that only ever swept height being why the review's second layout went unmeasured.
func TestNoScreenOutgrowsItsPane(t *testing.T) {
	for s, floor := range screenChromeFloor {
		for _, width := range paneWidths {
			for pane := floor; pane <= 40; pane++ {
				m := driftModel(t, s)
				m.width, m.height = width, pane

				height := frameHeight(m.screen.spec().view(m))
				assert.LessOrEqual(t, height, pane,
					"screen %d drew %d lines into a %d line pane at width %d", s, height, pane, width)
			}
		}
	}
}

// TestChromeFloorsAreWhatTheScreensDraw derives each floor from the screens themselves rather
// than trusting the constant, so a screen that gains a header row fails here instead of quietly
// widening the band the sweep above starts past.
func TestChromeFloorsAreWhatTheScreensDraw(t *testing.T) {
	for s, floor := range screenChromeFloor {
		for _, width := range paneWidths {
			drawn := func(pane int) int {
				m := driftModel(t, s)
				m.width, m.height = width, pane

				return frameHeight(m.screen.spec().view(m))
			}

			assert.Equal(t, floor, derivedChromeFloor(drawn), "screen %d at width %d", s, width)

			// Under the floor there is nothing left to give up, so the frame stands at the chrome's
			// own height rather than growing with the pane it cannot fit in.
			for pane := 1; pane < floor; pane++ {
				assert.Equal(t, floor, drawn(pane), "screen %d at width %d in a %d line pane", s, width, pane)
			}
		}
	}
}

// derivedChromeFloor is the shortest pane a screen's own frame fits in, found by walking up from
// a pane of one row until the frame stops overflowing.
func derivedChromeFloor(drawn func(pane int) int) int {
	for pane := 1; pane <= 40; pane++ {
		if drawn(pane) <= pane {
			return pane
		}
	}

	return 0
}

// TestFitFrameCutsAFrameTallerThanThePane covers the floor under every screen's own budget: a
// frame taller than its pane is cut rather than handed to bubbletea, which scrolls the top of it
// away.
func TestFitFrameCutsAFrameTallerThanThePane(t *testing.T) {
	m := model{width: 10, height: 3}

	require.Equal(t, "one\ntwo\nthree", m.fitFrame("one\ntwo\nthree\nfour\nfive"))
}
