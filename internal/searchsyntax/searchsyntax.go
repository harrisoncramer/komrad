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
	KeyHarness  = "harness"
	KeyPR       = "pr"
	KeySays     = "says"
	KeyType     = "type"
	KeyName     = "name"
	KeyTask     = "task"
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

// Grammar is the qualifiers one kind of search reads.
type Grammar struct {
	Keys   []Key
	Strict bool
}

// Transcripts is the grammar a transcript search reads.
var Transcripts = Grammar{
	Keys: []Key{
		{Name: KeyFrom},
		{Name: KeyAgent},
		{Name: KeyWorkflow},
		{
			Name:       KeyIs,
			Negatable:  true,
			Repeatable: true,
		},
		{Name: KeySince},
		{Name: KeyUntil},
		{Name: KeyOn},
	},
}

// Agents is the grammar the agents list is filtered by.
var Agents = Grammar{
	Keys: []Key{
		{Name: KeyAgent},
		{Name: KeyWorkflow},
		{Name: KeyTask},
		{
			Name:       KeyIs,
			Negatable:  true,
			Repeatable: true,
		},
		{Name: KeyHarness, Negatable: true},
		{Name: KeyPR},
		{Name: KeySince},
		{Name: KeyUntil},
		{Name: KeySays},
	},
	Strict: true,
}

// Documents is the grammar the documents list is filtered by.
var Documents = Grammar{
	Keys: []Key{
		{Name: KeyAgent},
		{Name: KeyWorkflow},
		{Name: KeyTask},
		{Name: KeyType},
		{Name: KeySince},
		{Name: KeyUntil},
		{Name: KeySays},
	},
	Strict: true,
}

// Schedules is the grammar the schedules list is filtered by.
var Schedules = Grammar{
	Keys: []Key{
		{Name: KeyName},
		{Name: KeyWorkflow},
		{Name: KeyIs, Negatable: true},
	},
	Strict: true,
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

// Parse reads text against the grammar, leaving any token whose key a lenient grammar lacks as a word.
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
			if grammar.Strict && given.Key != "" {
				return nil, ErrBadQualifier.Saying("%s: is not a qualifier here", given.Key)
			}
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
	bare    string
	negated bool
	quoted  bool
}

func readClause(tok token) (Clause, bool) {
	if tok.quoted {
		return Clause{}, false
	}

	key, value, found := strings.Cut(tok.bare, ":")
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

		tok := token{raw: raw.String(), bare: raw.String()}
		if strings.HasPrefix(tok.bare, "-") && !strings.HasPrefix(tok.bare, "--") {
			tok.negated, tok.bare = true, tok.bare[1:]
		}
		if strings.HasPrefix(tok.bare, `"`) {
			tok.quoted = true
		}
		tok.bare = strings.ReplaceAll(tok.bare, `"`, "")

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
