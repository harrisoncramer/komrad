package ui

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"claude-agents/internal/appconfig"
)

// stageTmux is the part of the tmux client the stage drives. The stage is
// presentation. It lays out the panel's own two panes and fires on every cursor
// move, so it talks to tmux directly rather than through the agent API.
type stageTmux interface {
	CurrentPane() (string, error)
	PaneAlive(pane string) bool
	SplitRight(anchorPane, dir string, percent int) string
	ResizePane(pane string, percent int)
	SwapPanes(src, dst string)
	RespawnPane(pane, dir string, argv ...string)
}

// shownBlank and shownOther are what the stage records for content that is not
// markdown it could compare against: an empty shell and a transcript or a staged
// agent. Neither can collide with rendered markdown, so a redraw of either always
// goes through.
const (
	shownBlank = "\x00blank"
	shownOther = "\x00other"
)

// Stage is the pane beside the nav list that shows whatever the cursor is on.
// navPane anchors the split. pane is the one the stage drives, running either
// bat or a blank shell. agentPane is the agent pane currently swapped into the
// slot, empty when the stage is showing its own pane.
//
// shown is what the stage's own pane currently displays. The panel restages on
// every refresh tick, and redrawing means respawning the pane, so the stage
// compares against what is already there and does nothing when they match.
//
// split is the configured share of the width the nav list takes, which the stage
// holds because it is the one that lays the two panes out.
type Stage struct {
	tmux      stageTmux
	split     float64
	navPane   string
	pane      string
	agentPane string
	shown     string
}

// NewStage builds a stage over a tmux client, dividing the width by split. It
// claims no panes until Init.
func NewStage(tmux stageTmux, split float64) *Stage {
	return &Stage{tmux: tmux, split: split}
}

// percent is the stage pane's share of the width as tmux takes it, a whole
// percentage. The configured split is the nav list's share, so the stage gets what
// is left of it. A split outside the range the loader allows falls back to the
// default, since a stage built by hand rather than from a config file has none.
func (s *Stage) percent() int {
	split := s.split
	if split <= 0 || split >= 1 {
		split = appconfig.DefaultSplit
	}

	return int(math.Round((1 - split) * 100))
}

// Init records the panel's own pane as the anchor and splits a stage pane to its
// right, rooted at home. Called once on startup. It is a no-op split if a live
// stage already exists, so a panel restart does not stack splits.
//
// The stage pane is sized either way. A live one was split at whatever the config
// said when it was created, so resizing here is what makes an edited split take
// effect on the next start rather than only once the pane has died.
func (s *Stage) Init() {
	if pane, err := s.tmux.CurrentPane(); err == nil && pane != "" {
		s.navPane = pane
	}
	s.ensure()

	if s.pane != "" {
		s.tmux.ResizePane(s.pane, s.percent())
	}
}

// ensure guarantees a live stage pane beside the panel, splitting a fresh one when
// the stage is missing or has died. A one-shot preview process like bat can exit
// and take its pane down with it, so this self-heals without a panel restart. A
// stage pane swapped out to hold an agent is still alive and is left as is.
func (s *Stage) ensure() {
	if s.navPane == "" {
		return
	}
	if s.pane != "" && s.tmux.PaneAlive(s.pane) {
		return
	}

	home, _ := os.UserHomeDir()
	s.pane = s.tmux.SplitRight(s.navPane, home, s.percent())
	s.agentPane = ""
	s.shown = ""
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
// beside the panel. Focus stays on the panel, and moving to the agent is a plain
// tmux pane move the user drives. A no-op when that agent is already staged.
func (s *Stage) Agent(pane string) {
	s.ensure()
	if s.pane == "" || pane == "" || pane == s.agentPane {
		return
	}

	s.restore()
	s.tmux.SwapPanes(pane, s.pane)
	s.agentPane = pane
	s.shown = shownOther
}

// Text shows markdown in the stage by writing it to a temp file and running bat
// there, which is how a workflow's info and a dormant agent's placeholder render.
// bat pages through less, so a record taller than the pane sits at its heading
// and scrolls from there rather than dumping past the top of the pane and leaving
// whatever slice of the middle happened to land last. less also holds the pane
// open, which a one-shot print does not: the process would exit and tmux would
// close the stage pane under it. Its flags are only -R and -X: bat's own pager
// arguments quit on anything that fits a single screen and every short record
// would take the pane with it, and -X keeps less out of the alternate screen,
// where its first paint is lost when the popup holding the panel is detached.
func (s *Stage) Text(markdown string) {
	s.ensure()
	if s.pane == "" {
		return
	}
	if s.agentPane == "" && s.shown == markdown {
		return
	}

	s.restore()

	home, _ := os.UserHomeDir()
	path := filepath.Join(os.TempDir(), "claude-agents-stage.md")
	if err := os.WriteFile(path, []byte(markdown), 0o644); err != nil {
		return
	}

	cmd := fmt.Sprintf("bat --language=markdown --style=plain --paging=always --pager='less -RX' '%s'", path)
	s.tmux.RespawnPane(s.pane, home, "sh", "-c", cmd)
	s.shown = markdown
}

// transcriptPreviewBefore and transcriptPreviewAfter bound how many lines show
// around a matched line in a search preview. Wide enough to read the match in its
// surrounding conversation, but still a window into a large transcript. The pager
// opens on the first line bat prints, so the lines before the match are what put
// it a short way down the pane rather than at the very top.
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

	args := "--language=markdown --style=plain --paging=always --pager='less -RX'"
	if highlightLine > 0 {
		start := max(highlightLine-transcriptPreviewBefore, 1)
		args += fmt.Sprintf(" --line-range %d:%d --highlight-line %d", start, highlightLine+transcriptPreviewAfter, highlightLine)
	}

	cmd := fmt.Sprintf("bat %s '%s'", args, path)
	s.tmux.RespawnPane(s.pane, home, "sh", "-c", cmd)
	s.shown = shownOther
}

// Blank clears the stage back to an empty shell.
func (s *Stage) Blank() {
	s.ensure()
	if s.pane == "" {
		return
	}
	if s.agentPane == "" && s.shown == shownBlank {
		return
	}

	s.restore()
	home, _ := os.UserHomeDir()
	s.tmux.RespawnPane(s.pane, home)
	s.shown = shownBlank
}
