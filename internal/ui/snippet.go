package ui

import (
	"slices"
	"strings"

	transcriptstore "weave/internal/store/transcript"
)

func lineAtOffset(content string, offset int64) int {
	if offset < 0 || offset > int64(len(content)) {
		return 0
	}

	return 1 + strings.Count(content[:offset], "\n")
}

const snippetIndent = 4

const snippetClipLead = 12

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

func trimLastRune(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}

	return string(r[:len(r)-1])
}

// matchSpan is a byte range in a transcript that a search hit's words sit on.
type matchSpan struct {
	start int
	end   int
}

// markedTerms pulls the words the search marked in a snippet, which are the forms the turn
// actually spells rather than the words the query asked for, since a stemmed match spells the
// word its own way.
func markedTerms(snippet string) []string {
	var terms []string
	seen := make(map[string]bool)

	rest := snippet
	for {
		i := strings.Index(rest, transcriptstore.SnippetOpen)
		if i < 0 {
			break
		}
		rest = rest[i+len(transcriptstore.SnippetOpen):]

		j := strings.Index(rest, transcriptstore.SnippetClose)
		if j < 0 {
			break
		}
		term := strings.TrimSpace(rest[:j])
		rest = rest[j+len(transcriptstore.SnippetClose):]

		key := asciiLower(term)
		if term == "" || seen[key] {
			continue
		}
		seen[key] = true
		terms = append(terms, term)
	}

	return terms
}

// matchSpans locates every place the marked words are spelled within the one turn a hit came
// from, which is the whole of what a hit says about where it matched: the search index gives a
// turn and the words, never the offsets of the words themselves.
func matchSpans(content string, offset, length int64, terms []string) []matchSpan {
	if len(terms) == 0 || offset < 0 || offset > int64(len(content)) || length <= 0 {
		return nil
	}

	end := min(offset+length, int64(len(content)))
	turn := asciiLower(content[offset:end])

	var spans []matchSpan
	for _, term := range terms {
		lowered := asciiLower(term)

		for at := 0; at < len(turn); {
			i := strings.Index(turn[at:], lowered)
			if i < 0 {
				break
			}

			start := int(offset) + at + i
			spans = append(spans, matchSpan{start: start, end: start + len(lowered)})
			at += i + len(lowered)
		}
	}

	if len(spans) == 0 {
		return nil
	}

	slices.SortFunc(spans, func(a, b matchSpan) int { return a.start - b.start })

	// Terms are looked for one at a time, so one word that contains another is found twice and
	// the pair would style the same bytes twice over.
	merged := make([]matchSpan, 0, len(spans))
	for _, span := range spans {
		if last := len(merged) - 1; last >= 0 && span.start < merged[last].end {
			merged[last].end = max(merged[last].end, span.end)

			continue
		}

		merged = append(merged, span)
	}

	return merged
}

// asciiLower folds a string for comparison without moving any byte, since the offsets taken off
// the folded copy are used to slice the original.
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}

	return string(b)
}
