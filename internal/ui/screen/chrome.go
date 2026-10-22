package screen

import (
	"komrad/internal/ui/column"
	"komrad/internal/ui/legend"
)

// Chrome is the root's drawing a screen lays itself out under.
type Chrome interface {
	Bars() []string
	Help(help legend.Help, above ...string) string
	Legend(own legend.Help) legend.Help
	Record(above ...string) string
	Lines(above ...string) int
	Join(split column.Split, sidebar, block string) string
	Form() string
}
