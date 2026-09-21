package harness

import (
	"regexp"
	"strings"
)

// escSeq is a control sequence tmux writes into a capture, of which only the one setting colour
// and attributes matters here.
var escSeq = regexp.MustCompile(`^\x1b\[[0-9;?]*[ -/]*[@-~]`)

var sgrSeq = regexp.MustCompile(`^\x1b\[([0-9;]*)m`)

// pen is the colour and attributes a cell is drawn with, carried from one line of a capture into
// the next because tmux writes an attribute at the cell it changes on.
type pen struct {
	Dim        bool
	Foreground string
}

// apply reads one SGR parameter list onto the pen, walking the parameters in order because a
// colour takes the ones after it as its own.
func (p *pen) apply(params string) {
	fields := strings.Split(params, ";")

	for i := 0; i < len(fields); i++ {
		field := fields[i]

		switch {
		case field == "" || field == "0":
			*p = pen{}
		case field == "2":
			p.Dim = true
		case field == "22":
			p.Dim = false
		case field == "39":
			p.Foreground = ""
		case field == "38" || field == "48":
			run := colourRun(fields[i:])
			if field == "38" {
				p.Foreground = strings.Join(run, ";")
			}

			i += len(run) - 1
		case isBasicColour(field):
			p.Foreground = field
		}
	}
}

// colourRun is the parameters an extended colour spans: the selector, and however many
// components it names.
func colourRun(fields []string) []string {
	span := 1
	if len(fields) > 1 {
		switch fields[1] {
		case "5":
			span = 3
		case "2":
			span = 5
		}
	}

	if span > len(fields) {
		span = len(fields)
	}

	return fields[:span]
}

// isBasicColour reports whether a parameter names a foreground out of the sixteen a terminal
// carries without an extended selector.
func isBasicColour(field string) bool {
	switch {
	case len(field) != 2:
		return false
	case field[0] == '3', field[0] == '9':
		return field[1] >= '0' && field[1] <= '7'
	}

	return false
}

// segment is a run of one captured line, either the characters drawn between two escapes or an
// escape itself, alongside the pen in force on each side of it.
type segment struct {
	Text   string
	Escape bool
	Before pen
	After  pen
}

// segments splits a captured line into the runs it draws and the escapes between them, leaving
// the pen where the line ends.
func (p *pen) segments(line string) []segment {
	var run []segment

	for line != "" {
		if line[0] == '\x1b' {
			seq := escSeq.FindString(line)
			if seq == "" {
				seq = line[:1]
			}

			before := *p
			if params := sgrSeq.FindStringSubmatch(seq); params != nil {
				p.apply(params[1])
			}

			run = append(run, segment{
				Text:   seq,
				Escape: true,
				Before: before,
				After:  *p,
			})
			line = line[len(seq):]

			continue
		}

		cut := strings.IndexByte(line, '\x1b')
		if cut < 0 {
			cut = len(line)
		}

		run = append(run, segment{
			Text:   line[:cut],
			Before: *p,
			After:  *p,
		})
		line = line[cut:]
	}

	return run
}

// StripDim removes from a captured region the runs a CLI draws dim, text the frame holds but
// nobody typed.
func StripDim(lines []string) []string {
	// The region keeps its line count, which a caller reads it alongside the whole region by.
	stripped := make([]string, len(lines))

	var drawn pen

	for i, line := range lines {
		var kept strings.Builder

		for _, seg := range drawn.segments(line) {
			switch {
			case seg.Escape:
				// An escape belongs to the run it opens or closes, so only one between two
				// bright runs is text nobody drew dim.
				if !seg.Before.Dim && !seg.After.Dim {
					kept.WriteString(seg.Text)
				}
			case !seg.Before.Dim:
				kept.WriteString(seg.Text)
			}
		}

		stripped[i] = kept.String()
	}

	return stripped
}

// Foregrounds is the colour each line of a region draws its first non-blank run in, and empty
// where that run carries the terminal's own.
func Foregrounds(lines []string) []string {
	colours := make([]string, len(lines))

	var drawn pen

	for i, line := range lines {
		found := false

		for _, seg := range drawn.segments(line) {
			if found || seg.Escape || strings.TrimSpace(seg.Text) == "" {
				continue
			}

			colours[i] = seg.Before.Foreground
			found = true
		}
	}

	return colours
}
