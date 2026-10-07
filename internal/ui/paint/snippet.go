package paint

import (
	"strings"

	"charm.land/lipgloss/v2"
)

const (
	// MarkOpen and MarkClose wrap the stretches a search hit matched on.
	MarkOpen  = "\x02"
	MarkClose = "\x03"
)

// styleMarks consumes the match markers into styling, putting back whatever styling was
// in force where a mark falls inside it.
func styleMarks(s string, style lipgloss.Style) string {
	var b strings.Builder

	active := ""
	for {
		i := strings.Index(s, MarkOpen)
		if i < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:i])
		active = activeSGR(active, s[:i])

		rest := s[i+len(MarkOpen):]
		j := strings.Index(rest, MarkClose)
		if j < 0 {
			b.WriteString(rest)
			break
		}
		b.WriteString(style.Render(rest[:j]))
		b.WriteString(active)
		s = rest[j+len(MarkClose):]
	}

	return b.String()
}

// activeSGR reports the styling still in force at the end of a chunk, cleared by a reset or a
// line ending.
func activeSGR(active, chunk string) string {
	for chunk != "" {
		i := strings.IndexAny(chunk, "\x1b\n")
		if i < 0 {
			return active
		}

		if chunk[i] == '\n' {
			active, chunk = "", chunk[i+1:]

			continue
		}

		rest := chunk[i:]
		end := strings.IndexByte(rest, 'm')
		if !strings.HasPrefix(rest, "\x1b[") || end < 0 {
			chunk = rest[1:]

			continue
		}

		if params := rest[2:end]; params == "" || params == "0" {
			active = ""
		} else {
			active = rest[:end+1]
		}

		chunk = rest[end+1:]
	}

	return active
}

// Span is a stretch of text a search matched on, in bytes.
type Span struct {
	Start int
	End   int
}
