package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// stageTmux is the part of the tmux client the stage drives. The stage is
// presentation: it lays out the panel's own two panes and fires on every cursor
// move, so it talks to tmux directly rather than through the session API.
type stageTmux interface {
	CurrentPane() (string, error)
	PaneAlive(pane string) bool
	SplitRight(anchorPane, dir string) string
	SwapPanes(src, dst string)
	RespawnPane(pane, dir string, argv ...string)
}

// Stage is the pane beside the nav list that shows whatever the cursor is on.
// navPane anchors the split. pane is the one the stage drives, running either
// bat or a blank shell. agentPane is the agent pane currently swapped into the
// slot, empty when the stage is showing its own pane.
type Stage struct {
	tmux      stageTmux
	navPane   string
	pane      string
	agentPane string
}

// NewStage builds a stage over a tmux client. It claims no panes until Init.
func NewStage(tmux stageTmux) *Stage {
	return &Stage{tmux: tmux}
}

// Init records the panel's own pane as the anchor and splits a stage pane to its
// right, rooted at home. Called once on startup. It is a no-op split if a live
// stage already exists, so a panel restart does not stack splits.
func (s *Stage) Init() {
	if pane, err := s.tmux.CurrentPane(); err == nil && pane != "" {
		s.navPane = pane
	}
	s.ensure()
}

// ensure guarantees a live stage pane beside the panel, splitting a fresh one
// when the stage is missing or has died. A one-shot preview process like bat can
// exit and take its pane down with it. This self-heals without a panel restart. A
// stage pane swapped out to hold an agent is still alive, so it is left as is.
func (s *Stage) ensure() {
	if s.navPane == "" {
		return
	}
	if s.pane != "" && s.tmux.PaneAlive(s.pane) {
		return
	}

	home, _ := os.UserHomeDir()
	s.pane = s.tmux.SplitRight(s.navPane, home)
	s.agentPane = ""
}

// restore swaps any staged agent pane back to its own window, leaving the stage's
// own pane sitting in the slot again. A no-op when nothing is staged.
func (s *Stage) restore() {
	if s.agentPane == "" {
		return
	}

	s.tmux.SwapPanes(s.pane, s.agentPane)
	s.agentPane = ""
}

// Release swaps an agent pane back out if it is the one currently staged. Killing
// a pane while it sits in the stage slot would take the stage down with it, so
// anything about to end an agent calls this first.
func (s *Stage) Release(pane string) {
	if pane != "" && pane == s.agentPane {
		s.restore()
	}
}

// Agent swaps a live agent's pane into the stage slot, so its running claude shows
// beside the panel. Focus stays on the panel. Moving to the agent is a plain tmux
// pane move the user drives. A no-op when that agent is already staged.
func (s *Stage) Agent(pane string) {
	s.ensure()
	if s.pane == "" || pane == "" || pane == s.agentPane {
		return
	}

	s.restore()
	s.tmux.SwapPanes(pane, s.pane)
	s.agentPane = pane
}

// Text shows markdown in the stage by writing it to a temp file and running bat
// there. Used for a workflow's info and for a dormant agent's placeholder. bat is
// one-shot, so it runs inside a shell that holds the pane open after it prints.
// Otherwise the process would exit and tmux would close the stage pane.
func (s *Stage) Text(markdown string) {
	s.ensure()
	if s.pane == "" {
		return
	}

	s.restore()

	home, _ := os.UserHomeDir()
	path := filepath.Join(os.TempDir(), "claude-agents-stage.md")
	if err := os.WriteFile(path, []byte(markdown), 0o644); err != nil {
		return
	}

	cmd := fmt.Sprintf("bat --language=markdown --style=plain --paging=never '%s'; while :; do sleep 3600; done", path)
	s.tmux.RespawnPane(s.pane, home, "sh", "-c", cmd)
}

// transcriptPreviewBefore and transcriptPreviewAfter bound how many lines show
// around a matched line in a search preview. Wide enough to read the match in its
// surrounding conversation, but still a window into a large transcript. bat
// renders from the top, so showing the whole thing would push the match
// off-screen.
const (
	transcriptPreviewBefore = 12
	transcriptPreviewAfter  = 80
)

// Transcript shows a transcript in the stage centered on a matched line, with
// that line highlighted, so a search hit reads in the context around it. A
// highlightLine of zero shows the transcript from the top. An empty transcript
// shows a placeholder.
func (s *Stage) Transcript(content string, highlightLine int) {
	if strings.TrimSpace(content) == "" {
		s.Text("_no transcript yet_")
		return
	}

	s.ensure()
	if s.pane == "" {
		return
	}

	s.restore()

	home, _ := os.UserHomeDir()
	path := filepath.Join(os.TempDir(), "claude-agents-stage.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return
	}

	args := "--language=markdown --style=plain --paging=never"
	if highlightLine > 0 {
		start := highlightLine - transcriptPreviewBefore
		if start < 1 {
			start = 1
		}
		args += fmt.Sprintf(" --line-range %d:%d --highlight-line %d", start, highlightLine+transcriptPreviewAfter, highlightLine)
	}

	cmd := fmt.Sprintf("bat %s '%s'; while :; do sleep 3600; done", args, path)
	s.tmux.RespawnPane(s.pane, home, "sh", "-c", cmd)
}

// Blank clears the stage back to an empty shell.
func (s *Stage) Blank() {
	s.ensure()
	if s.pane == "" {
		return
	}

	s.restore()
	home, _ := os.UserHomeDir()
	s.tmux.RespawnPane(s.pane, home)
}
