package review

import (
	"fmt"
	"strings"

	"weave/internal/diff"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// gutterWidth is the two line numbers, the marker, the thread mark, and the spaces between.
const gutterWidth = 13

const tabWidth = 4

const horizontalStep = 8

const (
	threadMark = "●"
	unreadMark = "◆"
)

var (
	addedBackground = lipgloss.AdaptiveColor{Light: "#dafbe1", Dark: "#1b3823"}

	deletedBackground = lipgloss.AdaptiveColor{Light: "#ffebe9", Dark: "#402222"}

	cursorBackground = lipgloss.AdaptiveColor{Light: "#dbeafe", Dark: "#1e3a5f"}

	plainBackground = lipgloss.NoColor{}

	gutterStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	fileStyle = lipgloss.NewStyle().Bold(true)

	metadataStyle = lipgloss.NewStyle().Faint(true)

	hunkStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))

	headerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Bold(true)

	footerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	noticeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))

	errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))

	threadStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))

	threadMarkStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))

	humanStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)

	agentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)

	resolvedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Strikethrough(true)

	selectedStyle = lipgloss.NewStyle().Background(cursorBackground)

	composerStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("11")).
			Padding(0, 1)

	quotedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true)
)

// View draws whichever screen is up. The diff is the one with the cursor in it; the others
// are the threads pinned to it, read on their own rather than folded into the code.
func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	switch m.mode {
	case modeThreads:
		return m.renderThreads()
	case modeThread:
		return m.renderThread()
	case modeNotify:
		return m.renderNotify()
	case modeDiff, modeCompose:
	}

	body := m.getBodyHeight()

	var out strings.Builder
	out.WriteString(m.renderHeader() + "\n")
	for y := m.top; y < m.top+body && y < len(m.view.rows); y++ {
		out.WriteString(m.renderRow(y) + "\n")
	}
	for range body - min(body, len(m.view.rows)-m.top) {
		out.WriteString("\n")
	}

	if m.mode == modeCompose {
		out.WriteString(m.renderComposer() + "\n")
	}

	out.WriteString(m.renderFooter())

	return out.String()
}

func (m model) renderHeader() string {
	path := "no changes"
	if file, ok := m.view.getFile(m.cursor); ok {
		path = file.Path()
	}

	right := fmt.Sprintf("  %s (%s)", m.patch.Base, shortCommit(m.patch.Commit))
	if len(m.loaded) > 0 {
		right += fmt.Sprintf("  %d thread(s)", len(m.loaded))
	}

	return ansi.Truncate(headerStyle.Render(path)+gutterStyle.Render(right), m.width, "…")
}

func (m model) renderFooter() string {
	switch {
	case m.err != nil:
		return ansi.Truncate(errorStyle.Render(m.err.Error()), m.width, "…")
	case m.mode == modeCompose:
		return ansi.Truncate(footerStyle.Render("ctrl+s save  esc cancel"), m.width, "…")
	case m.notice != "":
		return ansi.Truncate(noticeStyle.Render(m.notice), m.width, "…")
	}

	where := "—"
	if anchor, ok := m.view.GetAnchor(m.cursor); ok {
		where = describeAnchor(*anchor)
	}

	keys := "  c thread  enter open  C threads  n next  S tell agent  q quit"

	return ansi.Truncate(footerStyle.Render(where+keys), m.width, "…")
}

func describeAnchor(anchor Anchor) string {
	side := fmt.Sprintf("new %d", anchor.NewStart)
	if anchor.NewStart == 0 {
		side = fmt.Sprintf("old %d", anchor.OldStart)
	}

	return fmt.Sprintf("%s:%s", anchor.FilePath, side)
}

func describeThread(thread Thread) string {
	line, side := thread.NewStart, "new"
	if line == 0 {
		line, side = thread.OldStart, "old"
	}

	return fmt.Sprintf("%s:%s %d", thread.FilePath, side, line)
}

// renderComposer draws the box a message is typed into, with the line it is about above it so
// the anchor stays visible while writing.
func (m model) renderComposer() string {
	title := "reply"
	if m.editing == "" {
		title = "new thread"
		if anchor, ok := m.view.GetAnchor(m.cursor); ok {
			title = "new thread on " + describeAnchor(*anchor)

			if len(anchor.QuotedLines) > 0 {
				title += "\n" + quotedStyle.Render(ansi.Truncate(anchor.QuotedLines[0], max(10, m.width-8), "…"))
			}
		}
	}

	return composerStyle.Width(max(10, m.width-2)).Render(
		threadMarkStyle.Render(title) + "\n" + m.editor.View())
}

