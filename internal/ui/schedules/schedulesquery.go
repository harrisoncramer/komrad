package schedules

import (
	"strings"
	"time"

	"komrad/internal/searchsyntax"
	"komrad/internal/ui/list"
	"komrad/internal/ui/screen"
	"komrad/lib/xstr"
)

var grammar = list.Grammar{Syntax: searchsyntax.Schedules, Check: checkClause}

// QueryGrammar is the grammar the filter reads a query to the schedules in.
func (w *Screen) QueryGrammar() (list.Grammar, bool) {
	return grammar, true
}

// QueryGrammar is the grammar the filter reads a query to the archived schedules in.
func (a Archive) QueryGrammar() (list.Grammar, bool) {
	return grammar, true
}

func checkClause(clause searchsyntax.Clause, _ time.Time) error {
	if clause.Key == searchsyntax.KeyIs && !strings.EqualFold(clause.Value, "paused") {
		return searchsyntax.ErrBadQualifier.Saying("is:%s is not paused", clause.Value)
	}

	return nil
}

func rowFits(in screen.Inputs) func(row) list.Fits {
	return func(item row) list.Fits {
		return func(clause searchsyntax.Clause) bool {
			switch clause.Key {
			case searchsyntax.KeyName:
				return xstr.ContainsFold(item.schedule.Name, clause.Value)
			case searchsyntax.KeyWorkflow:
				return xstr.ContainsFold(in.Listing.WorkflowName(item.schedule.WorkflowID), clause.Value)
			case searchsyntax.KeyIs:
				return !item.schedule.Enabled
			}

			return false
		}
	}
}
