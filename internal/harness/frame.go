package harness

import (
	"regexp"
	"strings"
)

const dimOpen = "\x1b[2m"

var dimSpan = regexp.MustCompile(`\x1b\[2m.*?\x1b\[(?:0|22)m`)

// StripDim removes from a captured region the runs a CLI draws dim, which is text the frame
// holds but nobody typed.
func StripDim(lines []string) []string {
	// The region keeps its line count, since a caller reads it alongside the same region read
	// whole.
	stripped := make([]string, len(lines))

	open := false
	for i, line := range lines {
		// A run left open at the end of its line carries on into the next one, because tmux
		// writes an attribute at the cell it changes on, which is also why the reset ending a
		// run arrives before the first character drawn bright.
		if open {
			line = dimOpen + line
		}

		line = dimSpan.ReplaceAllString(line, "")

		if cut := strings.Index(line, dimOpen); cut >= 0 {
			line = line[:cut]
			open = true
		} else {
			open = false
		}

		stripped[i] = line
	}

	return stripped
}