// renderThreads lists the threads on the diff, pinned or not. A thread whose code has been
// rewritten has no mark in the gutter to find it by, so this is where it stays readable. It is
// windowed onto the screen rather than drawn whole, since a review longer than the popup would
// otherwise put its last threads and this screen's own footer past the bottom edge.
func (m model) renderThreads() string {
	title := fmt.Sprintf("%d review thread(s)", len(m.shown))
	if m.scoped {
		title = fmt.Sprintf("%d thread(s) on this line", len(m.shown))
	}

	lines := make([]string, 0, len(m.shown))
	for row, position := range m.shown {
		thread := m.loaded[position]

		prefix := "  "
		if row == m.picked {
			prefix = "> "
		}

		style := threadStyle
		if thread.Resolved {
			style = resolvedStyle
		}

		mark := " "
		if thread.UnreadByHuman {
			mark = unreadMark
		}

		first := ""
		if len(thread.Messages) > 0 {
			first = strings.ReplaceAll(strings.TrimSpace(thread.Messages[0].Body), "\n", " ")
		}

		text := ansi.Truncate(fmt.Sprintf("%s%s %s  %s", prefix, mark, describeThread(thread), first), m.width, "…")
		if row == m.picked {
			lines = append(lines, selectedStyle.Render(text))

			continue
		}

		lines = append(lines, style.Render(text))
	}

	return m.renderScreen(title, lines, windowFrom(len(lines), m.getScreenHeight(), m.picked),
		"enter open  d go to the line  esc back")
}

// renderThread draws one conversation, oldest message first, windowed the same way the thread
// list is: a conversation is as long as the two sides made it, and the keys under it have to
// stay on screen whatever that came to.
func (m model) renderThread() string {
	thread := m.getPicked()
	if thread == nil {
		return ""
	}

	title := describeThread(*thread)
	if thread.Resolved {
		title += "  resolved"
	}

	lines := m.conversationLines(*thread)

	return m.renderScreen(title, lines, clampTop(len(lines), m.getScreenHeight(), m.conversationTop),
		"r reply  x resolve  D delete  j/k scroll  esc back")
}

// conversationLines is the whole conversation as drawn lines: what the thread was written
// against, then each message under whoever wrote it. Scrolling reads the same list the
// drawing does, so a held-down key cannot run the offset past the end of it.
func (m model) conversationLines(thread Thread) []string {
	var lines []string
	for _, quoted := range thread.QuotedLines {
		lines = append(lines, quotedStyle.Render(ansi.Truncate(quoted, m.width, "…")))
	}
	lines = append(lines, "")

	for _, message := range thread.Messages {
		who, style := "you", humanStyle
		if message.ByAgent {
			who, style = m.agentName, agentStyle
			if who == "" {
				who = "the agent"
			}
		}

		lines = append(lines, style.Render(who))
		for _, line := range strings.Split(ansi.Wordwrap(strings.TrimSpace(message.Body), max(20, m.width-2), ""), "\n") {
			lines = append(lines, "  "+line)
		}
		lines = append(lines, "")
	}

	return lines
}

// renderScreen draws a titled screen with a windowed body and its keys along the bottom, which
// is the shape both thread screens have.
func (m model) renderScreen(title string, lines []string, top int, keys string) string {
	var out strings.Builder

	out.WriteString(headerStyle.Render(ansi.Truncate(title, m.width, "…")) + "\n\n")

	height := m.getScreenHeight()
	for index := top; index < min(top+height, len(lines)); index++ {
		out.WriteString(lines[index] + "\n")
	}

	scrolled := ""
	if len(lines) > height {
		scrolled = fmt.Sprintf("  %d-%d of %d", top+1, min(top+height, len(lines)), len(lines))
	}

	out.WriteString("\n" + footerStyle.Render(ansi.Truncate(keys+scrolled, m.width, "…")))

	return out.String()
}

// getScreenHeight is how many body lines a thread screen has room for: everything but its
// title, the blank line under it, and the keys at the bottom with its own blank line.
func (m model) getScreenHeight() int {
	return max(1, m.height-4)
}

// windowFrom keeps a selected row on screen by holding it near the middle, so moving down a
// long list scrolls rather than walking off the bottom edge.
func windowFrom(count, height, selected int) int {
	return clampTop(count, height, selected-height/2)
}

// clampTop pins a scroll offset inside a list, so neither key repeat nor a shorter list can
// scroll past what there is to read.
func clampTop(count, height, top int) int {
	return max(0, min(top, max(0, count-height)))
}

