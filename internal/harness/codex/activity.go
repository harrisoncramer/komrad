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

var idleComposer = regexp.MustCompile(`(?m)^\s*›`)

const liveRegionLines = 30

const waitingRegionLines = 8

const idleRegionLines = 8

var ansiSeq = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// ReadActivity reads what a captured frame says Codex is doing. Only a frame that shows
// the composer is idle, so a pane whose Codex has not drawn its input box yet is
// ActivityUnrecognized rather than an agent ready to be typed at.
//
// An interrupted turn still reads as finished, since escaping clears the live line but
// leaves the composer drawn, which the composer match accepts.
//
// The bottom line is read first because the question Codex asks with its own tool offers
// to interrupt the turn on the same line as the key that answers it.
func (h *Harness) ReadActivity(frame string) harness.Activity {
	switch {
	case frame == "":
		return harness.ActivityUnknown
	case doesFrameShowPromptFooter(frame):
		return harness.ActivityWaiting
	case doesFrameShowWork(frame):
		return harness.ActivityWorking
	case doesFrameShowWaiting(frame):
		return harness.ActivityWaiting
	case doesFrameShowComposer(frame):
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
	return idleComposer.MatchString(tailLines(frame, idleRegionLines))
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
