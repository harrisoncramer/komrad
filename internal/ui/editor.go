package ui

import (
	"os/exec"

	tea "charm.land/bubbletea/v2"
)

// Editor opens a file in the person's editor and answers once they close it.
type Editor interface {
	Edit(path string, closed func(error) tea.Msg) tea.Cmd
}

// shellEditor runs the configured editor in the panel's own terminal, on a file's first line.
type shellEditor struct {
	command string
}

func (e shellEditor) Edit(path string, closed func(error) tea.Msg) tea.Cmd {
	//nolint:noctx // the editor runs until the person closes it, so it takes no deadline
	open := exec.Command("sh", "-c", editorShell(e.command, path, 1))

	return tea.ExecProcess(open, closed)
}
