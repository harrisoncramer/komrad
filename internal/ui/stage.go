package ui

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"weave/internal/appconfig"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/x/ansi"
)

// stageTmux is the part of the tmux client the stage drives. The stage lays out the
// panel's own two panes and fires on every cursor move, so it talks to tmux directly
// rather than through the agent API.
type stageTmux interface {
	CurrentPane() (string, error)
	PaneAlive(pane string) bool
	PaneWidth(pane string) int
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
// navPane anchors the split, pane is the one the stage drives, and agentPane is the
// agent pane currently swapped into the slot, empty when the stage shows its own pane.
//
// shown is what the stage's own pane currently displays. The panel restages on every
// refresh tick, and redrawing means respawning the pane, so the stage compares against
// what is already there and does nothing when they match.
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

// share is the fraction of the width the nav list takes. A split outside the range
// the loader allows falls back to the default, since a stage built by hand rather
// than from a config file has none.
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

// wrap is how wide the stage pane is in columns, which is the width markdown renders
// to. It comes from tmux rather than from the configured split, because a pane the
// user has dragged is no longer the share the panel asked for, and rendering wider
// than the pane leaves the pager folding every line into three rows, most of them the
// padding the renderer fills a line out to. A pane tmux cannot measure leaves the
// renderer on its own default.
func (s *Stage) wrap() int {
	return s.tmux.PaneWidth(s.pane)
}

// Init records the panel's own pane as the anchor and splits a stage pane to its
// right, rooted at home. It is a no-op split if a live stage already exists, so a
// panel restart does not stack splits. The pane is resized either way, which is what
// makes an edited split take effect on the next start rather than only once the pane
// has died.
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
// the stage is missing or has died. A pager quit from the pane exits and takes the
// pane down with it, so this self-heals without a panel restart. A stage pane
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

// Agent swaps a live agent's pane into the stage slot, so its running CLI shows
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

// pagerArgv is the command a stage render puts in the pane to show a file, opening on
// the given line or at the top for a line of zero. A pager is what holds the pane open,
// which a one-shot print does not: the process would exit and tmux would close the
// stage pane under it. Of the flags, -R prints the escapes in the file as the colours
// they are rather than as their own text, and -X keeps less out of the alternate
// screen, where its first paint is lost when the popup holding the panel is detached.
// tmux runs this argv itself rather than through a shell, so nothing in it needs
// quoting.
func pagerArgv(path string, line int) []string {
	argv := []string{"less", "-R", "-X"}
	if line > 1 {
		argv = append(argv, fmt.Sprintf("+%d", line))
	}

	return append(argv, path)
}

// render turns markdown into the ANSI the pager prints. The style is named rather than
// detected: detection asks the terminal for its background colour and reads the answer
// back off the same input the panel's own event loop is reading. A renderer that cannot
// be built, or a document it cannot parse, falls back to the source text.
func (s *Stage) render(markdown string) string {
	options := []glamour.TermRendererOption{glamour.WithStandardStyle(styles.DarkStyle)}
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

// transcriptPreviewBefore is how many lines of a transcript show above a matched line
// in a search preview, so the match sits a short way down the pane with enough of the
// conversation above it to read in.
const transcriptPreviewBefore = 12

// highlightStyle marks the matched line of a transcript preview. The escape is written
// directly rather than through lipgloss, which styles against the colour profile it
// detects on the panel's own output and would drop the mark on the way to a file.
var highlightStyle = ansi.NewStyle().Reverse(true)

// highlighted returns the content with its given 1-based line marked. A line of
// zero, or one past the end of a transcript the search offsets have drifted from,
// leaves the content as it is.
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
