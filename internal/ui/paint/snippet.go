package paint

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

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

// MarkedTerms pulls the words the search marked in a snippet: the forms the turn actually
// spells, not the words the query asked for.
func MarkedTerms(snippet string) []string {
	var terms []string
	seen := make(map[string]bool)

	rest := snippet
	for {
		i := strings.Index(rest, MarkOpen)
		if i < 0 {
			break
		}
		rest = rest[i+len(MarkOpen):]

		j := strings.Index(rest, MarkClose)
		if j < 0 {
			break
		}
		term := foldASCII(strings.TrimSpace(rest[:j]))
		rest = rest[j+len(MarkClose):]

		if term == "" || seen[term] {
			continue
		}
		seen[term] = true
		terms = append(terms, term)
	}

	return terms
}

// Span is a stretch of text a search matched on, in bytes.
type Span struct {
	Start int
	End   int
}

// MatchSpansParams is a transcript, the turn of it a hit came from in bytes, and the words the
// hit marked.
type MatchSpansParams struct {
	Content string
	Offset  int64
	Length  int64
	Terms   []string
}

// MatchSpans locates the marked words within the one turn a hit came from.
func MatchSpans(params MatchSpansParams) []Span {
	content, offset := params.Content, params.Offset
	if len(params.Terms) == 0 || offset < 0 || offset > int64(len(content)) || params.Length <= 0 {
		return nil
	}

	end := min(offset+params.Length, int64(len(content)))
	turn := foldASCII(content[offset:end])

	var spans []Span
	for _, term := range params.Terms {
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
			if doesStartWord(turn, at+i) {
				start := int(offset) + at + i
				spans = append(spans, Span{Start: start, End: start + len(folded)})
			}

			at += i + len(folded)
		}
	}

	slices.SortFunc(spans, func(a, b Span) int { return a.Start - b.Start })

	return mergeSpans(spans)
}

// doesStartWord reports whether an index falls at the front of a word, reading the rune before
// it rather than the byte.
func doesStartWord(s string, at int) bool {
	if at == 0 {
		return true
	}

	r, _ := utf8.DecodeLastRuneInString(s[:at])

	return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
}

// mergeSpans folds overlapping spans together.
func mergeSpans(spans []Span) []Span {
	if len(spans) == 0 {
		return nil
	}

	merged := []Span{spans[0]}
	for _, span := range spans[1:] {
		last := len(merged) - 1
		if span.Start <= merged[last].End {
			merged[last].End = max(merged[last].End, span.End)

			continue
		}

		merged = append(merged, span)
	}

	return merged
}

// foldASCII lowercases the ASCII letters and leaves every other byte where it is, keeping the
// byte offsets a Unicode fold would move.
func foldASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}

	return string(b)
}
