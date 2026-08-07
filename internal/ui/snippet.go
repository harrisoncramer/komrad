package ui

import (
	"strings"

	transcriptstore "weave/internal/store/transcript"
)

// A search hit arrives with the store's markers around whatever matched. Turning that
// into one line that fits the pane, with the match still inside it, is all this does.

// lineAtOffset returns the 1-based line the given byte offset falls on, so the
// preview can centre on the turn that matched. Matching is stemmed, so the words the
// user typed need not appear in the text they found. The offset was recorded when
// the turn was indexed and locates it either way. An offset past the end of the
// content means the two have drifted, which anchors at the top rather than guessing.
func lineAtOffset(content string, offset int64) int {
	if offset < 0 || offset > int64(len(content)) {
		return 0
	}

	return 1 + strings.Count(content[:offset], "\n")
}

// snippetIndent is how much of a result row the cursor column and indent take, the
// part of the pane a snippet cannot have.
const snippetIndent = 4

// snippetClipLead is how many characters of a clipped snippet are kept ahead of its
// match, enough that the match reads in context instead of starting the line.
const snippetClipLead = 12

// clipToMatch cuts a snippet down to width visible characters, keeping the first
// marked match within what survives. A snippet that already fits, or a width from a
// pane nothing has measured yet, is returned whole. The markers are not visible and
// so do not count against the width, and an ellipsis stands in at either end that
// was cut.
//
// Without this a long snippet is simply too wide for the pane, and since the match
// can sit anywhere inside it, the part that gets cut off is often the match itself.
func clipToMatch(snippet string, width int) string {
	if width <= 0 {
		return snippet
	}

	const ellipsis = "…"

	openMark, closeMark := rune(transcriptstore.SnippetOpen[0]), rune(transcriptstore.SnippetClose[0])
	runes := []rune(snippet)

	visible, matchAt := 0, -1
	for _, r := range runes {
		switch r {
		case openMark:
			if matchAt < 0 {
				matchAt = visible
			}
		case closeMark:
		default:
			visible++
		}
	}
	if visible <= width {
		return snippet
	}

	// Both ellipses come out of the width, since a row that overruns the pane is the
	// thing being fixed.
	budget := max(width-2*len(ellipsis), 1)

	start := 0
	if matchAt > snippetClipLead {
		start = matchAt - snippetClipLead
	}
	if start+budget > visible {
		start = visible - budget
	}

	var (
		b       strings.Builder
		at      int
		marked  bool
		wrapped bool
	)
	if start > 0 {
		b.WriteString(ellipsis)
	}

	for _, r := range runes {
		switch r {
		case openMark:
			marked = true
			if at >= start && at < start+budget {
				b.WriteRune(r)
				wrapped = true
			}

			continue
		case closeMark:
			marked = false
			if wrapped {
				b.WriteRune(r)
				wrapped = false
			}

			continue
		}

		if at >= start && at < start+budget {
			// The window opened partway through a marked run, so the marker that began
			// it fell outside it and is reissued here.
			if marked && !wrapped {
				b.WriteString(transcriptstore.SnippetOpen)
				wrapped = true
			}
			b.WriteRune(r)
		}
		at++
	}

	if wrapped {
		b.WriteString(transcriptstore.SnippetClose)
	}
	if start+budget < visible {
		b.WriteString(ellipsis)
	}

	return b.String()
}

// renderSnippet styles a hit's snippet for the list, turning the store's match
// markers into a highlighted run, collapsing newlines so the snippet stays on one
// line, and clipping it to width so it cannot overrun the pane.
func renderSnippet(s string, width int) string {
	s = clipToMatch(strings.ReplaceAll(s, "\n", " "), width)

	var b strings.Builder
	for {
		i := strings.Index(s, transcriptstore.SnippetOpen)
		if i < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:i])

		rest := s[i+len(transcriptstore.SnippetOpen):]
		j := strings.Index(rest, transcriptstore.SnippetClose)
		if j < 0 {
			b.WriteString(rest)
			break
		}
		b.WriteString(matchStyle.Render(rest[:j]))
		s = rest[j+len(transcriptstore.SnippetClose):]
	}

	return b.String()
}

// trimLastRune drops the final rune of s, so backspace deletes a whole
// multi-byte character rather than one byte of it.
func trimLastRune(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}

	return string(r[:len(r)-1])
}
