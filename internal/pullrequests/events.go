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
		"failing": eventprompts.PullRequestChecksFailing,
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
// ended pull request holding only its ending and starting it only on the move that ended it.
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

	var started []string
	for _, move := range change.Moves {
		if event, ok := eventOf[move.Field][move.To]; ok {
			started = append(started, event)
		}
	}

	now := fieldValues(pullRequest)

	var holding []string
	for _, field := range fieldOrder {
		if event, ok := eventOf[field][now[field]]; ok {
			holding = append(holding, event)
		}
	}

	return started, holding
}
