package list

import (
	"regexp"
	"time"

	"komrad/internal/searchsyntax"
	"komrad/internal/ui/paint"
	"komrad/internal/view"
	"komrad/lib/xslice"
)

// Section is one of the sections of the panel's own pane that has a cursor of its own.
type Section int

const (
	SectionList Section = iota
	SectionRecord
	SectionTabs
	SectionKinds
	SectionViews
)

// Fits reports whether a row meets one qualifier.
type Fits func(searchsyntax.Clause) bool

// Row is one row a search can land on: the key that names it, the text it matches by and how it
// meets a qualifier.
type Row struct {
	Key  string
	Name string
	Fits Fits
}

type Pattern struct {
	Typed      string
	Query      string
	Regexp     *regexp.Regexp
	Broken     bool
	Clauses    []searchsyntax.Clause
	CompiledAt time.Time
	Filtering  bool
	// OpenedIn is the section the keys were in when the filter opened.
	OpenedIn Section
	Said     Said
}

type Said struct {
	Text    string
	Hits    map[string][]SaidHit
	Waiting bool
}

type PatternKey struct {
	narrows    bool
	query      string
	compiledAt time.Time
	said       string
	waiting    bool
}

// IsStanding reports whether anything is typed.
func (p Pattern) IsStanding() bool {
	return p.Typed != ""
}

// HasQuery reports whether a pattern or a qualifier stands to match rows by.
func (p Pattern) HasQuery() bool {
	return !p.Broken && (p.Regexp != nil || len(p.Clauses) > 0)
}

// IsNarrowing reports whether the pattern is taking rows off the screen.
func (p Pattern) IsNarrowing() bool {
	return p.Filtering && p.HasQuery()
}

// DoesNarrow reports whether the pattern is taking rows off one section, which it does only in
// the section it was typed in.
func (p Pattern) DoesNarrow(section Section) bool {
	return p.IsNarrowing() && p.OpenedIn == section
}

// Key is what a list built under the pattern is cached by.
func (p Pattern) Key() PatternKey {
	if !p.DoesNarrow(SectionList) {
		return PatternKey{}
	}

	return PatternKey{
		narrows:    true,
		query:      p.Query,
		compiledAt: p.CompiledAt,
		said:       p.Said.Text,
		waiting:    p.Said.Waiting,
	}
}

// DoesMatch reports whether a row's name matches the pattern.
func (p Pattern) DoesMatch(name string) bool {
	if p.Regexp == nil {
		return true
	}

	return p.Regexp.MatchString(name)
}

// Admits reports whether a row's name matches the pattern and the row meets the qualifiers.
func (p Pattern) Admits(name string, fits Fits) bool {
	return p.DoesMatch(name) && p.Holds(fits)
}

// Holds reports whether a row meets one of the qualifiers given under each key and none of the
// ones left out.
func (p Pattern) Holds(fits Fits) bool {
	if len(p.Clauses) == 0 {
		return true
	}
	if fits == nil {
		return false
	}

	met := map[string]bool{}
	for _, clause := range p.Clauses {
		if clause.Negated {
			if fits(clause) {
				return false
			}

			continue
		}

		met[clause.Key] = met[clause.Key] || fits(clause)
	}

	for _, ok := range met {
		if !ok {
			return false
		}
	}

	return true
}

// HasSaid reports whether the server found the row saying the text, and false while it has not
// answered.
func (p Pattern) HasSaid(key string) bool {
	return len(p.SaidHits(key)) > 0
}

// SaidHits is the turns the server found the row saying the text in, and none while it has not
// answered.
func (p Pattern) SaidHits(key string) []SaidHit {
	if p.Said.Waiting {
		return nil
	}

	return p.Said.Hits[key]
}

// MarkSaid lights the words the server found the row saying wherever they start a word in its
// rendered rows.
func (p Pattern) MarkSaid(key string, rows []string) []string {
	var terms []string
	for _, hit := range p.SaidHits(key) {
		terms = append(terms, paint.MarkedTerms(hit.Snippet)...)
	}
	if len(terms) == 0 {
		return rows
	}

	return xslice.Map(rows, func(row string) string {
		return paint.Panel.HighlightStyled(row, paint.TermSpans(paint.PlainText(row), terms))
	})
}

// Picked is the keys of the rows the pattern's qualifiers pick, nil when it names none.
func Picked[T Keyed](p Pattern, rows []T, fits func(T) Fits) map[string]bool {
	if p.Broken || len(p.Clauses) == 0 {
		return nil
	}

	picked := map[string]bool{}
	for _, r := range rows {
		if p.Holds(fits(r)) {
			picked[r.Key()] = true
		}
	}

	return picked
}

type NarrowedParams[T any] struct {
	Rows []T
	// Depth is how far a row is indented.
	Depth func(T) int
	Name  func(T) string
	Fits  func(T) Fits
}

// Narrowed is the rows a filter leaves on screen: each match, the rows above it and the rows
// under it.
func Narrowed[T any](p Pattern, params NarrowedParams[T]) []T {
	if !p.DoesNarrow(SectionList) {
		return params.Rows
	}

	rows := params.Rows
	matched := make([]bool, len(rows))
	for i, row := range rows {
		var fits Fits
		if params.Fits != nil {
			fits = params.Fits(row)
		}
		matched[i] = p.Admits(params.Name(row), fits)
	}

	keep := make([]bool, len(rows))

	// The stack holds the rows the one being read sits under, and lit counts how many of those
	// the pattern matched.
	var stack []int
	lit := 0
	for i, row := range rows {
		depth := params.Depth(row)
		for len(stack) > 0 && params.Depth(rows[stack[len(stack)-1]]) >= depth {
			if matched[stack[len(stack)-1]] {
				lit--
			}
			stack = stack[:len(stack)-1]
		}

		keep[i] = matched[i] || lit > 0
		if matched[i] {
			for _, above := range stack {
				keep[above] = true
			}
			lit++
		}

		stack = append(stack, i)
	}

	kept := make([]T, 0, len(rows))
	for i, row := range rows {
		if keep[i] {
			kept = append(kept, row)
		}
	}

	return kept
}

// Matches is the stretches of a name a search picked out, in bytes of that name.
func Matches(matcher *regexp.Regexp, name string) []view.Span {
	if matcher == nil {
		return nil
	}

	found := matcher.FindAllStringIndex(name, -1)
	if len(found) == 0 {
		return nil
	}

	return xslice.Map(found, func(at []int) view.Span {
		return view.Span{Start: at[0], End: at[1]}
	})
}

// PickedMatches is the stretches of a row's name the search lights, the whole of it when only
// qualifiers picked the row.
func PickedMatches(matcher *regexp.Regexp, picked map[string]bool, key, name string) []view.Span {
	if picked == nil {
		return Matches(matcher, name)
	}
	if !picked[key] {
		return nil
	}
	if matcher == nil {
		return []view.Span{{Start: 0, End: len(name)}}
	}

	return Matches(matcher, name)
}
