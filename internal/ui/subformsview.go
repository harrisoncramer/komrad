package ui

import (
	"strings"

	"komrad/internal/ui/paint"
	"komrad/lib/xslice"

	"charm.land/huh/v2"
	"github.com/charmbracelet/x/ansi"
)

// renderedGray is a form's lines as it stands, without its help, rendered gray.
func renderedGray(form *huh.Form) []string {
	view := form.WithShowHelp(false).View()

	lines := xslice.Map(strings.Split(ansi.Strip(view), "\n"), func(line string) string {
		return strings.TrimRight(line, " ")
	})
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	muted := paint.Panel.Style(paint.ToneMuted)

	return xslice.Map(lines, func(line string) string { return muted.Render(line) })
}
