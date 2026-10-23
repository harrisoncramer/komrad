package ui

import (
	"komrad/internal/ui/paint"

	"charm.land/glamour/v2/ansi"
)

// columnStyle is the markdown the block beside the list is rendered in.
func columnStyle() ansi.StyleConfig {
	return paint.Panel.Stage()
}
