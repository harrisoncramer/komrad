package ui

import (
	"strings"
	"time"

	"komrad/internal/searchsyntax"
	"komrad/internal/ui/list"
	"komrad/internal/ui/screen"
	"komrad/lib/xstr"
)

var schedulesQuery = list.Grammar{Syntax: searchsyntax.Schedules, Check: checkSchedulesClause}

func (w *schedulesScreen) QueryGrammar() (list.Grammar, bool) {
	return schedulesQuery, true
}

func (a *archivedSchedules) QueryGrammar() (list.Grammar, bool) {
	return schedulesQuery, true
}

func checkSchedulesClause(clause searchsyntax.Clause, _ time.Time) error {
	if clause.Key == searchsyntax.KeyIs && !strings.EqualFold(clause.Value, "paused") {
		return searchsyntax.ErrBadQualifier.Saying("is:%s is not paused", clause.Value)
	}

	return nil
}

func scheduleItemFits(in screen.Inputs) func(scheduleItem) list.Fits {
	return func(item scheduleItem) list.Fits {
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
