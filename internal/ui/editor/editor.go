// Package editor is the person's editor: how the panel opens a file or a directory in it, here or
// on the engine.
package editor

import (
	"os/exec"

	tea "charm.land/bubbletea/v2"
)

// Editor opens a file in the person's editor and answers once they close it.
type Editor interface {
	Edit(path string, closed func(error) tea.Msg) tea.Cmd
}

// Shell runs the configured editor in the panel's own terminal, on a file's first line.
type Shell struct {
	Command string
}

// Edit runs the editor on the file until the person closes it.
func (e Shell) Edit(path string, closed func(error) tea.Msg) tea.Cmd {
	//nolint:noctx // the editor runs until the person closes it, so it takes no deadline
	open := exec.Command("sh", "-c", editorShell(e.Command, path, 1))

	return tea.ExecProcess(open, closed)
}
