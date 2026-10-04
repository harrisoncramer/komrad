package render

import (
	"komrad/internal/forge"
	pullrequeststore "komrad/internal/store/pullrequest"
	"komrad/lib/dates"
	"komrad/lib/xslice"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
)

// CheckReading draws the checks read on a pull request's head as the wire carries them.
func CheckReading(reading *pullrequeststore.CheckReading) *pullrequestv1.ListChecksResponse {
	return &pullrequestv1.ListChecksResponse{
		HeadSha: reading.HeadSHA,
		ReadAt:  dates.StampPtr(&reading.ReadAt),
		Checks:  xslice.Map(reading.Checks, check),
	}
}

func check(stored forge.Check) *pullrequestv1.Check {
	return &pullrequestv1.Check{
		ProviderId: stored.ProviderID,
		Kind:       checkKindOf(stored.Kind),
		Source:     stored.Source,
		Name:       stored.Name,
		State:      checkStateOf(stored.State),
		Conclusion: stored.Conclusion,
		StartedAt:  dates.StampPtr(stored.StartedAt),
		FinishedAt: dates.StampPtr(stored.FinishedAt),
		Required:   stored.Required,
		Url:        stored.URL,
	}
}

// checkKindOf is a check's kind as the wire carries it.
func checkKindOf(kind forge.CheckKind) pullrequestv1.CheckKind {
	switch kind {
	case forge.CheckKindRun:
		return pullrequestv1.CheckKind_CHECK_KIND_CHECK_RUN
	case forge.CheckKindStatus:
		return pullrequestv1.CheckKind_CHECK_KIND_STATUS
	case forge.CheckKindPipeline:
		return pullrequestv1.CheckKind_CHECK_KIND_PIPELINE
	}

	return pullrequestv1.CheckKind_CHECK_KIND_UNSPECIFIED
}

// checkStateOf is how far along a check is as the wire carries it.
func checkStateOf(state forge.CheckState) pullrequestv1.CheckState {
	switch state {
	case forge.CheckStateQueued:
		return pullrequestv1.CheckState_CHECK_STATE_QUEUED
	case forge.CheckStateRunning:
		return pullrequestv1.CheckState_CHECK_STATE_RUNNING
	case forge.CheckStateWaiting:
		return pullrequestv1.CheckState_CHECK_STATE_WAITING
	case forge.CheckStateCompleted:
		return pullrequestv1.CheckState_CHECK_STATE_COMPLETED
	}

	return pullrequestv1.CheckState_CHECK_STATE_UNSPECIFIED
}
