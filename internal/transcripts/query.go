package transcripts

import (
	"strings"
	"time"

	"komrad/internal/searchsyntax"
	transcriptstore "komrad/internal/store/transcript"
)

// search is a query's words and the store filter its qualifiers narrow it to.
type search struct {
	Words  string
	Filter transcriptstore.Filter
}

// widens reports whether a qualifier names which agents to search.
func (q search) widens() bool {
	filter := q.Filter

	return filter.AgentName != nil || filter.WorkflowName != nil || len(filter.Kinds) > 0 ||
		len(filter.ExcludedKinds) > 0
}

// parseSearch reads a search's text, cutting days in the time zone of now.
func parseSearch(text string, now time.Time) (*search, error) {
	parsed, err := searchsyntax.Parse(text, searchsyntax.Transcripts)
	if err != nil {
		return nil, err
	}

	q := search{Words: parsed.Text()}
	seen := map[string]bool{}
	for _, clause := range parsed.Clauses {
		seen[clause.Key] = true
		if err := q.apply(clause, seen, now); err != nil {
			return nil, err
		}
	}

	return &q, nil
}

func (q *search) apply(clause searchsyntax.Clause, seen map[string]bool, now time.Time) error {
	switch clause.Key {
	case searchsyntax.KeyFrom:
		speaker, err := parseSpeaker(clause.Value)
		if err != nil {
			return err
		}
		q.Filter.Speaker = &speaker
	case searchsyntax.KeyAgent:
		q.Filter.AgentName = &clause.Value
	case searchsyntax.KeyWorkflow:
		q.Filter.WorkflowName = &clause.Value
	case searchsyntax.KeyIs:
		return q.applyIs(strings.ToLower(clause.Value), clause.Negated)
	case searchsyntax.KeySince:
		if seen[searchsyntax.KeyOn] {
			return searchsyntax.ErrBadQualifier.Saying("since: cannot be given with on:")
		}
		at, err := searchsyntax.ParseStart(clause.Key, clause.Value, now)
		if err != nil {
			return err
		}
		q.Filter.Since = &at
	case searchsyntax.KeyUntil:
		if seen[searchsyntax.KeyOn] {
			return searchsyntax.ErrBadQualifier.Saying("until: cannot be given with on:")
		}
		at, err := searchsyntax.ParseEnd(clause.Key, clause.Value, now)
		if err != nil {
			return err
		}
		q.Filter.Until = &at
	case searchsyntax.KeyOn:
		if seen[searchsyntax.KeySince] || seen[searchsyntax.KeyUntil] {
			return searchsyntax.ErrBadQualifier.Saying("on: cannot be given with since: or until:")
		}
		day, err := searchsyntax.ParseDay(clause.Key, clause.Value, now)
		if err != nil {
			return err
		}
		next := day.AddDate(0, 0, 1)
		q.Filter.Since, q.Filter.Until = &day, &next
	}

	return nil
}

func (q *search) applyIs(value string, negated bool) error {
	switch value {
	case "writer", "reviewer", "orchestrator":
		if negated {
			q.Filter.ExcludedKinds = append(q.Filter.ExcludedKinds, value)
		} else {
			q.Filter.Kinds = append(q.Filter.Kinds, value)
		}

		return nil
	}

	return searchsyntax.ErrBadQualifier.Saying("is:%s is not one of writer, helper, reviewer or orchestrator", value)
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
