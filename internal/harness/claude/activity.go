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

// retryLine is the banner Claude draws while it retries an API error, keyed on a countdown that
// closes its own line rather than on the status code it names, since the code varies with
// whatever the API returned and prose quoting the banner reads as the banner otherwise.
var retryLine = regexp.MustCompile(`(?mi)^\s*[^\s\p{L}⎿❯│─╭╮╰╯-]\s+.*retrying in\s.*attempt\s+\d+\s*/\s*\d+\s*$`)

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

func tailLines(frame string, count int) string {
	return strings.Join(tailRegion(ansiSeq.ReplaceAllString(frame, ""), count), "\n")
}

func tailRegion(frame string, count int) []string {
	lines := strings.Split(strings.TrimRight(frame, "\n"), "\n")
	if len(lines) > count {
		lines = lines[len(lines)-count:]
	}

	return lines
}

func doesFrameShowWork(frame string) bool {
	region := tailLines(frame, liveRegionLines)
	return workingLine.MatchString(region) || workingTimer.MatchString(region) || retryLine.MatchString(region)
}

func doesFrameShowWaiting(frame string) bool {
	return waitingFooter.MatchString(tailLines(frame, waitingRegionLines))
}

func doesFrameShowIdleInputBox(frame string) bool {
	_, ok := inputBox(frame)

	return ok
}

// doesInputBoxHoldText reports whether somebody has typed into the box, which is any text the
// box holds that Claude did not draw itself.
func doesInputBoxHoldText(frame string) bool {
	held, _ := inputBox(frame)

	return strings.TrimSpace(held) != ""
}

// inputBox is what the box holds, which is every line between the rule that opens it and the
// rule that closes it, so that text typed under the marker counts as much as text beside it,
// less the prompt Claude suggests there itself, which is nobody's unfinished work.
func inputBox(frame string) (string, bool) {
	drawn := tailRegion(frame, idleRegionLines)

	lines := harness.StripDim(drawn)
	for i, line := range lines {
		lines[i] = ansiSeq.ReplaceAllString(line, "")
	}

	for i, line := range drawn {
		drawn[i] = ansiSeq.ReplaceAllString(line, "")
	}

	for i, line := range drawn {
		if !boxRule.MatchString(line) || i+1 >= len(drawn) {
			continue
		}

		rest, ok := strings.CutPrefix(lines[i+1], promptMarker)
		if !ok {
			continue
		}

		held := []string{rest}
		for j, line := range drawn[i+2:] {
			if boxRule.MatchString(line) {
				break
			}

			held = append(held, lines[i+2+j])
		}

		return strings.Join(held, "\n"), true
	}

	return "", false
}
