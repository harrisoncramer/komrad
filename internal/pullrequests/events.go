package pullrequests

import (
	"slices"

	"komrad/internal/forge"
	pullrequeststore "komrad/internal/store/pullrequest"
	eventv1 "komrad/proto/gen/event/v1"
)

var eventOf = map[field]map[string]eventv1.Event{
	fieldState: {
		"MERGED": eventv1.Event_EVENT_PULL_REQUEST_MERGED,
		"CLOSED": eventv1.Event_EVENT_PULL_REQUEST_CLOSED,
	},
	fieldDecision: {
		"approved":          eventv1.Event_EVENT_PULL_REQUEST_APPROVED,
		"changes_requested": eventv1.Event_EVENT_PULL_REQUEST_CHANGES_REQUESTED,
	},
	fieldChecks: {
		"failing":         eventv1.Event_EVENT_PULL_REQUEST_CHECKS_FAILING,
		"failing_running": eventv1.Event_EVENT_PULL_REQUEST_CHECKS_FAILING_RUNNING,
	},
	fieldMergeability: {
		"conflicting": eventv1.Event_EVENT_PULL_REQUEST_CONFLICTING,
		"behind":      eventv1.Event_EVENT_PULL_REQUEST_BEHIND,
	},
	fieldAutoMerge: {
		forge.AutoMergeOn: eventv1.Event_EVENT_PULL_REQUEST_AUTO_MERGE_ON,
	},
}

// eventsOf is the events a change starts and the events the pull request holds now, an
// ended pull request holding only its ending and starting it only on the move that ended it, and a move of the
// checks to failing starting nothing.
func eventsOf(change Change, pullRequest pullrequeststore.PullRequest) ([]eventv1.Event, []eventv1.Event) {
	if ended, ok := eventOf[fieldState][pullRequest.State]; ok {
		endedNow := slices.ContainsFunc(change.Moves, func(move Move) bool {
			return move.Field == fieldState && move.To == pullRequest.State
		})
		if !endedNow {
			return nil, []eventv1.Event{ended}
		}

		return []eventv1.Event{ended}, []eventv1.Event{ended}
	}

	now := fieldValues(pullRequest)

	var started []eventv1.Event
	for _, move := range change.Moves {
		if event, ok := eventOf[move.Field][move.To]; ok && move.Field != fieldChecks {
			started = append(started, event)
		}
	}
	if passing(now) && slices.ContainsFunc(change.Moves, turnsPassing) {
		started = append(started, eventv1.Event_EVENT_PULL_REQUEST_CHECKS_PASSING)
	}

	var holding []eventv1.Event
	for _, field := range fieldOrder {
		if event, ok := eventOf[field][now[field]]; ok {
			holding = append(holding, event)
		}
	}
	if passing(now) {
		holding = append(holding, eventv1.Event_EVENT_PULL_REQUEST_CHECKS_PASSING)
	}

	return started, holding
}

// failingEvent is the event the checks failing on the pull request start, unset when they are not failing or
// the pull request has ended.
func failingEvent(pullRequest pullrequeststore.PullRequest) eventv1.Event {
	if _, ended := eventOf[fieldState][pullRequest.State]; ended {
		return eventv1.Event_EVENT_UNSPECIFIED
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
