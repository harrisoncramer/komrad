package pullrequests

import (
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
// ended pull request holding only its ending.
func noticesOf(change Change, pullRequest pullrequeststore.PullRequest) ([]string, []string) {
	if ended, ok := noticeOf[FieldState][pullRequest.State]; ok {
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
