package ui

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"weave/config"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/x/ansi"
)

type stageTmux interface {
	IsPaneAlive(pane string) bool
	GetPaneWidth(pane string) int
	SplitRight(anchorPane, dir string, percent int) string
	SplitBelow(anchorPane, dir string, percent int) string
	ResizePane(pane string, percent int)
	SwapPanes(src, dst string)
	RespawnPane(pane, dir string, argv ...string)
	ZoomPane(pane string, zoom bool)
	GetSiblingPanes(pane string) []string
	BreakPane(pane string)
	KillPane(pane string)
	GetPaneOwner(pane string) string
}

const (
	shownBlank = "\x00blank"
	shownOther = "\x00other"
)

// Stage manages the panes beside and below the panel navigation.
type Stage struct {
	tmux           stageTmux
	split          float64
	secondarySplit float64
	navPane        string
	pane           string
	agentPane      string
	shown          string
	secondary      string
	shownBelow     string
	hidden         bool
}

// NewStage builds a stage with the configured layout.
func NewStage(tmux stageTmux, layout config.Layout) *Stage {
	return &Stage{tmux: tmux, split: layout.Split, secondarySplit: layout.SecondarySplit}
}

func (s *Stage) share() float64 {
	if s.split <= 0 || s.split >= 1 {
		return config.DefaultSplit
	}

	return s.split
}

func (s *Stage) percent() int {
	return int(math.Round((1 - s.share()) * 100))
}

func (s *Stage) secondaryPercent() int {
	share := s.secondarySplit
	if share <= 0 || share >= 1 {
		share = config.DefaultSecondarySplit
	}

	return int(math.Round(share * 100))
}

func (s *Stage) wrap(pane string) int {
	return s.tmux.GetPaneWidth(pane)
}

// Init anchors the stage to the panel pane and applies its layout.
func (s *Stage) Init(pane string) {
	s.navPane = pane
	s.reclaim()
	s.claim()

	if s.pane != "" {
		s.tmux.ResizePane(s.pane, s.percent())
	}
}

// NavPane returns the pane containing the panel.
func (s *Stage) NavPane() string { return s.navPane }

// SlotPane returns the pane currently displayed beside the panel.
func (s *Stage) SlotPane() string {
	if s.agentPane != "" {
		return s.agentPane
	}

	return s.pane
}

// Agent panes must be detached rather than killed when reclaiming a stale layout.
func (s *Stage) reclaim() {
	if s.navPane == "" {
		return
	}

	for _, pane := range s.tmux.GetSiblingPanes(s.navPane) {
		if s.tmux.GetPaneOwner(pane) != "" {
			s.tmux.BreakPane(pane)

			continue
		}

		s.tmux.KillPane(pane)
	}
}

func (s *Stage) claim() {
	if s.navPane == "" || s.hasSlot() {
		return
	}

	s.abandon()

	home, _ := os.UserHomeDir()
	s.pane = s.tmux.SplitRight(s.navPane, home, s.percent())
	s.shown = ""
}

func (s *Stage) hasSlot() bool {
	if s.pane == "" || !s.tmux.IsPaneAlive(s.pane) {
		return false
	}

	return s.agentPane == "" || s.tmux.IsPaneAlive(s.agentPane)
}

func (s *Stage) abandon() {
	if s.pane != "" && s.tmux.IsPaneAlive(s.pane) {
		s.tmux.KillPane(s.pane)
	}

	s.CloseSecondary()
	s.pane, s.agentPane, s.shown = "", "", ""
}

// OpenSecondary opens and returns the pane below the main stage.
func (s *Stage) OpenSecondary() string {
	s.claim()
	if s.pane == "" {
		return ""
	}
	if s.IsSecondaryAlive() {
		return s.secondary
	}

	home, _ := os.UserHomeDir()
	s.secondary = s.tmux.SplitBelow(s.SlotPane(), home, s.secondaryPercent())
	s.shownBelow = ""

	return s.secondary
}

// CloseSecondary closes the pane below the main stage.
func (s *Stage) CloseSecondary() {
	if s.secondary == "" {
		return
	}

	s.tmux.KillPane(s.secondary)
	s.secondary = ""
	s.shownBelow = ""
}

// IsSecondaryAlive reports whether the secondary pane is still running.
func (s *Stage) IsSecondaryAlive() bool {
	return s.secondary != "" && s.tmux.IsPaneAlive(s.secondary)
}

// SecondaryText renders markdown in the secondary pane.
func (s *Stage) SecondaryText(markdown string) {
	pane := s.OpenSecondary()
	if pane == "" || s.shownBelow == markdown {
		return
	}

	path := filepath.Join(os.TempDir(), "weave-secondary.ansi")
	if !writeStageFile(path, s.render(markdown, pane)) {
		return
	}

	s.displayCommand(pane, pagerArgv(path, 0)...)
	s.shownBelow = markdown
}

