package ui

import (
	"strings"
	"time"

	"komrad/internal/searchsyntax"
	"komrad/internal/ui/panel"
)

var schedulesQuery = queryGrammar{syntax: searchsyntax.Schedules, check: checkSchedulesClause}

func (w *schedulesScreen) queryGrammar() (queryGrammar, bool) {
	return schedulesQuery, true
}

func (a *archivedSchedules) queryGrammar() (queryGrammar, bool) {
	return schedulesQuery, true
}

func checkSchedulesClause(clause searchsyntax.Clause, _ time.Time) error {
	if clause.Key == searchsyntax.KeyIs && !strings.EqualFold(clause.Value, "paused") {
		return searchsyntax.ErrBadQualifier.Saying("is:%s is not paused", clause.Value)
	}

	return nil
}

func (m *model) scheduleItemFits(l panel.Listing) func(scheduleItem) clauseFits {
	return func(item scheduleItem) clauseFits {
		return func(clause searchsyntax.Clause) bool {
			switch clause.Key {
			case searchsyntax.KeyName:
				return containsFold(item.schedule.Name, clause.Value)
			case searchsyntax.KeyWorkflow:
				return containsFold(l.WorkflowName(item.schedule.WorkflowID), clause.Value)
			case searchsyntax.KeyIs:
				return !item.schedule.Enabled
			}

			return false
		}
	}
}
