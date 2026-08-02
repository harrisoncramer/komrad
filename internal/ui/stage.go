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

// shownBlank and shownOther are what the stage records for content it cannot compare
// against markdown: an empty shell, and a transcript or staged agent. Neither can collide
// with rendered markdown, so a redraw of either always goes through.
const (
	shownBlank = "\x00blank"
	shownOther = "\x00other"
)

// Stage is the right-hand column of the panel's window, in two slots. The main slot shows
// whatever the cursor is on, and the secondary slot under it holds what the user asked
// for: a shell, an editor, the server log, or a record read beside the agent it is about.
//
// navPane anchors the split, pane is the main slot's own pane, and agentPane is the agent
// pane swapped into that slot, empty while the stage shows its own. secondary is the pane
// under it, empty while that slot is closed. It is always the panel's own pane, never an
// agent's, so respawning it can never take a running CLI down.
//
// shown and shownBelow are what each pane displays. The panel restages on every refresh
// tick and redrawing means respawning a pane, so it does nothing when the two match.
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
	// hidden is whether the stage is zoomed away. The panel reconciles this on every
	// message, so what was last asked for is remembered rather than paid for in a tmux
	// round trip.
	hidden bool
}

// NewStage builds a stage over a tmux client, divided as the layout asks. It claims no
// panes until Init.
func NewStage(tmux stageTmux, layout appconfig.Layout) *Stage {
	return &Stage{tmux: tmux, split: layout.Split, secondarySplit: layout.SecondarySplit}
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

// secondaryPercent is the share of the column's height the secondary slot opens at, as a
// whole percentage. A configured share outside the range the loader allows falls back to
// the default, since a stage built by hand has none.
func (s *Stage) secondaryPercent() int {
	share := s.secondarySplit
	if share <= 0 || share >= 1 {
		share = appconfig.DefaultSecondarySplit
	}

	return int(math.Round(share * 100))
}

// wrap is how wide a pane is in columns, which is the width markdown renders to. It comes
// from tmux rather than the configured split, since a pane the user dragged is no longer
// the share the panel asked for and rendering wider leaves the pager folding every line
// into three rows. A pane tmux cannot measure leaves the renderer on its default.
func (s *Stage) wrap(pane string) int {
	return s.tmux.GetPaneWidth(pane)
}

// Init records the panel's own pane as the anchor and splits a stage pane to its right,
// rooted at home. A live stage is left alone so a panel restart does not stack splits, but
// resized either way, so an edited split takes effect on the next start.
//
// The pane is passed in rather than asked of tmux. tmux reads a pane query against the
// attached client, which answers for whatever pane that client is looking at and fails
// outright when the session is running detached, and everything below rearranges the
// window around this pane.
func (s *Stage) Init(pane string) {
	s.navPane = pane
	s.reclaim()
	s.claim()

	if s.pane != "" {
		s.tmux.ResizePane(s.pane, s.percent())
	}
}

// NavPane is the pane the panel itself runs in, empty until Init has found it.
func (s *Stage) NavPane() string { return s.navPane }

// SlotPane is whichever pane fills the stage's slot beside the panel, the staged agent's
// own while one is swapped in and the stage's otherwise. Anything laying itself out against
// what is on screen anchors here rather than on the stage's own pane, which is away in the
// agent's window for as long as a swap is up.
func (s *Stage) SlotPane() string {
	if s.agentPane != "" {
		return s.agentPane
	}

	return s.pane
}

// reclaim clears the panel's window of whatever the last panel left in it. Which panes
// the stage owns is remembered in this process and nowhere else, so a panel that was
// replaced or that died leaves panes its successor cannot recognise, and claim would
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
		if s.tmux.GetPaneOwner(pane) != "" {
			s.tmux.BreakPane(pane)

			continue
		}

		s.tmux.KillPane(pane)
	}
}

// claim gives the stage the slot beside the panel, splitting a fresh pane whenever it no
// longer holds one. Everything the stage draws calls this first, so a slot lost to something
// outside the panel is back on the next frame.
func (s *Stage) claim() {
	if s.navPane == "" || s.hasSlot() {
		return
	}

	s.abandon()

	home, _ := os.UserHomeDir()
	s.pane = s.tmux.SplitRight(s.navPane, home, s.percent())
	s.shown = ""
}

