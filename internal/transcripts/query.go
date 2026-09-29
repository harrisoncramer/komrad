package transcripts

import (
	"strconv"
	"strings"
	"time"
	"unicode"

	"komrad/internal/fail"
	"komrad/internal/searchsyntax"
	transcriptstore "komrad/internal/store/transcript"
)

const dayLayout = "2006-01-02"

const maxSpan = 100 * 365 * 24 * time.Hour

// ErrBadQualifier is a query holding a qualifier whose value does not parse.
var ErrBadQualifier = fail.InvalidArgument("the query holds a qualifier that does not parse")

// Query is a search's text read as the words it looks for and the qualifiers narrowing it.
type Query struct {
	Words  string
	Filter transcriptstore.Filter
	Sort   transcriptstore.Sort
}

// Widens reports whether a qualifier names which agents to search.
func (q Query) Widens() bool {
	filter := q.Filter

	return filter.AgentName != nil || filter.WorkflowName != nil || len(filter.Kinds) > 0 ||
		len(filter.ExcludedKinds) > 0 || filter.Archived != nil
}

type token struct {
	raw     string
	text    string
	negated bool
	quoted  bool
}

type clause struct {
	key     string
	value   string
	negated bool
}

type parser struct {
	query Query
	now   time.Time
	seen  map[string]bool
}

// ParseQuery reads a search's text, cutting days in the time zone of now.
func ParseQuery(text string, now time.Time) (*Query, error) {
	p := parser{now: now, seen: map[string]bool{}}

	var words []string
	for _, tok := range tokenize(text) {
		given, ok := parseQualifier(tok)
		if !ok {
			words = append(words, tok.raw)

			continue
		}

		if given.key != searchsyntax.KeyIs && p.seen[given.key] {
			return nil, ErrBadQualifier.Saying("%s: is given twice", given.key)
		}
		p.seen[given.key] = true

		if err := p.apply(given); err != nil {
			return nil, err
		}
	}

	p.query.Words = strings.Join(words, " ")

	return &p.query, nil
}

func (p *parser) apply(given clause) error {
	q, seen, now := &p.query, p.seen, p.now
	key, value, negated := given.key, given.value, given.negated
	if value == "" {
		return ErrBadQualifier.Saying("%s: needs a value", key)
	}
	if negated && key != searchsyntax.KeyIs {
		return ErrBadQualifier.Saying("%s: cannot be negated", key)
	}

	switch key {
	case searchsyntax.KeyFrom:
		speaker, err := parseSpeaker(value)
		if err != nil {
			return err
		}
		q.Filter.Speaker = &speaker
	case searchsyntax.KeyAgent:
		q.Filter.AgentName = &value
	case searchsyntax.KeyWorkflow:
		q.Filter.WorkflowName = &value
	case searchsyntax.KeyIs:
		return q.applyIs(strings.ToLower(value), negated)
	case searchsyntax.KeySort:
		sort := transcriptstore.Sort(strings.ToLower(value))
		if sort.Validate() != nil {
			return ErrBadQualifier.Saying("sort:%s is not one of best, new or old", value)
		}
		q.Sort = sort
	case searchsyntax.KeySince:
		if seen[searchsyntax.KeyOn] {
			return ErrBadQualifier.Saying("since: cannot be given with on:")
		}
		at, err := parseStart(key, value, now)
		if err != nil {
			return err
		}
		q.Filter.Since = &at
	case searchsyntax.KeyUntil:
		if seen[searchsyntax.KeyOn] {
			return ErrBadQualifier.Saying("until: cannot be given with on:")
		}
		at, err := parseEnd(key, value, now)
		if err != nil {
			return err
		}
		q.Filter.Until = &at
	case searchsyntax.KeyOn:
		if seen[searchsyntax.KeySince] || seen[searchsyntax.KeyUntil] {
			return ErrBadQualifier.Saying("on: cannot be given with since: or until:")
		}
		day, err := parseDay(key, value, now)
		if err != nil {
			return err
		}
		next := day.AddDate(0, 0, 1)
		q.Filter.Since, q.Filter.Until = &day, &next
	}

	return nil
}

func (q *Query) applyIs(value string, negated bool) error {
	switch value {
	case "implementer", "reviewer", "orchestrator":
		if negated {
			q.Filter.ExcludedKinds = append(q.Filter.ExcludedKinds, value)
		} else {
			q.Filter.Kinds = append(q.Filter.Kinds, value)
		}

		return nil
	case "live", "archived":
		archived := (value == "archived") != negated
		if q.Filter.Archived != nil && *q.Filter.Archived != archived {
			return ErrBadQualifier.Saying("is:live and is:archived cannot both be given")
		}
		q.Filter.Archived = &archived

		return nil
	}

	return ErrBadQualifier.Saying("is:%s is not one of implementer, reviewer, orchestrator, live or archived", value)
}

func parseSpeaker(value string) (string, error) {
	switch strings.ToLower(value) {
	case "me", "you", "user":
		return "user", nil
	case "agent", "assistant":
		return assistantRole, nil
	}

	return "", ErrBadQualifier.Saying("from:%s is not one of me or agent", value)
}

func parseStart(key, value string, now time.Time) (time.Time, error) {
	if ago, ok := parseSpan(value); ok {
		return now.Add(-ago), nil
	}

	return parseDay(key, value, now)
}

func parseEnd(key, value string, now time.Time) (time.Time, error) {
	if ago, ok := parseSpan(value); ok {
		return now.Add(-ago), nil
	}

	day, err := parseDay(key, value, now)
	if err != nil {
		return time.Time{}, err
	}

	return day.AddDate(0, 0, 1), nil
}

func parseDay(key, value string, now time.Time) (time.Time, error) {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	switch strings.ToLower(value) {
	case "today":
		return today, nil
	case "yesterday":
		return today.AddDate(0, 0, -1), nil
	}

	day, err := time.ParseInLocation(dayLayout, value, now.Location())
	if err != nil {
		return time.Time{}, ErrBadQualifier.Saying("%s:%s is not a day; give today, yesterday, a date like 2026-09-20, or a span like 3d", key, value)
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

func parseQualifier(tok token) (clause, bool) {
	if tok.quoted {
		return clause{}, false
	}

	key, value, found := strings.Cut(tok.text, ":")
	if !found {
		return clause{}, false
	}

	key = strings.ToLower(key)
	switch key {
	case searchsyntax.KeyFrom, searchsyntax.KeyAgent, searchsyntax.KeyWorkflow, searchsyntax.KeyIs,
		searchsyntax.KeySort, searchsyntax.KeySince, searchsyntax.KeyUntil, searchsyntax.KeyOn:
		return clause{key: key, value: value, negated: tok.negated}, true
	}

	return clause{}, false
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
