package pullrequests

import (
	"slices"

	"komrad/internal/notices"
	pullrequeststore "komrad/internal/store/pullrequest"
)

var noticeOf = map[Field]map[string]string{
	FieldState: {
		"MERGED": notices.PullRequestMerged,
		"CLOSED": notices.PullRequestClosed,
	},
	FieldDecision: {
		"approved":          notices.PullRequestApproved,
		"changes_requested": notices.PullRequestChangesRequested,
	},
	FieldChecks: {
		"failing": notices.PullRequestChecksFailing,
	},
	FieldMergeability: {
		"conflicting": notices.PullRequestConflicting,
		"behind":      notices.PullRequestBehind,
	},
}

// noticesOf is the notices a change starts and the notices the pull request holds now, an
// ended pull request holding only its ending and starting it only on the move that ended it.
func noticesOf(change Change, pullRequest pullrequeststore.PullRequest) ([]string, []string) {
	if ended, ok := noticeOf[FieldState][pullRequest.State]; ok {
		endedNow := slices.ContainsFunc(change.Moves, func(move Move) bool {
			return move.Field == FieldState && move.To == pullRequest.State
		})
		if !endedNow {
			return nil, []string{ended}
		}

		return []string{ended}, []string{ended}
	}

	var started []string
	for _, move := range change.Moves {
		if notice, ok := noticeOf[move.Field][move.To]; ok {
			started = append(started, notice)
		}
	}

	now := map[Field]string{
		FieldDecision:     pullRequest.LastKnownDecision,
		FieldChecks:       pullRequest.LastKnownChecks,
		FieldMergeability: pullRequest.LastKnownMergeability,
	}

	holding := []string{}
	for _, field := range fieldOrder {
		if notice, ok := noticeOf[field][now[field]]; ok {
			holding = append(holding, notice)
		}
	}

	return started, holding
}
