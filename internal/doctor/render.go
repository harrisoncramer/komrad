package doctor

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// The escapes a status is drawn in, matching what the log reader already uses for a level
// so the two commands look like they come from the same tool.
var statusColors = map[Status]string{
	StatusOK:   "\033[32m",
	StatusWarn: "\033[33m",
	StatusFail: "\033[31m",
	StatusSkip: "\033[90m",
}

const colorReset = "\033[0m"

// markerWidth is how wide every marker is, which they all share so the columns after them
// do not move with the status.
const markerWidth = 2

// markers are what a status is drawn as. They are ASCII rather than glyphs because this
// output's second home is a pasted issue, where a terminal's idea of a symbol is not the
// reader's.
var markers = map[Status]string{
	StatusOK:   "ok",
	StatusWarn: "--",
	StatusFail: "xx",
	StatusSkip: "..",
}

// The columns a finding is drawn in: a gutter, the marker, a gap, the name padded to the
// widest, then this before what the check found. A fix is indented past all of it so it
// sits under the reason it repairs.
const (
	gutter  = "  "
	gap     = "   "
	nameGap = "  "
)

// WriteJSON writes the report as the machine-readable document, indented because the
// reader is as often a person running --json as it is a script.
func WriteJSON(out io.Writer, report Report) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("failed to write the report: %w", err)
	}

	return nil
}

// WriteText writes the report for a person: one line per check, and an indented fix under
// anything that needs one.
func WriteText(out io.Writer, report Report, color bool) error {
	var page strings.Builder
	width := nameWidth(report.Findings)
	indent := strings.Repeat(" ", len(gutter)+markerWidth+len(gap)+width+len(nameGap))

	page.WriteString("\n")
	for _, finding := range report.Findings {
		name := fmt.Sprintf("%-*s", width, finding.Name)
		page.WriteString(gutter + marker(finding.Status, color) + gap + name + nameGap + line(finding) + "\n")

		// Only under a finding that is not ok, since a fix is an instruction and a
		// passing check has nothing to instruct.
		if finding.Status != StatusOK && finding.Fix != "" {
			page.WriteString(dim(indent+"fix: "+finding.Fix, color) + "\n")
		}
	}

	page.WriteString("\n" + summaryLine(report.Summary) + "\n")

	if _, err := io.WriteString(out, page.String()); err != nil {
		return fmt.Errorf("failed to write the report: %w", err)
	}

	return nil
}

// line is what a check reports beside its name: what it saw when that is the answer, and
// why it is wrong when it is.
//
// Anything not ok shows the reason alone. Every problem is written to stand on its own, so
// pairing it with the observation it was drawn from would say the same thing twice. The
// observation is still in the report for a reader of the JSON.
func line(finding Finding) string {
	if finding.Error != "" {
		return oneLine(finding.Error)
	}

	return oneLine(finding.Detail)
}

// oneLine folds a reason onto the row it has to fit on. A parser reports what it objected
// to over several lines, and one of those landing in the middle of the table takes the
// columns with it. The unfolded text is still in the JSON.
func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// nameWidth is how wide the name column has to be for the details to line up down the
// page, which is what makes a list of twelve checks scannable rather than read.
func nameWidth(findings []Finding) int {
	width := 0
	for _, finding := range findings {
		if len(finding.Name) > width {
			width = len(finding.Name)
		}
	}

	return width
}

// summaryLine is the last thing on screen, and says what to do rather than only counting.
func summaryLine(summary Summary) string {
	if summary.Healthy && summary.Warn == 0 {
		return fmt.Sprintf("%d checks, all ok.", summary.Total)
	}

	counted := fmt.Sprintf("%d of %d checks failed", summary.Fail, summary.Total)
	if summary.Warn > 0 {
		counted += fmt.Sprintf(", %d worth a look", summary.Warn)
	}

	return counted + "."
}

func marker(status Status, color bool) string {
	text, ok := markers[status]
	if !ok {
		text = "??"
	}
	if !color {
		return text
	}

	return statusColors[status] + text + colorReset
}

func dim(text string, color bool) string {
	if !color {
		return text
	}

	return "\033[90m" + text + colorReset
}

// joinWords lists names the way a sentence would, so a message offering choices reads as
// one rather than as a slice printed out.
func joinWords(names []string) string {
	switch len(names) {
	case 0:
		return "none"
	case 1:
		return names[0]
	default:
		return strings.Join(names[:len(names)-1], ", ") + " or " + names[len(names)-1]
	}
}
