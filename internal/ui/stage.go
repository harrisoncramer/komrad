package ui

import (
	"os"
	"path/filepath"
	"strings"

	"weave/config"
)

const (
	shownBlank = "\x00blank"
	shownOther = "\x00other"
	shownLog   = "\x00log:"
)

const transcriptPreviewBefore = 12

// Stage manages the panes beside and below the panel navigation, and what
// content is currently rendered into them.
type Stage struct {
	panes
	shown      string
	shownBelow string
}

// NewStage builds a stage with the configured layout.
func NewStage(tmux stageTmux, layout config.Layout) *Stage {
	return &Stage{panes: newPanes(tmux, layout)}
}

// claimMain ensures the main stage pane exists, forgetting what was last shown
// in it (and below it) whenever that meant rebuilding it from scratch.
func (s *Stage) claimMain() {
	if s.panes.claim() {
		s.shown, s.shownBelow = "", ""
	}
}

// OpenSecondary opens and returns the pane below the main stage.
func (s *Stage) OpenSecondary() string {
	s.claimMain()

	pane, rebuilt := s.panes.openSecondary()
	if rebuilt {
		s.shownBelow = ""
	}

	return pane
}

// CloseSecondary closes the pane below the main stage.
func (s *Stage) CloseSecondary() {
	s.panes.closeSecondary()
	s.shownBelow = ""
}

// SecondaryText renders markdown in the secondary pane.
func (s *Stage) SecondaryText(markdown string) {
	pane := s.OpenSecondary()
	if pane == "" || s.shownBelow == markdown {
		return
	}

	path := filepath.Join(os.TempDir(), "weave-secondary.ansi")
	if !writeStageFile(path, render(markdown, s.panes.wrap(pane))) {
		return
	}

	s.panes.run(pane, pagerArgv(path, 0)...)
	s.shownBelow = markdown
}

// SecondaryLog displays a log in the secondary pane.
func (s *Stage) SecondaryLog(path string, argv []string) {
	pane := s.OpenSecondary()
	if pane == "" || len(argv) == 0 || s.shownBelow == shownLog+path {
		return
	}

	s.panes.run(pane, argv...)
	s.shownBelow = shownLog + path
}

// SecondaryClaimed records that another process controls the secondary pane.
func (s *Stage) SecondaryClaimed() {
	s.shownBelow = shownOther
}

// Agent displays a live agent pane in the stage.
func (s *Stage) Agent(pane string) {
	if s.panes.agent(pane) {
		s.shown = shownOther
	}
}

// Log displays a log in the main stage pane.
func (s *Stage) Log(path string, argv []string) {
	s.claimMain()
	if s.panes.pane == "" || len(argv) == 0 || (s.panes.agentPane == "" && s.shown == shownLog+path) {
		return
	}

	s.panes.restore()

	s.panes.run(s.panes.pane, argv...)
	s.shown = shownLog + path
}

// Text renders markdown in the main stage pane.
func (s *Stage) Text(markdown string) {
	s.claimMain()
	if s.panes.pane == "" || (s.panes.agentPane == "" && s.shown == markdown) {
		return
	}

	s.panes.restore()

	path := filepath.Join(os.TempDir(), "weave-stage.ansi")
	if !writeStageFile(path, render(markdown, s.panes.wrap(s.panes.pane))) {
		return
	}

	s.panes.run(s.panes.pane, pagerArgv(path, 0)...)
	s.shown = markdown
}

// Transcript displays a transcript with an optional highlighted line.
func (s *Stage) Transcript(content string, highlightLine int) {
	if strings.TrimSpace(content) == "" {
		s.Text("_no transcript yet_")
		return
	}

	s.claimMain()
	if s.panes.pane == "" {
		return
	}

	s.panes.restore()

	path := filepath.Join(os.TempDir(), "weave-stage.txt")
	if !writeStageFile(path, highlighted(content, highlightLine)) {
		return
	}

	open := 0
	if highlightLine > 0 {
		open = max(highlightLine-transcriptPreviewBefore, 1)
	}

	s.panes.run(s.panes.pane, pagerArgv(path, open)...)
	s.shown = shownOther
}

// Blank clears the main stage pane.
func (s *Stage) Blank() {
	s.claimMain()
	if s.panes.pane == "" || (s.panes.agentPane == "" && s.shown == shownBlank) {
		return
	}

	s.panes.restore()
	s.panes.run(s.panes.pane)
	s.shown = shownBlank
}
