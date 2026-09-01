package codex

import (
	"regexp"
	"slices"
	"strings"

	"weave/internal/harness"
)

var workingLine = regexp.MustCompile(`(?m)^\s*[^\s\p{L}]?\s*Working\s*\(\s*\d+`)

// interruptTimer leaves the verb open for a release that renames it, but keeps the shape
// of the live line, so prose quoting those words is not work.
var interruptTimer = regexp.MustCompile(`(?m)^\s*[^\s\p{L}]?\s*\p{L}+\s*\(\s*\d+[^)\n]*esc to interrupt`)

var waitingFooter = regexp.MustCompile(`(?i)press enter to (confirm|continue)|enter to submit answer`)

var composerMarker = regexp.MustCompile(`^\s*›(.*)$`)

const liveRegionLines = 30

const waitingRegionLines = 8

const idleRegionLines = 8

var ansiSeq = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// dimSpan is the hint Codex writes into an empty composer, which is text in the frame but not
// text anybody typed.
var dimSpan = regexp.MustCompile(`\x1b\[2m.*?\x1b\[(?:0|22)m`)

// ReadActivity reads what a captured frame says Codex is doing, counting only a frame showing
// the composer as idle.
func (h *Harness) ReadActivity(frame string) harness.Activity {
	switch {
	case frame == "":
		return harness.ActivityUnknown
	// The bottom line is read first because the question Codex asks with its own tool offers to
	// interrupt the turn on the same line as the key that answers it.
	case doesFrameShowPromptFooter(frame):
		return harness.ActivityWaiting
	case doesFrameShowWork(frame):
		return harness.ActivityWorking
	case doesFrameShowWaiting(frame):
		return harness.ActivityWaiting
	case doesFrameShowComposer(frame):
		if doesComposerHoldText(frame) {
			return harness.ActivityComposing
		}

		return harness.ActivityIdle
	default:
		return harness.ActivityUnrecognized
	}
}

func tailLines(frame string, n int) string {
	frame = ansiSeq.ReplaceAllString(frame, "")
	lines := strings.Split(strings.TrimRight(frame, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}

	return strings.Join(lines, "\n")
}

func doesFrameShowWork(frame string) bool {
	region := tailLines(frame, liveRegionLines)

	return workingLine.MatchString(region) || interruptTimer.MatchString(region)
}

func doesFrameShowWaiting(frame string) bool {
	return waitingFooter.MatchString(tailLines(frame, waitingRegionLines))
}

func doesFrameShowComposer(frame string) bool {
	_, ok := composerText(frame)

	return ok
}

func doesComposerHoldText(frame string) bool {
	held, _ := composerText(frame)

	return strings.TrimSpace(held) != ""
}

// composerText is what the composer holds, taken from the lowest marker in the region because
// the transcript echoes a message somebody sent with the same glyph the composer wears.
func composerText(frame string) (string, bool) {
	drawn := strings.Split(strings.TrimRight(frame, "\n"), "\n")
	if len(drawn) > idleRegionLines {
		drawn = drawn[len(drawn)-idleRegionLines:]
	}

	// Stripping line by line keeps the two readings of the region the same length, where
	// stripping the region whole would drop a line that came down to nothing.
	lines := make([]string, len(drawn))
	for i, line := range drawn {
		lines[i] = ansiSeq.ReplaceAllString(dimSpan.ReplaceAllString(line, ""), "")
		drawn[i] = ansiSeq.ReplaceAllString(line, "")
	}

	marker := -1
	for i, line := range lines {
		if composerMarker.MatchString(line) {
			marker = i
		}
	}

	if marker < 0 {
		return "", false
	}

	// The composer has no rule under it, so what it holds runs down to the lowest line Codex
	// draws below it, dim hints included, since those are the frame's own furniture.
	footer := marker
	for i := marker + 1; i < len(drawn); i++ {
		if strings.TrimSpace(drawn[i]) != "" {
			footer = i
		}
	}

	held := []string{composerMarker.FindStringSubmatch(lines[marker])[1]}
	if footer > marker {
		held = append(held, lines[marker+1:footer]...)
	}

	return strings.Join(held, "\n"), true
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
