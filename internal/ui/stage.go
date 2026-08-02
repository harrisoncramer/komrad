package ui

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"weave/internal/appconfig"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/x/ansi"
)

// stageTmux is the part of the tmux client the stage drives. The stage lays out the
// panel's own two panes on every cursor move, so it talks to tmux directly rather than
// through the agent API.
type stageTmux interface {
	PaneAlive(pane string) bool
	PaneWidth(pane string) int
	SplitRight(anchorPane, dir string, percent int) string
	ResizePane(pane string, percent int)
	SwapPanes(src, dst string)
	RespawnPane(pane, dir string, argv ...string)
	ZoomPane(pane string, zoom bool)
	GetSiblingPanes(pane string) []string
	BreakPane(pane string)
	KillPane(pane string)
	PaneOwner(pane string) string
}

// shownBlank and shownOther are what the stage records for content it cannot compare
// against markdown: an empty shell, and a transcript or staged agent. Neither can collide
// with rendered markdown, so a redraw of either always goes through.
const (
	shownBlank = "\x00blank"
	shownOther = "\x00other"
)

// Stage is the pane beside the nav list that shows whatever the cursor is on. navPane
// anchors the split, pane is the one the stage drives, and agentPane is the agent pane
// swapped into the slot, empty when the stage shows its own.
//
// shown is what the stage's own pane displays. The panel restages on every refresh tick
// and redrawing means respawning the pane, so it does nothing when the two match.
type Stage struct {
	tmux      stageTmux
	split     float64
	navPane   string
	pane      string
	agentPane string
	shown     string
	// hidden is whether the stage is zoomed away. The panel reconciles this on every
	// message, so what was last asked for is remembered rather than paid for in a tmux
	// round trip.
	hidden bool
}

// NewStage builds a stage over a tmux client, dividing the width by split. It claims no
// panes until Init.
func NewStage(tmux stageTmux, split float64) *Stage {
	return &Stage{tmux: tmux, split: split}
}

// share is the fraction of the width the nav list takes. A split outside the range the
// loader allows falls back to the default, since a stage built by hand has none.
func (s *Stage) share() float64 {
	if s.split <= 0 || s.split >= 1 {
		return appconfig.DefaultSplit
	}

	return s.split
}

// percent is the stage pane's share of the width as tmux takes it, a whole percentage.
// The configured split is the nav list's share, so the stage gets what is left of it.
func (s *Stage) percent() int {
	return int(math.Round((1 - s.share()) * 100))
}

// wrap is how wide the stage pane is in columns, which is the width markdown renders to.
// It comes from tmux rather than the configured split, since a pane the user dragged is no
// longer the share the panel asked for and rendering wider leaves the pager folding every
// line into three rows. A pane tmux cannot measure leaves the renderer on its default.
func (s *Stage) wrap() int {
	return s.tmux.PaneWidth(s.pane)
}

// Init records the panel's own pane as the anchor and splits a stage pane to its right,
// rooted at home. A live stage is left alone so a panel restart does not stack splits, but
// resized either way, so an edited split takes effect on the next start.
//
// The pane is passed in rather than asked of tmux. tmux resolves a pane query against the
// attached client, which answers for whatever pane that client is looking at and fails
// outright when the session is running detached, and everything below rearranges the
// window around this pane.
func (s *Stage) Init(pane string) {
	s.navPane = pane
	s.reclaim()
	s.ensure()

	if s.pane != "" {
		s.tmux.ResizePane(s.pane, s.percent())
	}
}

// NavPane is the pane the panel itself runs in, empty until Init has found it.
func (s *Stage) NavPane() string { return s.navPane }

// reclaim clears the panel's window of whatever the last panel left in it. Which panes
// the stage owns is remembered in this process and nowhere else, so a panel that was
// replaced or that died leaves panes its successor cannot recognise, and ensure would
// split a second stage beside them.
//
// An agent's pane is staged by swapping the pane itself into the slot, so one found here
// is returned to a window of its own rather than killed. The stamp says which, and is the
// same one the reconciler reads. Anything else is the last stage's own pane, which the
// panel is about to replace.
func (s *Stage) reclaim() {
	if s.navPane == "" {
		return
	}

	for _, pane := range s.tmux.GetSiblingPanes(s.navPane) {
		if s.tmux.PaneOwner(pane) != "" {
			s.tmux.BreakPane(pane)

			continue
		}

		s.tmux.KillPane(pane)
	}
}

// ensure guarantees a live stage pane beside the panel, splitting a fresh one when the
// stage is missing or has died, which is what a quit pager leaves behind. A stage pane
// swapped out to hold an agent is still alive and is left as is.
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

// Hide zooms the nav pane over the stage, and Hide(false) gives the stage its share of the
// width back. A form has nothing to preview, so leaving the stage up beside one spends
// half the width on whatever the cursor happened to be on.
//
// It zooms rather than killing the pane, since the pane may hold a swapped-in agent and a
// torn-down stage would have to be split and repainted on the way back.
func (s *Stage) Hide(hide bool) {
	if s.navPane == "" || s.hidden == hide {
		return
	}

	s.hidden = hide
	s.tmux.ZoomPane(s.navPane, hide)
}

