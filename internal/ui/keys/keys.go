// Package keys is the panel's key tables: the actions a screen answers, the order a press is
// offered to them, and the legend that names them.
//
// It holds no panel state. A table is generic over what its arms read, so a screen hands its own
// state to the dispatch and the legend, and the root checks every screen's bindings for conflicts
// from the arms the tables describe.
package keys

import (
	"slices"
	"strings"

	"komrad/config"
)

// Probe is the seam for asking which bindings a press reached, and is nil unless a test sets it.
var Probe func(keys []string)

// IsBound reports whether the press is one of the keys.
func IsBound(pressed string, keys []string) bool {
	if !slices.Contains(keys, pressed) {
		return false
	}

	if Probe != nil {
		Probe(keys)
	}

	return true
}

// DirectionOf is back for a press of the backward keys and forward for any other.
func DirectionOf(pressed string, backward []string) int {
	if IsBound(pressed, backward) {
		return -1
	}

	return 1
}

// FixedBack is the fixed key every screen, menu and form goes back on.
func FixedBack(config.Keybindings) []string {
	return config.Fixed().Back
}

// FixedQuit is the fixed key every screen, menu and form quits on.
func FixedQuit(config.Keybindings) []string {
	return config.Fixed().Quit
}

// FixedOpen is the fixed key every screen, menu and form opens the hovered row on.
func FixedOpen(config.Keybindings) []string {
	return config.Fixed().Open
}

var abbreviations = strings.NewReplacer("ctrl+", "C-", "shift+", "S-")

// Label is the keys as the legend spells them.
func Label(keys []string) string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = abbreviations.Replace(k)
	}

	return strings.Join(out, "/")
}

// HeadLabel is the first of the keys as the legend spells it.
func HeadLabel(keys []string) string {
	if len(keys) == 0 {
		return ""
	}

	return Label(keys[:1])
}

// doubledLabel is each of the keys spelled twice, as a key pressed twice in a row is named.
func doubledLabel(keys []string) string {
	pairs := make([]string, len(keys))
	for i, k := range keys {
		label := Label([]string{k})
		pairs[i] = label + label
	}

	return strings.Join(pairs, "/")
}
