package ui

import (
	"komrad/internal/ui/panel"

	tea "charm.land/bubbletea/v2"
)

// readWorktreeCmd asks the server for one directory's standing, once at a time.
func (m *model) readWorktreeCmd(dir string) tea.Cmd {
	last, asked := m.inspections[dir]
	if asked && last.Pending {
		return nil
	}

	pending := last
	pending.Pending = true
	m.applyWorktreeInspection(worktreeInspectedMsg{dir: dir, inspection: pending})
	clients, clock := m.api, m.clock

	return func() tea.Msg {
		read, err := clients.GetWorktree(dir)
		if err != nil {
			// The last answer stays on the row with the refusal beside it.
			last.Answered, last.ReadAt, last.Err = true, clock.Now(), err

			return worktreeInspectedMsg{dir: dir, inspection: last}
		}

		return worktreeInspectedMsg{dir: dir, inspection: worktreeInspectionFrom(read, clock.Now())}
	}
}

// worktreeInspectedMsg is one directory's standing come back.
type worktreeInspectedMsg struct {
	dir        string
	inspection panel.WorktreeInspection
}

func (m *model) applyWorktreeInspection(msg worktreeInspectedMsg) {
	if m.inspections == nil {
		m.inspections = map[string]panel.WorktreeInspection{}
	}

	m.inspections[msg.dir] = msg.inspection
}

// forgetUnlistedInspections forgets the readings of directories that have left the listing.
func (m *model) forgetUnlistedInspections() {
	listedDirs := make(map[string]bool, len(m.listing.Worktrees))
	for _, item := range m.listing.Worktrees {
		listedDirs[item.Dir] = true
	}
	for dir := range m.inspections {
		if !listedDirs[dir] {
			delete(m.inspections, dir)
		}
	}
}
