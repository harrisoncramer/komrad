package claude

import (
	"regexp"
	"strings"

	"weave/internal/harness"
)

var (
	workingLine  = regexp.MustCompile(`^\s*[^\s\p{L}⎿❯│─╭╮╰╯-]\s+\p{L}.*…`)
	workingTimer = regexp.MustCompile(`…\s*\(\s*\d+m?\s*\d*\s*s`)
)

var waitingFooter = regexp.MustCompile(`(?i)esc to cancel|enter to select`)

// optionCursor is the cursor Claude draws beside the chosen answer of a question, which is the
// only sign of one left when a tall preview beside the options pushes the footer off the
// bottom of the pane, and which a bordered dialog draws inside its own frame.
var optionCursor = regexp.MustCompile(`^\s*│?\s*❯\s+\d+\.\s`)

var boxRule = regexp.MustCompile(`^─{3,}\s*$`)

// transcriptMarker opens a line of the conversation above the box, whether a tool call, an
// answer, a person's own turn, or one of the notes Claude ends a turn with.
var transcriptMarker = regexp.MustCompile(`^\s*[⏺❯※]`)

// hintLine is the note Claude draws under a status line, which sits between that line and the
// box without ending the region a status can be found in.
var hintLine = regexp.MustCompile(`^\s*⎿`)

const promptMarker = "❯"

// boxSearchLines is how far up the rule opening the input box is looked for, which reaches
// well past the box itself since one holding a long unsent message is several lines tall.
const boxSearchLines = 30

const waitingRegionLines = 6

var ansiSeq = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// inputBox is where Claude draws its prompt and the anchor every other reading of a frame is
// taken from, holding the index of the rule that opens it and the text it carries, less the
// prompt Claude suggests there itself.
type inputBox struct {
	Rule int
	Held string
}

// ReadActivity reads what a captured frame says Claude is doing, taking every reading
// relative to the input box rather than from a count of lines off the bottom, since the
// conversation above it holds the same glyphs and phrases a live status line does.
func (h *Harness) ReadActivity(frame string) harness.Activity {
	if frame == "" {
		return harness.ActivityUnknown
	}

	lines, held := frameLines(frame)
	box, boxed := findInputBox(lines, held)

	switch {
	case doesFrameShowWork(lines, box, boxed):
		return harness.ActivityWorking
	case doesFrameShowWaiting(lines, boxed):
		return harness.ActivityWaiting
	case boxed:
		if strings.TrimSpace(box.Held) != "" {
			return harness.ActivityComposing
		}

		return harness.ActivityIdle
	default:
		return harness.ActivityUnrecognized
	}
}

// frameLines splits a captured frame into the lines its structure is read from and the same
// lines less the text Claude draws dim, which is the prompt it suggests rather than anything
// anybody typed, taken off first because dim is itself an escape sequence.
func frameLines(frame string) ([]string, []string) {
	drawn := strings.Split(strings.TrimRight(frame, "\n"), "\n")
	held := harness.StripDim(drawn)

	lines := make([]string, len(drawn))
	for i, line := range drawn {
		lines[i] = ansiSeq.ReplaceAllString(line, "")
		held[i] = ansiSeq.ReplaceAllString(held[i], "")
	}

	return lines, held
}

// findInputBox is the box nearest the bottom of the frame, which is the rule carrying the
// prompt marker on the line under it, since the rules opening and closing a box are drawn
// alike and the marker is the only thing that tells them apart.
func findInputBox(lines, held []string) (inputBox, bool) {
	for i := len(lines) - 2; i >= 0 && i >= len(lines)-boxSearchLines; i-- {
		if !boxRule.MatchString(lines[i]) {
			continue
		}

		if !strings.HasPrefix(lines[i+1], promptMarker) {
			continue
		}

		// The dim pass can blank the marker's own line, which is a box holding nothing rather
		// than no box at all, so the marker is read from the frame and the text from the frame
		// less what Claude drew dim.
		rest, _ := strings.CutPrefix(held[i+1], promptMarker)

		typed := []string{rest}
		for j := i + 2; j < len(lines) && !boxRule.MatchString(lines[j]); j++ {
			typed = append(typed, held[j])
		}

		return inputBox{Rule: i, Held: strings.Join(typed, "\n")}, true
	}

	return inputBox{}, false
}

// doesFrameShowWork reads only the lines between the box and the conversation above it, since
// that is where Claude draws what it is doing and everything higher is transcript carrying the
// same ellipses and timers a status line does.
func doesFrameShowWork(lines []string, box inputBox, boxed bool) bool {
	for _, line := range statusRegion(lines, box, boxed) {
		if workingLine.MatchString(line) || workingTimer.MatchString(line) {
			return true
		}
	}

	return false
}

// statusRegion is the lines between the box and the conversation above it, which is the
// blanks and hints Claude draws under a status line and the status line itself, taking only
// the lines starting in the first column since Claude indents everything it says under a
// marker and keeps that column for its own.
func statusRegion(lines []string, box inputBox, boxed bool) []string {
	start := len(lines) - 1
	if boxed {
		start = box.Rule - 1
	}

	var region []string

	for i := start; i >= 0; i-- {
		switch {
		case transcriptMarker.MatchString(lines[i]):
			return region
		case boxRule.MatchString(lines[i]):
			// A rule ends the region only under a box that was found, since with no box it is
			// furniture the status line sits over rather than the edge of anything.
			if boxed {
				return region
			}

			continue
		case strings.TrimSpace(lines[i]) == "", hintLine.MatchString(lines[i]):
			continue
		case strings.HasPrefix(lines[i], " "), strings.HasPrefix(lines[i], "\t"):
			continue
		}

		region = append(region, lines[i])
	}

	return region
}

// doesFrameShowWaiting reads the footer under a question and falls back to the cursor beside
// its options for one tall enough to have pushed that footer off the pane, reading neither
// while the box is drawn since a question replaces it.
func doesFrameShowWaiting(lines []string, boxed bool) bool {
	if boxed {
		return false
	}

	region := lines
	if len(region) > waitingRegionLines {
		region = region[len(region)-waitingRegionLines:]
	}

	if waitingFooter.MatchString(strings.Join(region, "\n")) {
		return true
	}

	// The whole frame, rather than a window off the bottom: what pushes the footer off the pane
	// is a preview under the cursor, so a question tall enough to lose its footer is tall enough
	// to have pushed the cursor out of any window worth naming.
	for _, line := range lines {
		if optionCursor.MatchString(line) {
			return true
		}
	}

	return false
}