// restore swaps any staged agent pane back to its own window, leaving the stage's own pane
// in the slot again.
func (s *Stage) restore() {
	if s.agentPane == "" {
		return
	}

	s.tmux.SwapPanes(s.pane, s.agentPane)
	s.agentPane = ""
}

// Release swaps an agent pane back out if it is the one staged. Killing a pane while it
// sits in the stage slot would take the stage down with it, so anything about to end an
// agent calls this first.
func (s *Stage) Release(pane string) {
	if pane != "" && pane == s.agentPane {
		s.restore()
	}
}

// Agent swaps a live agent's pane into the stage slot, so its running CLI shows beside the
// panel. Focus stays on the panel, and moving to the agent is a plain tmux pane move the
// user drives.
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

// pagerArgv is the command a stage render puts in the pane to show a file, opening on the
// given line or at the top for a line of zero. A pager is what holds the pane open, which
// a one-shot print does not: it would exit and tmux would close the pane under it.
//
// Of the flags, -R prints the escapes in the file as the colours they are, -X keeps less
// out of the alternate screen, where its first paint is lost when the popup is detached,
// and -Ps with no prompt after it empties the status line less otherwise fills with the
// temp file's path in reverse video. tmux runs this argv itself rather than through a
// shell, so nothing in it needs quoting.
func pagerArgv(path string, line int) []string {
	argv := []string{"less", "-R", "-X", "-Ps"}
	if line > 1 {
		argv = append(argv, fmt.Sprintf("+%d", line))
	}

	return append(argv, path)
}

// shownLog prefixes what the stage records for a file it is paging, so a restage of the
// same file is the no-op every other kind of content gets.
const shownLog = "\x00log:"

// Log runs argv in the stage to read the file at path, in place of whatever the cursor was
// showing. It uses the same pane a preview does, so reading the log is scrolling the stage
// rather than something that has to be dismissed.
//
// argv belongs to the caller, since the log is the one thing here whose reader the user
// chooses. The path comes alongside only to tell one file from another, so a restage of
// the log already up does not respawn the pane under somebody reading it.
func (s *Stage) Log(path string, argv []string) {
	s.ensure()
	if s.pane == "" || len(argv) == 0 {
		return
	}
	if s.agentPane == "" && s.shown == shownLog+path {
		return
	}

	s.restore()

	home, _ := os.UserHomeDir()
	s.tmux.RespawnPane(s.pane, home, argv...)
	s.shown = shownLog + path
}

// render turns markdown into the ANSI the pager prints. The style is given rather than
// detected, since detection asks the terminal for its background colour and reads the
// answer off the same input the panel's event loop is reading. Nothing is lost by not
// asking, because the palette draws from the sixteen the terminal maps and so suits a
// light background and a dark one alike.
func (s *Stage) render(markdown string) string {
	options := []glamour.TermRendererOption{glamour.WithStyles(stageStyle)}
	if wrap := s.wrap(); wrap > 0 {
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

// Text shows markdown in the stage by rendering it to a temp file and paging that
// file, which is how a workflow's info and a dormant agent's placeholder render.
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
	path := filepath.Join(os.TempDir(), "weave-stage.ansi")
	if err := os.WriteFile(path, []byte(s.render(markdown)), 0o644); err != nil {
		return
	}

	s.tmux.RespawnPane(s.pane, home, pagerArgv(path, 0)...)
	s.shown = markdown
}

// transcriptPreviewBefore is how many lines of a transcript show above a matched line in a
// search preview, so the match sits a short way down the pane with enough above it to read
// in.
const transcriptPreviewBefore = 12

// highlightStyle marks the matched line of a transcript preview. The escape is written
// directly rather than through lipgloss, which styles against the colour profile it
// detects on the panel's own output and would drop the mark on the way to a file.
var highlightStyle = ansi.NewStyle().Reverse(true)

// highlighted returns the content with its given 1-based line marked. A line of zero, or
// one past the end of a transcript the search offsets have drifted from, changes nothing.
func highlighted(content string, line int) string {
	lines := strings.Split(content, "\n")
	if line < 1 || line > len(lines) {
		return content
	}

	lines[line-1] = highlightStyle.Styled(lines[line-1])

	return strings.Join(lines, "\n")
}

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
	path := filepath.Join(os.TempDir(), "weave-stage.txt")
	if err := os.WriteFile(path, []byte(highlighted(content, highlightLine)), 0o644); err != nil {
		return
	}

	open := 0
	if highlightLine > 0 {
		open = max(highlightLine-transcriptPreviewBefore, 1)
	}

	s.tmux.RespawnPane(s.pane, home, pagerArgv(path, open)...)
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