// hasSlot reports whether the stage still holds the slot beside the panel, which comes down
// to whether the pane filling that slot is running.
//
// A staged agent's pane is asked after as well as the stage's own. Staging swaps the agent's
// pane into the panel's window and sends the stage's own out to the agent's, so whoever ends
// that agent ends its pane in the panel's window. The slot goes with it, and the stage is
// left running in a window nobody can see.
func (s *Stage) hasSlot() bool {
	if s.pane == "" || !s.tmux.IsPaneAlive(s.pane) {
		return false
	}

	return s.agentPane == "" || s.tmux.IsPaneAlive(s.agentPane)
}

// abandon drops a stage that has lost its slot. Its own pane is killed while it is still
// running, since by then it is away in another window and nothing will find it again. The
// secondary slot hangs off the main one, so it goes at the same time.
func (s *Stage) abandon() {
	if s.pane != "" && s.tmux.IsPaneAlive(s.pane) {
		s.tmux.KillPane(s.pane)
	}

	s.CloseSecondary()
	s.pane, s.agentPane, s.shown = "", "", ""
}

// OpenSecondary opens the slot under the main one and returns its pane, splitting one when
// it is not there and answering with the pane already open otherwise. Everything that fills
// that slot is respawned into this pane, whether the panel does it or an action does.
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

// CloseSecondary takes the slot away and gives the main one its height back.
func (s *Stage) CloseSecondary() {
	if s.secondary == "" {
		return
	}

	s.tmux.KillPane(s.secondary)
	s.secondary = ""
	s.shownBelow = ""
}

// IsSecondaryAlive reports whether the slot is open and still holds a pane. It is false once
// whatever was running down there has exited, since tmux takes the pane with it.
func (s *Stage) IsSecondaryAlive() bool {
	return s.secondary != "" && s.tmux.IsPaneAlive(s.secondary)
}

// SecondaryText shows markdown in the slot under the main one, the way Text does above it,
// so a record reads beside the agent it is about rather than in place of it.
func (s *Stage) SecondaryText(markdown string) {
	pane := s.OpenSecondary()
	if pane == "" || s.shownBelow == markdown {
		return
	}

	home, _ := os.UserHomeDir()
	path := filepath.Join(os.TempDir(), "weave-secondary.ansi")
	if err := os.WriteFile(path, []byte(s.render(markdown, pane)), 0o644); err != nil {
		return
	}

	s.tmux.RespawnPane(pane, home, pagerArgv(path, 0)...)
	s.shownBelow = markdown
}

// SecondaryLog runs argv in the slot under the main one to read the file at path. argv
// belongs to the caller, since the log is the one thing here whose reader the user chooses.
// The path comes alongside only to tell one file from another, so a restage of the log
// already up does not respawn the pane under somebody reading it.
func (s *Stage) SecondaryLog(path string, argv []string) {
	pane := s.OpenSecondary()
	if pane == "" || len(argv) == 0 || s.shownBelow == shownLog+path {
		return
	}

	home, _ := os.UserHomeDir()
	s.tmux.RespawnPane(pane, home, argv...)
	s.shownBelow = shownLog + path
}

// SecondaryClaimed records that something outside the panel has filled the slot, which is
// what an action does. What it put there cannot be compared against anything, so the next
// render of the panel's own content into that pane always goes through.
func (s *Stage) SecondaryClaimed() {
	s.shownBelow = shownOther
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
// in the slot again. A pane tmux no longer has is only forgotten, since the swap would be
// refused and claim has already rebuilt the slot around it.
func (s *Stage) restore() {
	if s.agentPane == "" {
		return
	}

	if s.tmux.IsPaneAlive(s.agentPane) {
		s.tmux.SwapPanes(s.pane, s.agentPane)
	}

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
	s.claim()
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
	s.claim()
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

// Text shows markdown in the stage by rendering it to a temp file and paging that
// file, which is how a workflow's info and a dormant agent's placeholder render.
func (s *Stage) Text(markdown string) {
	s.claim()
	if s.pane == "" {
		return
	}
	if s.agentPane == "" && s.shown == markdown {
		return
	}

	s.restore()

	home, _ := os.UserHomeDir()
	path := filepath.Join(os.TempDir(), "weave-stage.ansi")
	if err := os.WriteFile(path, []byte(s.render(markdown, s.pane)), 0o644); err != nil {
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

	s.claim()
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
	s.claim()
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
