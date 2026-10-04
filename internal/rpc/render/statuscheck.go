package render

import (
	"komrad/internal/forge"
	pullrequeststore "komrad/internal/store/pullrequest"
	"komrad/lib/dates"
	"komrad/lib/xslice"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
)

// StatusCheckReadings draws the checks read on each of a pull request's heads as the wire carries them.
func StatusCheckReadings(readings []pullrequeststore.StatusCheckReading) *pullrequestv1.ListStatusChecksResponse {
	return &pullrequestv1.ListStatusChecksResponse{
		Readings: xslice.Map(readings, statusCheckReading),
	}
}

func statusCheckReading(reading pullrequeststore.StatusCheckReading) *pullrequestv1.StatusCheckReading {
	return &pullrequestv1.StatusCheckReading{
		HeadSha:      reading.HeadSHA,
		FirstReadAt:  dates.StampPtr(&reading.FirstReadAt),
		ReadAt:       dates.StampPtr(&reading.ReadAt),
		StatusChecks: xslice.Map(reading.StatusChecks, statusCheck),
	}
}

func statusCheck(stored forge.StatusCheck) *pullrequestv1.StatusCheck {
	return &pullrequestv1.StatusCheck{
		ProviderId:   stored.ProviderID,
		Kind:         statusCheckKindOf(stored.Kind),
		Source:       stored.Source,
		Name:         stored.Name,
		State:        statusCheckStateOf(stored.State),
		Conclusion:   stored.Conclusion,
		StartedAt:    dates.StampPtr(stored.StartedAt),
		FinishedAt:   dates.StampPtr(stored.FinishedAt),
		Required:     stored.Required,
		Url:          stored.URL,
		Steps:        xslice.Map(stored.Steps, statusCheckStep),
		RunId:        stored.RunID,
		RunEvent:     stored.RunEvent,
		RunStartedAt: dates.StampPtr(stored.RunStartedAt),
	}
}

func statusCheckStep(stored forge.StatusCheckStep) *pullrequestv1.StatusCheckStep {
	return &pullrequestv1.StatusCheckStep{
		Number:     int32(stored.Number),
		Name:       stored.Name,
		State:      statusCheckStateOf(stored.State),
		Conclusion: stored.Conclusion,
		StartedAt:  dates.StampPtr(stored.StartedAt),
		FinishedAt: dates.StampPtr(stored.FinishedAt),
	}
}

// statusCheckKindOf is a check's kind as the wire carries it.
func statusCheckKindOf(kind forge.StatusCheckKind) pullrequestv1.StatusCheckKind {
	switch kind {
	case forge.StatusCheckKindRun:
		return pullrequestv1.StatusCheckKind_STATUS_CHECK_KIND_CHECK_RUN
	case forge.StatusCheckKindStatus:
		return pullrequestv1.StatusCheckKind_STATUS_CHECK_KIND_STATUS
	case forge.StatusCheckKindPipeline:
		return pullrequestv1.StatusCheckKind_STATUS_CHECK_KIND_PIPELINE
	}

	return pullrequestv1.StatusCheckKind_STATUS_CHECK_KIND_UNSPECIFIED
}

// statusCheckStateOf is how far along a check is as the wire carries it.
func statusCheckStateOf(state forge.StatusCheckState) pullrequestv1.StatusCheckState {
	switch state {
	case forge.StatusCheckStateQueued:
		return pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_QUEUED
	case forge.StatusCheckStateRunning:
		return pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_RUNNING
	case forge.StatusCheckStateWaiting:
		return pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_WAITING
	case forge.StatusCheckStateCompleted:
		return pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_COMPLETED
	}

	return pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_UNSPECIFIED
}
