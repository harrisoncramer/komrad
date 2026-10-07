// Package searchsyntax reads a typed query into its words and qualifiers, for the panel and the server alike.
package searchsyntax

import (
	"strconv"
	"strings"
	"time"
	"unicode"

	"komrad/internal/fail"
)

// The keys a qualifier is typed with, before its colon.
const (
	KeyFrom     = "from"
	KeyAgent    = "agent"
	KeyWorkflow = "workflow"
	KeyIs       = "is"
	KeySince    = "since"
	KeyUntil    = "until"
	KeyOn       = "on"
)

const dayLayout = "2006-01-02"

const maxSpan = 100 * 365 * 24 * time.Hour

// ErrBadQualifier is a query holding a qualifier that does not parse.
var ErrBadQualifier = fail.InvalidArgument("the query holds a qualifier that does not parse")

// Key is one qualifier key a grammar reads and how it may be typed.
type Key struct {
	Name       string
	Negatable  bool
	Repeatable bool
}

// Qualifier is one qualifier as it is typed and what it narrows the search to.
type Qualifier struct {
	Syntax  string
	Meaning string
}

// Grammar is the qualifiers one kind of search reads and the help shown for them.
type Grammar struct {
	Keys []Key
	Help []Qualifier
}

// Transcripts is the grammar a transcript search reads.
var Transcripts = Grammar{
	Keys: []Key{
		{Name: KeyFrom},
		{Name: KeyAgent},
		{Name: KeyWorkflow},
		{Name: KeyIs, Negatable: true, Repeatable: true},
		{Name: KeySince},
		{Name: KeyUntil},
		{Name: KeyOn},
	},
	Help: []Qualifier{
		{Syntax: "from:me", Meaning: "prompts, mail included"},
		{Syntax: "from:agent", Meaning: "what the agent wrote"},
		{Syntax: "agent:NAME", Meaning: "agents named like NAME"},
		{Syntax: "workflow:NAME", Meaning: "agents in that workflow"},
		{Syntax: "is:reviewer", Meaning: "or implementer, orchestrator"},
		{Syntax: "-is:reviewer", Meaning: "leave that kind out"},
		{Syntax: "since:3d", Meaning: "or 30m, 5h, 2w, today, a date"},
		{Syntax: "until:DATE", Meaning: "up to the end of that day"},
		{Syntax: "on:DATE", Meaning: "that one day"},
	},
}

// Clause is one qualifier read from a query.
type Clause struct {
	Key     string
	Value   string
	Negated bool
}

// Query is a typed query read as its free words and the qualifiers beside them.
type Query struct {
	Words   []string
	Clauses []Clause
}

// Text is the query's free words as they were typed.
func (q Query) Text() string {
	return strings.Join(q.Words, " ")
}

// Parse reads text against the grammar, leaving any token whose key the grammar lacks as a word.
func Parse(text string, grammar Grammar) (*Query, error) {
	keys := make(map[string]Key, len(grammar.Keys))
	for _, key := range grammar.Keys {
		keys[key.Name] = key
	}

	var (
		query Query
		seen  = map[string]bool{}
	)
	for _, tok := range tokenize(text) {
		given, ok := readClause(tok)
		if !ok {
			query.Words = append(query.Words, tok.raw)

			continue
		}

		key, known := keys[given.Key]
		if !known {
			query.Words = append(query.Words, tok.raw)

			continue
		}

		if !key.Repeatable && seen[key.Name] {
			return nil, ErrBadQualifier.Saying("%s: is given twice", key.Name)
		}
		seen[key.Name] = true

		if given.Value == "" {
			return nil, ErrBadQualifier.Saying("%s: needs a value", key.Name)
		}
		if given.Negated && !key.Negatable {
			return nil, ErrBadQualifier.Saying("%s: cannot be negated", key.Name)
		}

		query.Clauses = append(query.Clauses, given)
	}

	return &query, nil
}

// ParseStart reads a since: value as the moment it starts at, cutting days in the time zone of now.
func ParseStart(key, value string, now time.Time) (time.Time, error) {
	if ago, ok := parseSpan(value); ok {
		return now.Add(-ago), nil
	}

	return ParseDay(key, value, now)
}

// ParseEnd reads an until: value as the moment it ends at, which for a day is the following midnight.
func ParseEnd(key, value string, now time.Time) (time.Time, error) {
	if ago, ok := parseSpan(value); ok {
		return now.Add(-ago), nil
	}

	day, err := ParseDay(key, value, now)
	if err != nil {
		return time.Time{}, err
	}

	return day.AddDate(0, 0, 1), nil
}

// ParseDay reads a value naming one day as that day's midnight in the time zone of now.
func ParseDay(key, value string, now time.Time) (time.Time, error) {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	switch strings.ToLower(value) {
	case "today":
		return today, nil
	case "yesterday":
		return today.AddDate(0, 0, -1), nil
	}

	day, err := time.ParseInLocation(dayLayout, value, now.Location())
	if err != nil {
		return time.Time{}, ErrBadQualifier.Saying("%s:%s is not a day; give today, yesterday, a date like 2026-09-20, or a span like 30m or 3d", key, value)
	}

	return day, nil
}

func parseSpan(value string) (time.Duration, bool) {
	if len(value) < 2 {
		return 0, false
	}

	count, err := strconv.Atoi(value[:len(value)-1])
	if err != nil || count <= 0 {
		return 0, false
	}

	var unit time.Duration
	switch unicode.ToLower(rune(value[len(value)-1])) {
	case 'm':
		unit = time.Minute
	case 'h':
		unit = time.Hour
	case 'd':
		unit = 24 * time.Hour
	case 'w':
		unit = 7 * 24 * time.Hour
	default:
		return 0, false
	}
	if count > int(maxSpan/unit) {
		return 0, false
	}

	return time.Duration(count) * unit, true
}

type token struct {
	raw     string
	text    string
	negated bool
	quoted  bool
}

func readClause(tok token) (Clause, bool) {
	if tok.quoted {
		return Clause{}, false
	}

	key, value, found := strings.Cut(tok.text, ":")
	if !found {
		return Clause{}, false
	}

	return Clause{
		Key:     strings.ToLower(key),
		Value:   value,
		Negated: tok.negated,
	}, true
}

func tokenize(text string) []token {
	var (
		tokens []token
		raw    strings.Builder
		inside bool
	)

	flush := func() {
		if raw.Len() == 0 {
			return
		}

		tok := token{raw: raw.String(), text: raw.String()}
		if strings.HasPrefix(tok.text, "-") && !strings.HasPrefix(tok.text, "--") {
			tok.negated, tok.text = true, tok.text[1:]
		}
		if strings.HasPrefix(tok.text, `"`) {
			tok.quoted = true
		}
		tok.text = strings.ReplaceAll(tok.text, `"`, "")

		tokens = append(tokens, tok)
		raw.Reset()
	}
	for _, r := range text {
		switch {
		case unicode.IsSpace(r) && !inside:
			flush()
		default:
			if r == '"' {
				inside = !inside
			}
			raw.WriteRune(r)
		}
	}
	flush()

	return tokens
}
