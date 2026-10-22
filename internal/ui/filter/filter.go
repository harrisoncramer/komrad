// Package filter holds the search line a screen narrows or walks its rows by.
package filter

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"komrad/internal/errui"
	"komrad/internal/glyph"
	"komrad/internal/searchsyntax"
	"komrad/internal/ui/field"
	"komrad/internal/ui/list"
	"komrad/internal/ui/screen"
	"komrad/internal/vim"

	tea "charm.land/bubbletea/v2"
)

// OpenKey opens the filter on every screen that renders one.
const OpenKey = "/"

// Mode is what a pattern does to the rows it does not match.
type Mode int

const (
	ModeSearch Mode = iota
	ModeFilter
)

// Flipped is the other mode, the one tab switches to.
func (m Mode) Flipped() Mode {
	if m == ModeFilter {
		return ModeSearch
	}

	return ModeFilter
}

// Glyph is the character the prompt opens with.
func (m Mode) Glyph(glyphs glyph.Set) string {
	if m == ModeFilter {
		return glyphs.Spell(glyph.Filter)
	}

	return "/"
}

// String names the mode in the legend.
func (m Mode) String() string {
	if m == ModeFilter {
		return "filter"
	}

	return "search"
}

// State is a screen's search line, what was typed into it and the pattern compiled from it.
type State struct {
	Typing  bool
	Input   field.Line
	Matcher *regexp.Regexp
	Broken  bool
	Mode    Mode
	// OpenedIn is the section the keys were in when the search opened.
	OpenedIn list.Section
	// Origin is the row the cursor sat on when the search opened, which abandoning it returns to.
	Origin string
	// AppliedAt is the row the cursor sat on when the search was last applied.
	AppliedAt     string
	debounce      time.Duration
	minLength     int
	compiledQuery string
	problem       string
	Qualifiers    qualifiers
}

// qualifiers is the qualifiers a search reads and what the server found for its says: one.
type qualifiers struct {
	Grammar *list.Grammar
	// clock is what the grammar reads relative dates against.
	clock      screen.Clock
	clauses    []searchsyntax.Clause
	compiledAt time.Time
	Said       said
}

// said is the rows the server found saying the text of a says: qualifier.
type said struct {
	Text    string
	Hits    map[string][]list.SaidHit
	Problem string
	Waiting bool
	Queued  bool
}

// Fail marks the search broken for the reason given.
func (f *State) Fail(reason string) {
	f.Broken, f.problem = true, reason
}

type Option func(*State)

// WithDebounce holds compiling the pattern until typing rests for wait.
func WithDebounce(wait time.Duration) Option {
	return func(f *State) {
		f.debounce = wait
	}
}

// WithGrammar reads qualifiers out of the pattern, with relative dates read against clock.
func WithGrammar(grammar list.Grammar, clock screen.Clock) Option {
	return func(f *State) {
		f.Qualifiers.Grammar, f.Qualifiers.clock = &grammar, clock
	}
}

// WithMinLength compiles no pattern shorter than length.
func WithMinLength(length int) Option {
	return func(f *State) {
		f.minLength = length
	}
}

// Query is what the user typed.
func (f State) Query() string {
	return f.Input.Value()
}

// IsStanding reports whether a pattern has been typed.
func (f State) IsStanding() bool {
	return f.Query() != ""
}

// Pattern is the compiled pattern a screen narrows its rows by.
func (f State) Pattern() list.Pattern {
	return list.Pattern{
		Typed:      f.Query(),
		Query:      f.compiledQuery,
		Regexp:     f.Matcher,
		Broken:     f.Broken,
		Clauses:    f.Qualifiers.clauses,
		CompiledAt: f.Qualifiers.compiledAt,
		Filtering:  f.Mode == ModeFilter && !f.Typing,
		OpenedIn:   f.OpenedIn,
		Said: list.Said{
			Text:    f.Qualifiers.Said.Text,
			Hits:    f.Qualifiers.Said.Hits,
			Waiting: f.Qualifiers.Said.Waiting,
		},
	}
}

// OpenDesc names the key that opens the search by the pattern standing.
func (f State) OpenDesc() string {
	return f.Mode.OpenDesc(f.Query())
}

// ClearDesc names the key that clears the search.
func (f State) ClearDesc() string {
	return f.Mode.ClearDesc()
}

