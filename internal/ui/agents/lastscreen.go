package agents

import (
	"komrad/config"
	"komrad/internal/glyph"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/screen"
)

const separated = " " + glyph.Separator + " "

func (a *archivedAgents) ScreenedAgentID(in screen.Inputs) (string, bool) {
	row, ok := a.hoveredRow(in)
	if !ok {
		return "", false
	}

	item, ok := row.paneItem()

	return item.Agent.ID, ok
}

// ScreenedAgentID is the dormant agent under the cursor, whose saved screen is rendered on the stage.
func (a *Screen) ScreenedAgentID(in screen.Inputs) (string, bool) {
	item, ok := a.Resting(in)

	return item.Agent.ID, ok
}

func (a *archivedAgents) ScreenNotice(in screen.Inputs) string {
	restore := a.deps.Keys.ArchiveMenu.Restore
	if target, ok := a.menuTarget(in); ok {
		if _, err := target.opensAgent(in.Listing); err == nil {
			restore = a.deps.Keys.ArchiveMenu.RestoreOpen
		}
	}

	return "archived" + separated + "press " + keys.MenuChordLabel(a.deps.Keys, a.deps.Keys.Menu.Archive, restore) + " to restore"
}

// ScreenNotice is the line over a dormant agent's saved screen.
func (a *Screen) ScreenNotice(in screen.Inputs) string {
	return "dormant" + separated + "press " + keys.Label(config.Fixed().Open) + " to restore"
}
