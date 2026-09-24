package ui

// worktreesHelp is the worktrees legend as the frame draws it.
func (m model) worktreesHelp() keyMap {
	return m.listHelpOf(m.worktrees.listParams(m.shared()))
}
