// Package textsearch reads a search query into the words it wants and leaves out, and shapes
// them for the full text and near matching every searchable store shares.
package textsearch

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"komrad/lib/xslice"
)

const (
	Open  = "\x02"
	Close = "\x03"
)

const (
	Lead     = 60
	Width    = 200
	Headline = "StartSel=" + Open + ",StopSel=" + Close +
		",MaxWords=35,MinWords=15,MaxFragments=1"
)

const minNearRunes = 3

type terms struct {
	Wanted   []string
	Excluded []string
}

// Split reads a query as the words it is looking for, quoted or not, and the words it
// wants left out.
func Split(query string) terms {
	var (
		split   terms
		text    strings.Builder
		quoted  bool
		minuses int
	)

	flush := func() {
		term, leading := text.String(), minuses
		text.Reset()
		quoted, minuses = false, 0

		switch {
		case term == "":
		case leading == 1:
			split.Excluded = append(split.Excluded, term)
		default:
			// Only a single minus excludes, so a flag name stays the word it looks like.
			split.Wanted = append(split.Wanted, strings.Repeat("-", leading)+term)
		}
	}
	for _, r := range query {
		switch {
		case r == '"':
			quoted = !quoted
		case unicode.IsSpace(r) && !quoted:
			flush()
		case r == '-' && !quoted && text.Len() == 0:
			minuses++
		default:
			text.WriteRune(r)
		}
	}
	flush()

	return split
}

// Quote wraps each term in quotes behind a prefix, as a websearch query reads them.
func Quote(terms []string, prefix string) string {
	quoted := xslice.Map(terms, func(term string) string {
		return prefix + `"` + term + `"`
	})

	return strings.Join(quoted, " ")
}

// HasNear reports whether every term is long enough for a near match to mean anything.
func HasNear(terms []string) bool {
	for _, term := range terms {
		if utf8.RuneCountInString(term) < minNearRunes {
			return false
		}
	}

	return true
}

// Phrase is the pattern matching the words of the terms in a row as whole words, and nil when they hold none.
func Phrase(terms []string) *string {
	var words []string
	for _, term := range terms {
		words = append(words, strings.FieldsFunc(term, func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r)
		})...)
	}
	if len(words) == 0 {
		return nil
	}

	pattern := `\m` + strings.Join(words, `[^[:alnum:]]+`) + `\M`

	return &pattern
}

// LikePatterns is the LIKE pattern of each term.
func LikePatterns(terms []string) []string {
	patterns := xslice.Map(terms, LikePattern)

	return patterns
}

// LikePattern is a LIKE pattern finding the text anywhere, with its wildcards escaped.
func LikePattern(query string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)
	return "%" + escaped + "%"
}

// Mark wraps every place a term appears in a snippet in Open and Close, ignoring case.
func Mark(snippet string, terms []string) string {
	var (
		out strings.Builder
		at  int
	)
	for at < len(snippet) {
		start, end, ok := firstMatch(snippet[at:], terms)
		if !ok {
			break
		}

		out.WriteString(snippet[at : at+start])
		out.WriteString(Open)
		out.WriteString(snippet[at+start : at+end])
		out.WriteString(Close)
		at += end
	}
	out.WriteString(snippet[at:])

	return out.String()
}

// firstMatch returns the earliest occurrence of any term, preferring the longest where two
// start at the same place.
func firstMatch(hay string, terms []string) (int, int, bool) {
	found := false

	var bestStart, bestEnd int
	for _, term := range terms {
		if term == "" {
			continue
		}

		start, end, ok := foldIndex(hay, term)
		if !ok {
			continue
		}
		if !found || start < bestStart || (start == bestStart && end > bestEnd) {
			found, bestStart, bestEnd = true, start, end
		}
	}

	return bestStart, bestEnd, found
}

func foldIndex(hay, needle string) (int, int, bool) {
	for start := 0; start < len(hay); {
		_, width := utf8.DecodeRuneInString(hay[start:])

		i, j := start, 0
		for j < len(needle) && i < len(hay) {
			h, hw := utf8.DecodeRuneInString(hay[i:])
			n, nw := utf8.DecodeRuneInString(needle[j:])
			if unicode.ToLower(h) != unicode.ToLower(n) {
				break
			}

			i, j = i+hw, j+nw
		}
		if j == len(needle) {
			return start, i, true
		}

		start += width
	}

	return 0, 0, false
}
