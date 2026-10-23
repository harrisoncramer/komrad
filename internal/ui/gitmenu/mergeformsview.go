package ui

import (
	"komrad/internal/ui/paint"
	forgev1 "komrad/proto/gen/forge/v1"
)

// mergeOptionLabel names a merge method, muted when the base branch does not allow it.
func mergeOptionLabel(option *forgev1.MergeOption) string {
	name := mergeMethodLabel(option.GetMethod())
	if option.GetAllowed() {
		return name
	}

	return paint.Panel.Style(paint.ToneMuted).Render(name + "  not allowed")
}
