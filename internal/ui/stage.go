package ui

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"claude-agents/internal/appconfig"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/x/ansi"
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
// a pager or a blank shell. agentPane is the agent pane currently swapped into the
// slot, empty when the stage is showing its own pane.
//
// shown is what the stage's own pane currently displays. The panel restages on
// every refresh tick, and redrawing means respawning the pane, so the stage
// compares against what is already there and does nothing when they match.
//
// split is the configured share of the width the nav list takes, which the stage
// holds because it is the one that lays the two panes out. listWidth is how wide
// that nav list is in columns, which is what the stage works its own width out
// from, since rendered markdown has to be wrapped to something.
type Stage struct {
	tmux      stageTmux
	split     float64
	listWidth int
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

// percent is the stage pane's share of the width as tmux takes it, a whole
// percentage. The configured split is the nav list's share, so the stage gets what
// is left of it.
func (s *Stage) percent() int {
	return int(math.Round((1 - s.share()) * 100))
}

// SetListWidth records how wide the panel's own pane is, which the stage measures
// itself against. The panel hands it over on every resize.
func (s *Stage) SetListWidth(cols int) {
	s.listWidth = cols
}

// wrap is how wide the stage pane is in columns, which is the width markdown is
// rendered to. The two panes divide one popup between them at the configured split,
// so the stage's width follows from the list's: the list holds its share of the
// whole and the stage the rest, less the column tmux draws the divider in. A stage
// nothing has measured yet has no width to report, which leaves the renderer on its
// own default.
func (s *Stage) wrap() int {
	if s.listWidth <= 0 {
		return 0
	}

	share := s.share()

	return int(math.Round(float64(s.listWidth)*(1-share)/share)) - 1
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

// pagerArgv is the command a stage render puts in the pane to show a file, opening
// on the given line or at the top for a line of zero. A pager is what holds the
// pane open, which a one-shot print does not: the process would exit and tmux would
// close the stage pane under it. It is also what lets content taller than the pane
// sit at its heading and scroll from there rather than dumping past the top and
// leaving whatever slice of the middle happened to land last. Of the flags, -R
// prints the escapes in the file as the colours they are rather than as their own
// text, and -X keeps less out of the alternate screen, where its first paint is lost
// when the popup holding the panel is detached. tmux runs this argv itself rather
// than through a shell, so nothing in it needs quoting.
func pagerArgv(path string, line int) []string {
	argv := []string{"less", "-R", "-X"}
	if line > 1 {
		argv = append(argv, fmt.Sprintf("+%d", line))
	}

	return append(argv, path)
}

// render turns markdown into the ANSI the pager prints. Glamour is the renderer
// glow itself is built on, so the panel carries markdown rendering in-process rather
// than shelling out to a highlighter the person running it would have to have
// installed. The style is named rather than detected: detection asks the terminal
// for its background colour and reads the answer back off the same input the panel's
// own event loop is reading. A renderer that cannot be built, or a document it
// cannot parse, falls back to the source text, which is plain enough to read as it
// is.
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

// Text shows markdown in the stage by rendering it, writing that to a temp file and
// paging the file there, which is how a workflow's info and a dormant agent's
// placeholder render.
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
	path := filepath.Join(os.TempDir(), "claude-agents-stage.ansi")
	if err := os.WriteFile(path, []byte(s.render(markdown)), 0o644); err != nil {
		return
	}

	s.tmux.RespawnPane(s.pane, home, pagerArgv(path, 0)...)
	s.shown = markdown
}

// transcriptPreviewBefore is how many lines of a transcript show above a matched
// line in a search preview. The pager opens on the line it is given, so the lines
// before the match are what put it a short way down the pane rather than at the
// very top, with enough of the conversation above it to read the match in.
const transcriptPreviewBefore = 12

// highlightStyle marks the matched line of a transcript preview. The pager prints
// the escapes in the file as the colours they are, so the mark is written into the
// copy it reads rather than asked of the pager itself. The escape is written
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
	path := filepath.Join(os.TempDir(), "claude-agents-stage.txt")
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
