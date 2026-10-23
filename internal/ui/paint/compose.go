package paint

import "charm.land/lipgloss/v2"

// Compose renders layers stacked by their depth onto a canvas of the size given.
func Compose(width, height int, layers []*lipgloss.Layer) string {
	canvas := lipgloss.NewCanvas(width, height)
	canvas.Compose(lipgloss.NewCompositor(layers...))

	return canvas.Render()
}
