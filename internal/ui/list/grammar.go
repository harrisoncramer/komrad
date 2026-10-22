package list

import (
	"context"
	"time"

	"komrad/internal/searchsyntax"
)

// Grammar is the qualifiers one list's filter reads and the values it takes for each.
type Grammar struct {
	Syntax searchsyntax.Grammar
	Check  func(searchsyntax.Clause, time.Time) error
	Search func(ctx context.Context, text string) (map[string][]SaidHit, error)
}

// SaidHit is one turn the server found saying the text of a says: qualifier.
type SaidHit struct {
	Snippet string
	Offset  int64
	Length  int64
}

// Parse reads the qualifiers out of a query, refusing a value the grammar does not take.
func (g Grammar) Parse(text string, now time.Time) (*searchsyntax.Query, error) {
	parsed, err := searchsyntax.Parse(text, g.Syntax)
	if err != nil {
		return nil, err
	}

	for _, clause := range parsed.Clauses {
		if err := g.Check(clause, now); err != nil {
			return nil, err
		}
	}

	return parsed, nil
}
