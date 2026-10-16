package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"komrad/internal/ui/layout"
)

const editorFailedSaid = "the editor on the engine failed, press enter to close"

// fileEditorContent opens a file the agents work on, on the engine when it is another machine.
func (m *model) fileEditorContent(dir, path string, line int) layout.Content {
	if m.settings.SSHHost == "" {
		return editorContent(m.settings.Editor, dir, path, line)
	}

	return layout.Content{
		Key:    fmt.Sprintf("edit:%s:%d", path, line),
		Render: renderShell(remoteEditorShell(m.settings, filepath.Join(dir, path), line)),
	}
}

// agentDirEditorContent opens an agent's directory, on the engine when it is another machine.
func (m *model) agentDirEditorContent(dir string) layout.Content {
	if m.settings.SSHHost == "" {
		return dirEditorContent(m.settings.Editor, dir)
	}

	return layout.Content{
		Key:    "edit:" + dir,
		Render: renderShell(remoteDirEditorShell(m.settings, dir)),
	}
}

func renderShell(line string) func(layout.Target) ([]string, bool) {
	return func(layout.Target) ([]string, bool) { return []string{"sh", "-c", line}, true }
}

// remoteEditorShell is the shell line that opens a file on the engine, on one line of it.
func remoteEditorShell(settings PanelSettings, path string, line int) string {
	command := editorCommand(settings.Editor)
	at := strings.Join(editorLineArgs(command, path, line), " ")

	switch editorName(command) {
	case "code", "code-insiders", "codium", "vscodium", "cursor", "windsurf":
		return "exec " + command + " --remote " + shellQuote("ssh-remote+"+settings.SSHHost) + " " + at
	case "zed":
		return "exec " + command + " --wait " + shellQuote("ssh://"+settings.SSHHost+path)
	}

	return sshEditorShell(settings, "exec "+engineEditorCommand(command)+" "+at)
}

// remoteDirEditorShell is the shell line that opens a directory on the engine.
func remoteDirEditorShell(settings PanelSettings, dir string) string {
	command := editorCommand(settings.Editor)

	switch editorName(command) {
	case "code", "code-insiders", "codium", "vscodium", "cursor", "windsurf":
		return "exec " + command + " --remote " + shellQuote("ssh-remote+"+settings.SSHHost) + " " + shellQuote(dir)
	case "zed":
		return "exec " + command + " " + shellQuote("ssh://"+settings.SSHHost+dir)
	}

	return sshEditorShell(settings, "exec "+engineEditorCommand(command)+" "+shellQuote(dir))
}

// sshEditorShell runs a terminal editor on the engine through its ssh host.
func sshEditorShell(settings PanelSettings, remote string) string {
	return "ssh -F " + shellQuote(settings.SSHConfig) + " -t " + shellQuote(settings.SSHHost) + " " + shellQuote(remote) +
		" || { printf '\\n%s\\n' " + shellQuote(editorFailedSaid) + "; read -r _; }"
}

// engineEditorCommand is the editor as the engine finds it, by the name of its program rather than
// where it lives on this machine.
func engineEditorCommand(command string) string {
	fields := strings.Fields(command)
	fields[0] = filepath.Base(fields[0])

	return strings.Join(fields, " ")
}

// isRemoteEditRefused returns why the edit keys cannot open the engine's files, before the panel
// stops an agent for them.
func (m *model) isRemoteEditRefused() (string, bool) {
	if m.settings.SSHHost == "" {
		return "", false
	}

	switch name := editorName(editorCommand(m.settings.Editor)); name {
	case "subl", "sublime_text", "goland", "idea", "pycharm", "webstorm", "phpstorm", "rubymine",
		"clion", "rider", "rustrover", "datagrip":
		return name + " cannot open files on another machine" + separated +
			"set EDITOR to one that can, such as nvim or code", true
	}

	if _, err := os.Stat(m.settings.SSHConfig); err != nil {
		return "this machine has no ssh host for the engine" + separated + "run komrad login to write one", true
	}

	return "", false
}
