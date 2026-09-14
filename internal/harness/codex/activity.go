package codex

import (
	"regexp"
	"slices"
	"strings"

	"weave/internal/harness"
)

var workingLine = regexp.MustCompile(`(?m)^\s*[^\s\p{L}]?\s*Working\s*\(\s*\d+`)

// interruptTimer leaves open both the verb and whatever a step names between the verb and the
// timer.
var interruptTimer = regexp.MustCompile(`^\s*[^\s\p{L}]?\s*\p{L}.*\(\s*\d+[^)\n]*esc to interrupt`)

var waitingFooter = regexp.MustCompile(`(?i)press enter to (confirm|continue)|enter to submit answer`)

var composerMarker = regexp.MustCompile(`^\s*›(.*)$`)

var slashWord = regexp.MustCompile(`^\s*/\S*$`)

var searchMenuHint = regexp.MustCompile(`esc close.*switch search modes`)

const liveRegionLines = 30

const waitingRegionLines = 8

// composerSearchLines is how far up the composer marker is looked for, which reaches past the
// menus Codex draws under the composer.
const composerSearchLines = 30

var ansiSeq = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// ReadInput reads what a captured frame says Codex is doing and what its composer holds,
// counting only a frame showing the composer as idle.
func (h *Harness) ReadInput(frame string) harness.Input {
	switch {
	case frame == "":
		return harness.Input{Activity: harness.ActivityUnknown}
	// The bottom line is read first because the question Codex asks with its own tool offers to
	// interrupt the turn on the same line as the key that answers it.
	case doesFrameShowPromptFooter(frame):
		return harness.Input{Activity: harness.ActivityWaiting}
	case doesFrameShowWork(frame):
		return harness.Input{Activity: harness.ActivityWorking}
	case doesFrameShowWaiting(frame):
		return harness.Input{Activity: harness.ActivityWaiting}
	default:
		held, composing := composerText(frame)
		if !composing {
			return harness.Input{Activity: harness.ActivityUnrecognized}
		}

		input := harness.Input{Activity: harness.ActivityIdle, Held: held}
		if strings.TrimSpace(input.Text()) != "" {
			input.Activity = harness.ActivityComposing
		}

		return input
	}
}

func tailLines(frame string, count int) string {
	frame = ansiSeq.ReplaceAllString(frame, "")
	lines := strings.Split(strings.TrimRight(frame, "\n"), "\n")
	if len(lines) > count {
		lines = lines[len(lines)-count:]
	}

	return strings.Join(lines, "\n")
}

func doesFrameShowWork(frame string) bool {
	region := tailLines(frame, liveRegionLines)

	return workingLine.MatchString(region) || doesFrameOfferInterrupt(frame)
}

// doesFrameOfferInterrupt reads the live line offering to interrupt the turn, whose timer has to
// be in the line as drawn and gone from the same line less the dim.
func doesFrameOfferInterrupt(frame string) bool {
	// Codex draws that timer dim and a message quoting the words does not, which is what carries
	// the pattern being loose enough to take a label as well as a verb.
	drawn := strings.Split(strings.TrimRight(frame, "\n"), "\n")
	if len(drawn) > liveRegionLines {
		drawn = drawn[len(drawn)-liveRegionLines:]
	}

	held := harness.StripDim(drawn)

	for i, line := range drawn {
		if !interruptTimer.MatchString(ansiSeq.ReplaceAllString(line, "")) {
			continue
		}

		rest := ansiSeq.ReplaceAllString(held[i], "")

		// Codex draws the label bright and only the timer dim, so a line that comes down to
		// nothing was dim from end to end, which is how it draws tool output rather than status.
		if strings.TrimSpace(rest) == "" {
			continue
		}

		if !interruptTimer.MatchString(rest) {
			return true
		}
	}

	return false
}

func doesFrameShowWaiting(frame string) bool {
	return waitingFooter.MatchString(tailLines(frame, waitingRegionLines))
}

// composerText is the lines the composer holds, looked for at the bottom of the region because
// the transcript echoes a sent message with the same glyph the composer wears.
func composerText(frame string) ([]string, bool) {
	drawn := strings.Split(strings.TrimRight(frame, "\n"), "\n")
	if len(drawn) > composerSearchLines {
		drawn = drawn[len(drawn)-composerSearchLines:]
	}

	lines := harness.StripDim(drawn)
	for i, line := range lines {
		lines[i] = ansiSeq.ReplaceAllString(line, "")
	}

	for i, line := range drawn {
		drawn[i] = ansiSeq.ReplaceAllString(line, "")
	}

	// A dim-blanked marker is a composer holding nothing rather than no composer, so the marker
	// is read from the frame and the text from the frame less the dim.
	marker := -1

	// Codex draws the file-search menu where the footer would be and draws the row it has
	// selected at the first column.
	menu := searchMenuTop(drawn)

	bottom := len(drawn) - 1
	if menu >= 0 {
		bottom = menu
	}

	// The composer is the lowest line Codex draws at the first column, with indented furniture
	// under it and the transcript above.
	for i := bottom; i >= 0; i-- {
		if strings.TrimSpace(drawn[i]) == "" || strings.HasPrefix(drawn[i], " ") || strings.HasPrefix(drawn[i], "\t") {
			continue
		}

		if composerMarker.MatchString(drawn[i]) {
			marker = i
		}

		break
	}

	if marker < 0 {
		return nil, false
	}

	held := []string{""}
	if typed := composerMarker.FindStringSubmatch(lines[marker]); typed != nil {
		held[0] = typed[1]
	}

	// The composer has no rule under it, so what it holds runs down to the lowest line Codex
	// draws below it, dim hints included.
	end := marker + 1

	for i := len(drawn) - 1; i > marker; i-- {
		if strings.TrimSpace(drawn[i]) != "" {
			end = i

			break
		}
	}

	if menu >= 0 {
		end = menu
	}

	// Codex opens the slash menu for a box holding one slash word and nothing else, and draws it
	// where the footer would be.
	if slashWord.MatchString(held[0]) {
		for i := end; i > marker; i-- {
			if strings.TrimSpace(drawn[i]) == "" {
				end = i

				break
			}
		}
	}

	if end > marker+1 {
		held = append(held, lines[marker+1:end]...)
	}

	return held, true
}

// searchMenuTop is the blank line separating the composer from an open file-search menu, or -1
// for a frame whose lowest line is not that menu's hint.
func searchMenuTop(drawn []string) int {
	hint := -1

	for i := len(drawn) - 1; i >= 0; i-- {
		if strings.TrimSpace(drawn[i]) == "" {
			continue
		}

		if searchMenuHint.MatchString(drawn[i]) {
			hint = i
		}

		break
	}

	if hint < 0 {
		return -1
	}

	i := hint - 1
	for i >= 0 && strings.TrimSpace(drawn[i]) == "" {
		i--
	}

	for i >= 0 && strings.TrimSpace(drawn[i]) != "" {
		// Codex draws the menu under the composer.
		if composerMarker.MatchString(drawn[i]) {
			return -1
		}

		i--
	}

	return i
}

func doesFrameShowPromptFooter(frame string) bool {
	return waitingFooter.MatchString(lastFilledLine(frame))
}

func lastFilledLine(frame string) string {
	lines := strings.Split(ansiSeq.ReplaceAllString(frame, ""), "\n")
	for _, line := range slices.Backward(lines) {
		if strings.TrimSpace(line) != "" {
			return line
		}
	}

	return ""
}