// OpenDesc names the key that opens the search by the query standing.
func (m Mode) OpenDesc(query string) string {
	if query != "" {
		return m.String() + ": " + query
	}

	return "search"
}

// ClearDesc names the key that clears a search in this mode.
func (m Mode) ClearDesc() string {
	return "clear " + m.String()
}

// PatternMode is the mode a pattern was compiled under, read while the search is not being typed.
func PatternMode(p list.Pattern) Mode {
	if p.Filtering {
		return ModeFilter
	}

	return ModeSearch
}

// SetQuery replaces what the user typed and compiles it.
func (f *State) SetQuery(query string) {
	f.Input.Set(query)
	f.Compile()
}

// Open starts typing at the search with the options given.
func (f *State) Open(opts ...Option) {
	f.Typing = true
	query := f.Query()
	if query == "" {
		f.Mode = ModeSearch
	}
	f.debounce, f.minLength, f.Qualifiers.Grammar = 0, 0, nil
	for _, opt := range opts {
		opt(f)
	}
	f.SetQuery(query)
}

func (f State) isTooShort() bool {
	query := f.Query()

	return strings.TrimSpace(query) == "" || utf8.RuneCountInString(query) < f.minLength
}

// IsSearched reports whether the search has a pattern or a broken one.
func (f State) IsSearched() bool {
	return f.Pattern().HasQuery() || f.Broken
}

// IsSettled reports whether the pattern compiled is what the user typed.
func (f State) IsSettled() bool {
	return f.compiledQuery == f.Query()
}

// SettleCmd waits out the debounce and hands back what was typed.
func (f State) SettleCmd(clock screen.Clock, settled func(query string) tea.Msg) tea.Cmd {
	query := f.Query()

	return clock.After(f.debounce, func(time.Time) tea.Msg {
		return settled(query)
	})
}

// Settle compiles a query the debounce held back, and reports whether it did.
func (f *State) Settle(query string) bool {
	if query != f.Query() || f.IsSettled() {
		return false
	}

	f.Compile()

	return true
}

// Compile reads the pattern out of what the user typed.
func (f *State) Compile() {
	query := f.Query()
	f.Matcher, f.Broken, f.compiledQuery = nil, false, query
	f.problem, f.Qualifiers.clauses = "", nil
	defer f.askSaid()

	if f.isTooShort() {
		return
	}

	expr := query
	if f.Qualifiers.Grammar != nil {
		f.Qualifiers.compiledAt = f.Qualifiers.clock.Now()
		parsed, err := f.Qualifiers.Grammar.Parse(query, f.Qualifiers.compiledAt)
		if err != nil {
			f.Fail(errui.GetReason(err))

			return
		}

		f.Qualifiers.clauses, expr = parsed.Clauses, parsed.Text()
		if expr == "" {
			return
		}
	}

	if strings.ToLower(expr) == expr {
		expr = "(?i)" + expr
	}

	pattern, err := regexp.Compile(expr)
	if err != nil {
		f.Broken = true

		return
	}

	f.Matcher = pattern
}

// askSaid notes a says: qualifier whose text changed, for the model to send to the server.
func (f *State) askSaid() {
	text := ""
	for _, clause := range f.Qualifiers.clauses {
		if clause.Key == searchsyntax.KeySays {
			text = clause.Value
		}
	}
	if text == f.Qualifiers.Said.Text {
		if f.Qualifiers.Said.Problem != "" {
			f.Fail(f.Qualifiers.Said.Problem)
		}

		return
	}

	f.Qualifiers.Said = said{
		Text:    text,
		Waiting: text != "",
		Queued:  text != "",
	}
}

// TakeKey applies one keypress to a search being typed at, with vim reading it first when
// register is set, and reports whether it was one of the search's.
func (f *State) TakeKey(key tea.KeyPressMsg, register *vim.Register) bool {
	switch f.Input.Take(key, register) {
	case field.LineLeft:
		f.Abandon()
	case field.LineEntered:
		f.Typing = false
		f.Compile()
	case field.LineChanged:
		if f.debounce == 0 || f.isTooShort() {
			f.Compile()
		}
	case field.LineIgnored:
		return false
	case field.LineKept:
	}

	return true
}

// Abandon drops the search and the row it was going to leave you on.
func (f *State) Abandon() {
	f.Typing = false
	f.Mode = ModeSearch
	f.SetQuery("")
}
