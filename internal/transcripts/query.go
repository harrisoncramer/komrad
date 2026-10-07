package transcripts

import (
	"strings"
	"time"

	"komrad/internal/searchsyntax"
	transcriptstore "komrad/internal/store/transcript"
)

// query is a search's text read as the words it looks for and the qualifiers narrowing it.
type query struct {
	Words  string
	Filter transcriptstore.Filter
}

// widens reports whether a qualifier names which agents to search.
func (q query) widens() bool {
	filter := q.Filter

	return filter.AgentName != nil || filter.WorkflowName != nil || len(filter.Kinds) > 0 ||
		len(filter.ExcludedKinds) > 0
}

// parseQuery reads a search's text, cutting days in the time zone of now.
func parseQuery(text string, now time.Time) (*query, error) {
	parsed, err := searchsyntax.Parse(text, searchsyntax.Transcripts)
	if err != nil {
		return nil, err
	}

	q := query{Words: parsed.Text()}
	seen := map[string]bool{}
	for _, given := range parsed.Clauses {
		seen[given.Key] = true
		if err := q.apply(given, seen, now); err != nil {
			return nil, err
		}
	}

	return &q, nil
}

func (q *query) apply(given searchsyntax.Clause, seen map[string]bool, now time.Time) error {
	key, value := given.Key, given.Value

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
		return q.applyIs(strings.ToLower(value), given.Negated)
	case searchsyntax.KeySince:
		if seen[searchsyntax.KeyOn] {
			return searchsyntax.ErrBadQualifier.Saying("since: cannot be given with on:")
		}
		at, err := searchsyntax.ParseStart(key, value, now)
		if err != nil {
			return err
		}
		q.Filter.Since = &at
	case searchsyntax.KeyUntil:
		if seen[searchsyntax.KeyOn] {
			return searchsyntax.ErrBadQualifier.Saying("until: cannot be given with on:")
		}
		at, err := searchsyntax.ParseEnd(key, value, now)
		if err != nil {
			return err
		}
		q.Filter.Until = &at
	case searchsyntax.KeyOn:
		if seen[searchsyntax.KeySince] || seen[searchsyntax.KeyUntil] {
			return searchsyntax.ErrBadQualifier.Saying("on: cannot be given with since: or until:")
		}
		day, err := searchsyntax.ParseDay(key, value, now)
		if err != nil {
			return err
		}
		next := day.AddDate(0, 0, 1)
		q.Filter.Since, q.Filter.Until = &day, &next
	}

	return nil
}

func (q *query) applyIs(value string, negated bool) error {
	switch value {
	case "implementer", "reviewer", "orchestrator":
		if negated {
			q.Filter.ExcludedKinds = append(q.Filter.ExcludedKinds, value)
		} else {
			q.Filter.Kinds = append(q.Filter.Kinds, value)
		}

		return nil
	}

	return searchsyntax.ErrBadQualifier.Saying("is:%s is not one of implementer, reviewer or orchestrator", value)
}

func parseSpeaker(value string) (string, error) {
	switch strings.ToLower(value) {
	case "me", "you", "user":
		return "user", nil
	case "agent", "assistant":
		return assistantRole, nil
	}

	return "", searchsyntax.ErrBadQualifier.Saying("from:%s is not one of me or agent", value)
}
