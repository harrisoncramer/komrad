package editor

import (
	"fmt"
	"path/filepath"
	"strings"

	"komrad/internal/ui/layout"
)

// defaultEditor is what the file is opened in when nothing said otherwise.
const defaultEditor = "vi"

// editorShell is the shell line that runs the user's editor on a file, on one line of it.
func editorShell(editor, path string, line int) string {
	command := editorCommand(editor)

	// The editor is left unquoted so an EDITOR of several words stays several words.
	return "exec " + command + " " + strings.Join(editorLineArgs(command, path, line), " ")
}

func editorCommand(editor string) string {
	command := strings.TrimSpace(editor)
	if command == "" {
		return defaultEditor
	}

	return command
}

// editorLineArgs is how one editor is told which line to open a file on, defaulting to the +line
// argument every terminal editor in the vi and emacs line takes.
func editorLineArgs(command, path string, line int) []string {
	at := shellQuote(fmt.Sprintf("%s:%d", path, line))

	switch name := editorName(command); {
	case isVSCodeEditor(name):
		return []string{"--wait", "--goto", at}
	case name == "subl", name == "sublime_text", name == "zed":
		return []string{"--wait", at}
	case name == "hx", name == "helix":
		return []string{at}
	case isJetBrainsEditor(name):
		return []string{"--wait", "--line", fmt.Sprint(line), shellQuote(path)}
	}

	return []string{fmt.Sprintf("+%d", line), shellQuote(path)}
}

// editorName is the program an EDITOR of several words runs.
func editorName(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return ""
	}

	return filepath.Base(fields[0])
}

// isVSCodeEditor reports whether the editor is VS Code or one built from it.
func isVSCodeEditor(name string) bool {
	switch name {
	case "code", "code-insiders", "codium", "vscodium", "cursor", "windsurf":
		return true
	}

	return false
}

// isJetBrainsEditor reports whether the editor is one of the JetBrains IDEs.
func isJetBrainsEditor(name string) bool {
	switch name {
	case "goland", "idea", "pycharm", "webstorm", "phpstorm", "rubymine", "clion", "rider", "rustrover",
		"datagrip":
		return true
	}

	return false
}

// Content opens a file in the user's editor, on one line of it.
func Content(editor, dir, path string, line int) layout.Content {
	open := editorShell(editor, path, line)

	return layout.Content{
		Key:    fmt.Sprintf("edit:%s:%d", path, line),
		Dir:    dir,
		Render: func(layout.Target) ([]string, bool) { return []string{"sh", "-c", open}, true },
	}
}

func dirEditorContent(editor, dir string) layout.Content {
	open := "exec " + editorCommand(editor) + " " + shellQuote(dir)

	return layout.Content{
		Key:    "edit:" + dir,
		Dir:    dir,
		Render: func(layout.Target) ([]string, bool) { return []string{"sh", "-c", open}, true },
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
