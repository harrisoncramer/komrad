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

// optionCursor is the cursor Claude draws beside the chosen answer of a question, indented
// when a bordered dialog draws it inside its own frame.
var optionCursor = regexp.MustCompile(`^\s*│?\s*❯\s+\d+\.\s`)

// boxRule is an edge of the input box, the top one carrying the conversation's title.
var boxRule = regexp.MustCompile(`^─{3,}(\s\S.*─)?\s*$`)

// panelRule is the rule Claude draws across the top of a panel it opens over the whole pane,
// which eats every key typed at the pane until somebody dismisses it.
var panelRule = regexp.MustCompile(`^\s*▔{20,}`)

// transcriptMarker opens a line of the conversation above the box.
var transcriptMarker = regexp.MustCompile(`^\s*[⏺❯※]`)

// dialogBorder is the frame Claude draws a question inside, which ends a status region.
var dialogBorder = regexp.MustCompile(`^[╭╮╰╯│]`)

// hintLine is the note Claude draws under a status line.
var hintLine = regexp.MustCompile(`^\s*⎿`)

// The markers opening the line the box holds, the second being the box in shell mode.
const (
	chatPrompt  = "❯"
	shellPrompt = "!"
)

var promptMarkers = []string{chatPrompt, shellPrompt}

// boxSearchLines is how far up the rule opening the input box is looked for.
const boxSearchLines = 30

const waitingRegionLines = 6

var ansiSeq = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// inputBox is the rule opening the box, the marker it holds, and the lines the box holds less
// the prompt Claude suggests there itself.
type inputBox struct {
	Rule   int
	Marker string
	Held   []string
}

// ReadInput reads what a captured frame says Claude is doing and what its box holds, measured
// from the input box rather than from the bottom of the frame.
func (h *Harness) ReadInput(frame string) harness.Input {
	if frame == "" {
		return harness.Input{Activity: harness.ActivityUnknown}
	}

	lines, held, colours := frameLines(frame)
	box, boxed := findInputBox(lines, held)

	switch {
	case doesFrameShowWork(lines, box, boxed), isPromptGreyed(colours, box, boxed):
		return harness.Input{Activity: harness.ActivityWorking}
	case doesFrameShowWaiting(lines, boxed):
		return harness.Input{Activity: harness.ActivityWaiting}
	case boxed:
		input := harness.Input{Activity: harness.ActivityIdle, Held: box.Held}
		if strings.TrimSpace(input.Text()) != "" {
			input.Activity = harness.ActivityComposing
		}

		return input
	default:
		return harness.Input{Activity: harness.ActivityUnrecognized}
	}
}

// frameLines splits a captured frame into the lines its structure is read from, the same lines
// less the text Claude draws dim, and the colour each line's first character is drawn in.
func frameLines(frame string) (lines, held, colours []string) {
	drawn := strings.Split(strings.TrimRight(frame, "\n"), "\n")
	held = harness.StripDim(drawn)
	colours = harness.Foregrounds(drawn)

	lines = make([]string, len(drawn))
	for i, line := range drawn {
		lines[i] = ansiSeq.ReplaceAllString(line, "")
		held[i] = ansiSeq.ReplaceAllString(held[i], "")
	}

	return lines, held, colours
}

// findInputBox is the box nearest the bottom of the frame, being the rule carrying the prompt
// marker on the line under it.
func findInputBox(lines, held []string) (inputBox, bool) {
	for i := len(lines) - 2; i >= 0 && i >= len(lines)-boxSearchLines; i-- {
		if !boxRule.MatchString(lines[i]) {
			continue
		}

		marker, prompted := readOpeningMarker(lines[i+1])
		if !prompted {
			continue
		}

		// A dim-blanked marker is a box holding nothing rather than no box, so the marker is
		// read from the frame and the text from the frame less the dim.
		rest, _ := strings.CutPrefix(held[i+1], marker)

		typed := []string{rest}
		closed := false

		for j := i + 2; j < len(lines); j++ {
			if boxRule.MatchString(lines[j]) {
				closed = true

				break
			}

			typed = append(typed, held[j])
		}

		// A frame cut off at the marker is a box the capture caught half of rather than no box.
		if !closed && i+1 < len(lines)-1 {
			continue
		}

		return inputBox{Rule: i, Marker: marker, Held: typed}, true
	}

	return inputBox{}, false
}

// readOpeningMarker reads the prompt marker a line opens with, when it opens with one.
func readOpeningMarker(line string) (string, bool) {
	for _, marker := range promptMarkers {
		if strings.HasPrefix(line, marker) {
			return marker, true
		}
	}

	return "", false
}

// doesFrameShowWork reads only the lines between the box and the conversation above it.
func doesFrameShowWork(lines []string, box inputBox, boxed bool) bool {
	for _, line := range statusRegion(lines, box, boxed) {
		if workingLine.MatchString(line) || workingTimer.MatchString(line) {
			return true
		}
	}

	return false
}

// isPromptGreyed reports whether the box draws its prompt marker in the colour of the rule
// above it rather than in the terminal's own, which is Claude greying the box out for a turn.
func isPromptGreyed(colours []string, box inputBox, boxed bool) bool {
	// Shell mode draws the whole box in one colour, marker included.
	if !boxed || box.Marker != chatPrompt {
		return false
	}

	// A box with no colour on it at all says nothing either way, which is what a capture taken
	// without the colours looks like, so the rule has to carry a colour for the marker to match.
	rule := colours[box.Rule]

	return rule != "" && rule == colours[box.Rule+1]
}

// statusRegion is the lines between the box and the conversation above it, taking only the first
// column, which Claude keeps for its own status lines.
func statusRegion(lines []string, box inputBox, boxed bool) []string {
	start := len(lines) - 1
	if boxed {
		start = box.Rule - 1
	}

	var region []string

	for i := start; i >= 0; i-- {
		switch {
		case transcriptMarker.MatchString(lines[i]), dialogBorder.MatchString(lines[i]):
			return region
		case boxRule.MatchString(lines[i]):
			// With no box, a rule is furniture the status line sits over rather than an edge.
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

// doesFrameShowWaiting reads the footer under a question, or the cursor beside its options when
// the footer has been pushed off the pane.
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

	// A panel is read off its own rule rather than off a footer, which is the first thing the pane
	// cuts off.
	for _, line := range lines {
		if panelRule.MatchString(line) {
			return true
		}
	}

	// The whole frame rather than a window off the bottom, a preview that pushes the footer off the
	// pane pushing the cursor up with it.
	for _, line := range lines {
		if optionCursor.MatchString(line) {
			return true
		}
	}

	return false
}