// renderNotify is the menu that tells the agent there is something to read. The threads are
// not sent: the agent is told there is activity and reads the conversation itself.
func (m model) renderNotify() string {
	name := m.agentName
	if name == "" {
		name = "the agent"
	}

	var out strings.Builder

	out.WriteString(headerStyle.Render("tell "+name+" its diff has been reviewed") + "\n\n")
	out.WriteString(threadStyle.Render(
		"One piece of mail, saying how many threads have activity it has not read.") + "\n")
	out.WriteString(threadStyle.Render(
		"It calls list_review_threads for the conversation itself, so anything you") + "\n")
	out.WriteString(threadStyle.Render(
		"rewrite before it looks is simply read as it now stands.") + "\n\n")

	out.WriteString(footerStyle.Render(
		"s tell it    i interrupt it and tell it    esc back"))

	return out.String()
}

func (m model) renderRow(y int) string {
	current, ok := m.view.getRow(y)
	if !ok {
		return ""
	}

	switch current.kind {
	case fileRow:
		return ansi.Truncate(fileStyle.Render(current.text), m.width, "…")
	case metadataRow:
		return ansi.Truncate(metadataStyle.Render("  "+current.text), m.width, "…")
	case hunkRow:
		return ansi.Truncate(hunkStyle.Render(current.text), m.width, "…")
	case lineRow:
	}

	line, ok := m.view.getLine(y)
	if !ok {
		return ""
	}

	background := getBackground(line.Kind, y == m.cursor)
	content := renderTokens(m.view.getStyledLine(current, line), line.Text, background)

	return m.renderGutter(y, line, background) +
		fitWindow(content, m.leftColumn, max(0, m.width-gutterWidth), background)
}

// renderGutter draws the line numbers, what the diff did to the line, and whether a thread is
// pinned to it. A thread the agent has replied to since you last looked is marked differently
// from one you are still waiting on.
func (m model) renderGutter(y int, line diff.Line, background lipgloss.TerminalColor) string {
	mark := " "
	if here := m.view.getThreads(y); len(here) > 0 {
		mark = threadMark
		for _, thread := range here {
			if thread.UnreadByHuman {
				mark = unreadMark
			}
		}
	}

	numbers := fmt.Sprintf("%4s %4s %s",
		formatNumber(line.OldNumber), formatNumber(line.NewNumber), markerFor(line.Kind))

	return gutterStyle.Background(background).Render(numbers) +
		threadMarkStyle.Background(background).Render(mark) +
		gutterStyle.Background(background).Render(" ")
}

func formatNumber(value int) string {
	if value == 0 {
		return ""
	}

	return fmt.Sprint(value)
}

func getBackground(kind diff.LineKind, isCursor bool) lipgloss.TerminalColor {
	switch {
	case isCursor:
		return cursorBackground
	case kind == diff.Addition:
		return addedBackground
	case kind == diff.Deletion:
		return deletedBackground
	}

	return plainBackground
}

// renderTokens draws one lexed line on a background. The tokens are drawn one at a time
// rather than through a chroma formatter, which is what lets the background survive: a
// formatter resets after every token, and a reset drops whatever was painted behind it.
func renderTokens(line styledLine, fallback string, background lipgloss.TerminalColor) string {
	if len(line) == 0 {
		return lipgloss.NewStyle().Background(background).Render(expandTabs(fallback))
	}

	var out strings.Builder
	for _, current := range line {
		style := lipgloss.NewStyle().Background(background)
		if current.foreground != "" {
			style = style.Foreground(lipgloss.Color(current.foreground))
		}

		out.WriteString(style.Render(expandTabs(current.text)))
	}

	return out.String()
}

// fitWindow slides an already-coloured line under the horizontal offset and pads it out to
// the full width, so the background reaches the edge of the pane rather than stopping at the
// end of the text.
func fitWindow(value string, offset, width int, background lipgloss.TerminalColor) string {
	if width <= 0 {
		return ""
	}

	if offset > 0 {
		value = ansi.TruncateLeft(value, offset, "")
	}
	value = ansi.Truncate(value, width, "")

	if padding := width - lipgloss.Width(value); padding > 0 {
		value += lipgloss.NewStyle().Background(background).Render(strings.Repeat(" ", padding))
	}

	return value
}

func expandTabs(value string) string {
	return strings.ReplaceAll(value, "\t", strings.Repeat(" ", tabWidth))
}

func shortCommit(commit string) string {
	if len(commit) > 8 {
		return commit[:8]
	}

	return commit
}