// SecondaryLog displays a log in the secondary pane.
func (s *Stage) SecondaryLog(path string, argv []string) {
	pane := s.OpenSecondary()
	if pane == "" || len(argv) == 0 || s.shownBelow == shownLog+path {
		return
	}

	s.displayCommand(pane, argv...)
	s.shownBelow = shownLog + path
}

// SecondaryClaimed records that another process controls the secondary pane.
func (s *Stage) SecondaryClaimed() {
	s.shownBelow = shownOther
}

// Hide toggles whether the navigation pane is zoomed over the stage.
func (s *Stage) Hide(hide bool) {
	if s.navPane == "" || s.hidden == hide {
		return
	}

	s.hidden = hide
	s.tmux.ZoomPane(s.navPane, hide)
}

func (s *Stage) restore() {
	if s.agentPane == "" {
		return
	}

	if s.tmux.IsPaneAlive(s.agentPane) {
		s.tmux.SwapPanes(s.pane, s.agentPane)
	}

	s.agentPane = ""
}

// Release removes an agent pane from the stage before it is killed.
func (s *Stage) Release(pane string) {
	if pane != "" && pane == s.agentPane {
		s.restore()
	}
}

// Agent displays a live agent pane in the stage.
func (s *Stage) Agent(pane string) {
	s.claim()
	if s.pane == "" || pane == "" || pane == s.agentPane {
		return
	}

	s.restore()
	s.tmux.SwapPanes(pane, s.pane)
	s.agentPane = pane
	s.shown = shownOther
}

// The pager must remain running or tmux will close its pane.
func pagerArgv(path string, line int) []string {
	argv := []string{"less", "-R", "-X", "-Ps"}
	if line > 1 {
		argv = append(argv, fmt.Sprintf("+%d", line))
	}

	return append(argv, path)
}

const shownLog = "\x00log:"

// Log displays a log in the main stage pane.
func (s *Stage) Log(path string, argv []string) {
	s.claim()
	if s.pane == "" || len(argv) == 0 || (s.agentPane == "" && s.shown == shownLog+path) {
		return
	}

	s.restore()

	s.displayCommand(s.pane, argv...)
	s.shown = shownLog + path
}

// Terminal style detection would compete with the panel event loop for input.
func (s *Stage) render(markdown, pane string) string {
	options := []glamour.TermRendererOption{glamour.WithStyles(stageStyle)}
	if wrap := s.wrap(pane); wrap > 0 {
		options = append(options, glamour.WithWordWrap(wrap))
	}

	renderer, err := glamour.NewTermRenderer(options...)
	if err != nil {
		return markdown
	}

	rendered, err := renderer.Render(markdown)
	if err != nil {
		return markdown
	}

	return rendered
}

// Text renders markdown in the main stage pane.
func (s *Stage) Text(markdown string) {
	s.claim()
	if s.pane == "" || (s.agentPane == "" && s.shown == markdown) {
		return
	}

	s.restore()

	path := filepath.Join(os.TempDir(), "weave-stage.ansi")
	if !writeStageFile(path, s.render(markdown, s.pane)) {
		return
	}

	s.displayCommand(s.pane, pagerArgv(path, 0)...)
	s.shown = markdown
}

const transcriptPreviewBefore = 12

// ANSI styling avoids terminal profile detection on the panel's input stream.
var highlightStyle = ansi.NewStyle().Reverse(true)

func highlighted(content string, line int) string {
	lines := strings.Split(content, "\n")
	if line < 1 || line > len(lines) {
		return content
	}

	lines[line-1] = highlightStyle.Styled(lines[line-1])

	return strings.Join(lines, "\n")
}

// Transcript displays a transcript with an optional highlighted line.
func (s *Stage) Transcript(content string, highlightLine int) {
	if strings.TrimSpace(content) == "" {
		s.Text("_no transcript yet_")
		return
	}

	s.claim()
	if s.pane == "" {
		return
	}

	s.restore()

	path := filepath.Join(os.TempDir(), "weave-stage.txt")
	if !writeStageFile(path, highlighted(content, highlightLine)) {
		return
	}

	open := 0
	if highlightLine > 0 {
		open = max(highlightLine-transcriptPreviewBefore, 1)
	}

	s.displayCommand(s.pane, pagerArgv(path, open)...)
	s.shown = shownOther
}

// Blank clears the main stage pane.
func (s *Stage) Blank() {
	s.claim()
	if s.pane == "" || (s.agentPane == "" && s.shown == shownBlank) {
		return
	}

	s.restore()
	s.displayCommand(s.pane)
	s.shown = shownBlank
}

func writeStageFile(path, content string) bool {
	return os.WriteFile(path, []byte(content), 0o644) == nil
}

func (s *Stage) displayCommand(pane string, argv ...string) {
	home, _ := os.UserHomeDir()
	s.tmux.RespawnPane(pane, home, argv...)
}
