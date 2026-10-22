package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"komrad/internal/glyph"
	"komrad/internal/ui/layout"
)

const (
	editorFailedSaid = "the editor on the engine failed, press enter to close"
	separated        = " " + glyph.Separator + " "
)

// Settings is the editor the person set and the engine's ssh host it reaches another machine through.
type Settings struct {
	Command       string
	SSHHost       string
	SSHConfig     string
	IsSSHIncluded bool
}

// FileContent opens a file the agents work on, on the engine when it is another machine.
func (s Settings) FileContent(dir, path string, line int) layout.Content {
	if s.SSHHost == "" {
		return Content(s.Command, dir, path, line)
	}

	return layout.Content{
		Key:    fmt.Sprintf("edit:%s:%d", path, line),
		Render: renderShell(remoteEditorShell(s, filepath.Join(dir, path), line)),
	}
}

// DirContent opens an agent's directory, on the engine when it is another machine.
func (s Settings) DirContent(dir string) layout.Content {
	if s.SSHHost == "" {
		return dirEditorContent(s.Command, dir)
	}

	return layout.Content{
		Key:    "edit:" + dir,
		Render: renderShell(remoteDirEditorShell(s, dir)),
	}
}

func renderShell(line string) func(layout.Target) ([]string, bool) {
	return func(layout.Target) ([]string, bool) { return []string{"sh", "-c", line}, true }
}

// remoteEditorShell is the shell line that opens a file on the engine, on one line of it.
func remoteEditorShell(settings Settings, path string, line int) string {
	command := editorCommand(settings.Command)
	at := strings.Join(editorLineArgs(command, path, line), " ")

	switch name := editorName(command); {
	case isVSCodeEditor(name):
		return "exec " + command + " --remote " + shellQuote("ssh-remote+"+settings.SSHHost) + " " + at
	case name == "zed":
		return "exec " + command + " --wait " + shellQuote("ssh://"+settings.SSHHost+path)
	}

	return sshEditorShell(settings, "exec "+engineEditorCommand(command)+" "+at)
}

// remoteDirEditorShell is the shell line that opens a directory on the engine.
func remoteDirEditorShell(settings Settings, dir string) string {
	command := editorCommand(settings.Command)

	switch name := editorName(command); {
	case isVSCodeEditor(name):
		return "exec " + command + " --remote " + shellQuote("ssh-remote+"+settings.SSHHost) + " " + shellQuote(dir)
	case name == "zed":
		return "exec " + command + " " + shellQuote("ssh://"+settings.SSHHost+dir)
	}

	return sshEditorShell(settings, "exec "+engineEditorCommand(command)+" "+shellQuote(dir))
}

// sshEditorShell runs a terminal editor on the engine through its ssh host.
func sshEditorShell(settings Settings, remote string) string {
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

// IsRemoteEditRefused returns why the edit keys cannot open the engine's files, before the panel
// stops an agent for them.
func (s Settings) IsRemoteEditRefused() (string, bool) {
	if s.SSHHost == "" {
		return "", false
	}

	name := editorName(editorCommand(s.Command))
	if name == "subl" || name == "sublime_text" || isJetBrainsEditor(name) {
		return name + " cannot open files on another machine" + separated +
			"set EDITOR to one that can, such as nvim or code", true
	}

	if _, err := os.Stat(s.SSHConfig); err != nil {
		return "this machine has no ssh host for the engine" + separated + "run komrad login to write one", true
	}

	if (isVSCodeEditor(name) || name == "zed") && !s.IsSSHIncluded {
		return name + " cannot find the engine's ssh host" + separated +
			"add Include ~/.ssh/komrad/*.conf to the top of ~/.ssh/config, then reopen the panel", true
	}

	return "", false
}
