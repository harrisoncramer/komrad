package ui

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	transcriptstore "weave/internal/store/transcript"
)

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
	return styleMarks(clipToMatch(strings.ReplaceAll(s, "\n", " "), width))
}

// styleMarks consumes the store's match markers into styling, which is the same pass for a
// snippet in the listing and for a whole transcript on the stage.
func styleMarks(s string) string {
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
		term := foldASCII(strings.TrimSpace(rest[:j]))
		rest = rest[j+len(transcriptstore.SnippetClose):]

		if term == "" || seen[term] {
			continue
		}
		seen[term] = true
		terms = append(terms, term)
	}

	return terms
}

// span is a stretch of a transcript a hit matched on, in bytes.
type span struct {
	start int
	end   int
}

// matchSpans locates the marked words within the one turn a hit came from, which is as much as
// a hit says about where it matched: the search index gives a turn and the words, never a
// position for the words themselves.
func matchSpans(content string, offset, length int64, terms []string) []span {
	if len(terms) == 0 || offset < 0 || offset > int64(len(content)) || length <= 0 {
		return nil
	}

	end := min(offset+length, int64(len(content)))
	turn := foldASCII(content[offset:end])

	var spans []span
	for _, term := range terms {
		folded := foldASCII(term)
		if folded == "" {
			continue
		}

		for at := 0; at < len(turn); {
			i := strings.Index(turn[at:], folded)
			if i < 0 {
				break
			}

			// The listing's own snippet marks whole words, so a term found inside a longer word
			// is not what the search matched on, while a match running past the word's end is
			// kept for the stemmed hit that spells it.
			if startsWord(turn, at+i) {
				start := int(offset) + at + i
				spans = append(spans, span{start: start, end: start + len(folded)})
			}

			at += i + len(folded)
		}
	}

	slices.SortFunc(spans, func(a, b span) int { return a.start - b.start })

	return mergeSpans(spans)
}

// startsWord reports whether an index falls at the front of a word, reading the rune before it
// rather than the byte, since the punctuation these transcripts are written with runs well past
// ASCII and a curly quote is no more a word than a straight one is.
func startsWord(s string, at int) bool {
	if at == 0 {
		return true
	}

	r, _ := utf8.DecodeLastRuneInString(s[:at])

	return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
}

// mergeSpans folds overlapping spans together, since two of the matched words can be spelled
// over the same letters and a mark written inside another mark would not close.
func mergeSpans(spans []span) []span {
	if len(spans) == 0 {
		return nil
	}

	merged := []span{spans[0]}
	for _, s := range spans[1:] {
		last := len(merged) - 1
		if s.start <= merged[last].end {
			merged[last].end = max(merged[last].end, s.end)

			continue
		}

		merged = append(merged, s)
	}

	return merged
}

// foldASCII lowercases the ASCII letters and leaves every other byte where it is, since the
// spans it is used to find are byte offsets into the transcript and a Unicode fold can change
// how long a string is.
func foldASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}

	return string(b)
}
