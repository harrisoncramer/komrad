package claude

import (
	"regexp"
	"strings"

	"weave/internal/harness"
)

var (
	workingLine  = regexp.MustCompile(`(?m)^\s*[^\s\p{L}⎿❯│─╭╮╰╯-]\s+\p{L}.*…`)
	workingTimer = regexp.MustCompile(`…\s*\(\s*\d+m?\s*\d*\s*s`)
)

var waitingFooter = regexp.MustCompile(`(?i)esc to cancel|enter to select`)

var boxRule = regexp.MustCompile(`^─{3,}\s*$`)

const promptMarker = "❯"

const liveRegionLines = 30

const waitingRegionLines = 6

const idleRegionLines = 6

var ansiSeq = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// ReadActivity reads what a captured frame says Claude is doing, counting only a frame showing
// the empty input box as idle and anything else unrecognised.
func (h *Harness) ReadActivity(frame string) harness.Activity {
	switch {
	case frame == "":
		return harness.ActivityUnknown
	case doesFrameShowWork(frame):
		return harness.ActivityWorking
	case doesFrameShowWaiting(frame):
		return harness.ActivityWaiting
	case doesFrameShowIdleInputBox(frame):
		if doesInputBoxHoldText(frame) {
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
	return workingLine.MatchString(region) || workingTimer.MatchString(region)
}

func doesFrameShowWaiting(frame string) bool {
	return waitingFooter.MatchString(tailLines(frame, waitingRegionLines))
}

func doesFrameShowIdleInputBox(frame string) bool {
	_, ok := inputBox(frame)

	return ok
}

// doesInputBoxHoldText reports whether somebody has typed into the box, which is any text at
// all, since Claude draws no placeholder in an empty one.
func doesInputBoxHoldText(frame string) bool {
	held, _ := inputBox(frame)

	return strings.TrimSpace(held) != ""
}

// inputBox is what the box holds, which is every line between the rule that opens it and the
// rule that closes it, so that text typed under the marker counts as much as text beside it.
func inputBox(frame string) (string, bool) {
	lines := strings.Split(tailLines(frame, idleRegionLines), "\n")

	for i, line := range lines {
		if !boxRule.MatchString(line) || i+1 >= len(lines) {
			continue
		}

		rest, ok := strings.CutPrefix(lines[i+1], promptMarker)
		if !ok {
			continue
		}

		held := []string{rest}
		for _, line := range lines[i+2:] {
			if boxRule.MatchString(line) {
				break
			}

			held = append(held, line)
		}

		return strings.Join(held, "\n"), true
	}

	return "", false
}
