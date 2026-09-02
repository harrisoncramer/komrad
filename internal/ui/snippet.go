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
		term := strings.ToLower(strings.TrimSpace(rest[:j]))
		rest = rest[j+len(transcriptstore.SnippetClose):]

		if term == "" || seen[term] {
			continue
		}
		seen[term] = true
		terms = append(terms, term)
	}

	return terms
}

// matchLines reports the lines the marked words are spelled on within the one turn a hit came
// from, which is as much as a hit says about where it matched: the search index gives a turn
// and the words, never a position for the words themselves.
func matchLines(content string, offset, length int64, terms []string) []int {
	if len(terms) == 0 || offset < 0 || offset > int64(len(content)) || length <= 0 {
		return nil
	}

	end := min(offset+length, int64(len(content)))
	first := lineAtOffset(content, offset)

	var lines []int
	for i, line := range strings.Split(strings.ToLower(content[offset:end]), "\n") {
		if slices.ContainsFunc(terms, func(term string) bool { return strings.Contains(line, term) }) {
			lines = append(lines, first+i)
		}
	}

	return lines
}
