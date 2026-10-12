package pullrequests

import (
	"slices"

	"komrad/internal/eventprompts"
	"komrad/internal/forge"
	pullrequeststore "komrad/internal/store/pullrequest"
)

var eventOf = map[field]map[string]string{
	fieldState: {
		"MERGED": eventprompts.PullRequestMerged,
		"CLOSED": eventprompts.PullRequestClosed,
	},
	fieldDecision: {
		"approved":          eventprompts.PullRequestApproved,
		"changes_requested": eventprompts.PullRequestChangesRequested,
	},
	fieldChecks: {
		"failing":         eventprompts.PullRequestChecksFailing,
		"failing_running": eventprompts.PullRequestChecksFailingRunning,
	},
	fieldMergeability: {
		"conflicting": eventprompts.PullRequestConflicting,
		"behind":      eventprompts.PullRequestBehind,
	},
	fieldAutoMerge: {
		forge.AutoMergeOn: eventprompts.PullRequestAutoMergeOn,
	},
}

// eventsOf is the events a change starts and the events the pull request holds now, an
// ended pull request holding only its ending and starting it only on the move that ended it, and a move of the
// checks to failing starting nothing.
func eventsOf(change Change, pullRequest pullrequeststore.PullRequest) ([]string, []string) {
	if ended, ok := eventOf[fieldState][pullRequest.State]; ok {
		endedNow := slices.ContainsFunc(change.Moves, func(move Move) bool {
			return move.Field == fieldState && move.To == pullRequest.State
		})
		if !endedNow {
			return nil, []string{ended}
		}

		return []string{ended}, []string{ended}
	}

	now := fieldValues(pullRequest)

	var started []string
	for _, move := range change.Moves {
		if event, ok := eventOf[move.Field][move.To]; ok && move.Field != fieldChecks {
			started = append(started, event)
		}
	}
	if passing(now) && slices.ContainsFunc(change.Moves, turnsPassing) {
		started = append(started, eventprompts.PullRequestChecksPassing)
	}

	var holding []string
	for _, field := range fieldOrder {
		if event, ok := eventOf[field][now[field]]; ok {
			holding = append(holding, event)
		}
	}
	if passing(now) {
		holding = append(holding, eventprompts.PullRequestChecksPassing)
	}

	return started, holding
}

// failingEvent is the event the checks failing on the pull request start, empty when they are not failing or
// the pull request has ended.
func failingEvent(pullRequest pullrequeststore.PullRequest) string {
	if _, ended := eventOf[fieldState][pullRequest.State]; ended {
		return ""
	}

	return eventOf[fieldChecks][fieldValues(pullRequest)[fieldChecks]]
}

// passing says whether the checks pass on a pull request the forge would merge.
func passing(values map[field]string) bool {
	return values[fieldChecks] == string(forge.ChecksPassing) && values[fieldMergeability] == string(forge.MergeabilityClean)
}

// turnsPassing says whether a move is one of the two that can leave a pull request passing.
func turnsPassing(move Move) bool {
	return move.Field == fieldChecks && move.To == string(forge.ChecksPassing) ||
		move.Field == fieldMergeability && move.To == string(forge.MergeabilityClean)
}

// finishesFailing says whether a move is the run finishing on checks that were failing while others still ran.
func finishesFailing(move Move) bool {
	return move.Field == fieldChecks && move.From == string(forge.ChecksFailingRunning) && move.To == string(forge.ChecksFailing)
}
