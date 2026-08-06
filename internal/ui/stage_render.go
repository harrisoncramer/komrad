package ui

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// Terminal style detection would compete with the panel event loop for input.
func render(markdown string, wrap int) string {
	return renderFlash(markdown, wrap, 0)
}

// renderFlash is render, but the flashField-th record field (1-based, 0 for none) has
// its whole line highlighted instead of just its marker dimmed: the brief flash
// that confirms which field a digit key just copied.
func renderFlash(markdown string, wrap, flashField int) string {
	options := []glamour.TermRendererOption{glamour.WithStyles(stageStyle)}
	if wrap > 0 {
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

	return styleNumberedLines(rendered, flashField)
}

// numberStyle is how a record field's leading "N)" is drawn once glamour has laid the
// line out. glamour's own Enumeration style never reaches this text: an ordered list
// item renders its marker through the surrounding block's style rather than its own,
// so recolouring Enumeration in stageStyle has no visible effect. Dimming it here
// after the fact is what actually sets the marker apart from the value beside it,
// which a digit key copies and so is left in the plain foreground.
var numberStyle = ansi.NewStyle().Faint()

// numberPrefix matches a record field's rendered "N) " marker, glamour's own left
// margin ahead of it included, so styleNumberedLines can style just the marker (or
// the whole line, for a flash) and leave that margin where it is.
var numberPrefix = regexp.MustCompile(`^(\s*)(\d+\)) `)

// styleNumberedLines dims every record field's leading "N)" in already-rendered text,
// except flashField's own line (1-based, counting only numbered lines; 0 flashes
// none), which is highlighted in full instead. It is a no-op on a document with no
// numbered lines, which is every document that is not a record.
func styleNumberedLines(rendered string, flashField int) string {
	lines := strings.Split(rendered, "\n")
	seen := 0

	for i, line := range lines {
		if !numberPrefix.MatchString(line) {
			continue
		}

		seen++
		lines[i] = numberPrefix.ReplaceAllStringFunc(line, func(match string) string {
			groups := numberPrefix.FindStringSubmatch(match)

			return groups[1] + numberStyle.Styled(groups[2]) + " "
		})

		if seen == flashField {
			lines[i] = flashValue(lines[i])
		}
	}

	return strings.Join(lines, "\n")
}

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

// flashStyle is the copy flash: the palette's accent as a background, the same
// pairing FocusedButton uses for the one other thing on screen you have just
// acted on. It is forced onto termenv's ANSI profile so lipgloss emits it
// unconditionally, rather than gating it behind detecting weave's own terminal,
// which is not the tmux pane a flash actually renders into.
var flashStyle = func() lipgloss.Style {
	renderer := lipgloss.NewRenderer(nil)
	renderer.SetColorProfile(termenv.ANSI)

	return renderer.NewStyle().Background(colorAccent).Foreground(lipgloss.Color("0"))
}()

// lineReset matches a reset an already-rendered line carries partway through it,
// the point past which flashing a span as a whole would leave it plain.
var lineReset = regexp.MustCompile(`\x1b\[0?m`)

// flashLine highlights a whole already-rendered span by styling each piece between
// its own resets on its own, so that reset does not cancel the highlight partway
// through it.
func flashLine(line string) string {
	spans := lineReset.Split(line, -1)
	for i, span := range spans {
		spans[i] = flashStyle.Render(span)
	}

	return strings.Join(spans, "")
}

// flashValue highlights only a record field line's value, the "label: " ahead of
// it (renderRecord's own separator) left in its usual dimmed-marker, plain-label
// styling. It is the value alone a digit key copies, so it is the only part of the
// line that flashes. Glamour pads a line's value out to the wrap width with plain
// trailing spaces, which is left alone rather than swept into the flash.
func flashValue(line string) string {
	sep := strings.Index(line, ": ")
	if sep < 0 {
		return flashLine(line)
	}

	head, rest := line[:sep+2], line[sep+2:]
	value := strings.TrimRight(rest, " ")
	padding := rest[len(value):]

	return head + flashLine(value) + padding
}

func writeStageFile(path, content string) bool {
	return os.WriteFile(path, []byte(content), 0o644) == nil
}

// The pager must remain running or tmux will close its pane.
func pagerArgv(path string, line int) []string {
	argv := []string{"less", "-R", "-X", "-Ps"}
	if line > 1 {
		argv = append(argv, fmt.Sprintf("+%d", line))
	}

	return append(argv, path)
}
