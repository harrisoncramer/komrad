package render

import (
	"komrad/internal/checklog"
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
		HeadSha:     reading.HeadSHA,
		FirstReadAt: dates.StampPtr(&reading.FirstReadAt),
		ReadAt:      dates.StampPtr(&reading.ReadAt),
		StatusChecks: xslice.Map(reading.StatusChecks, func(stored forge.StatusCheck) *pullrequestv1.StatusCheck {
			return statusCheck(reading, stored)
		}),
	}
}

func statusCheck(reading pullrequeststore.StatusCheckReading, stored forge.StatusCheck) *pullrequestv1.StatusCheck {
	return &pullrequestv1.StatusCheck{
		Id:           reading.CheckIDs[stored.ProviderID],
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
		RunEvent:     stored.RunEvent,
		RunStartedAt: dates.StampPtr(stored.RunStartedAt),
		Log:          checkLog(reading.GetLogOf(stored)),
	}
}

func checkLog(state pullrequeststore.CheckLogState, stored *pullrequeststore.CheckLog) *pullrequestv1.CheckLog {
	log := &pullrequestv1.CheckLog{State: checkLogStateOf(state)}
	if stored == nil {
		return log
	}

	log.FetchedAt = dates.StampPtr(&stored.FetchedAt)
	log.LineCount = int32(stored.LineCount)
	log.ErrorCount = int32(stored.ErrorCount)
	log.Steps = xslice.Map(stored.Steps, checkLogStep)

	return log
}

func checkLogStep(step checklog.StepRange) *pullrequestv1.CheckLogStep {
	return &pullrequestv1.CheckLogStep{
		Number:     int32(step.Number),
		Name:       step.Name,
		Conclusion: step.Conclusion,
		FirstLine:  int32(step.FirstLine),
		LineCount:  int32(step.LineCount),
		ErrorCount: int32(step.ErrorCount),
	}
}

// checkLogStateOf is where fetching a check's log stands as the wire carries it.
func checkLogStateOf(state pullrequeststore.CheckLogState) pullrequestv1.CheckLogState {
	switch state {
	case pullrequeststore.CheckLogStateNone:
		return pullrequestv1.CheckLogState_CHECK_LOG_STATE_NONE
	case pullrequeststore.CheckLogStatePending:
		return pullrequestv1.CheckLogState_CHECK_LOG_STATE_PENDING
	case pullrequeststore.CheckLogState(pullrequeststore.CheckLogFetched):
		return pullrequestv1.CheckLogState_CHECK_LOG_STATE_FETCHED
	case pullrequeststore.CheckLogState(pullrequeststore.CheckLogTooLarge):
		return pullrequestv1.CheckLogState_CHECK_LOG_STATE_TOO_LARGE
	case pullrequeststore.CheckLogState(pullrequeststore.CheckLogMissing):
		return pullrequestv1.CheckLogState_CHECK_LOG_STATE_MISSING
	case pullrequeststore.CheckLogState(pullrequeststore.CheckLogExpired):
		return pullrequestv1.CheckLogState_CHECK_LOG_STATE_EXPIRED
	case pullrequeststore.CheckLogState(pullrequeststore.CheckLogFailed):
		return pullrequestv1.CheckLogState_CHECK_LOG_STATE_FAILED
	}

	return pullrequestv1.CheckLogState_CHECK_LOG_STATE_UNSPECIFIED
}

// CheckLogLines draws a window of a check's log as the wire carries it.
func CheckLogLines(lines *pullrequeststore.CheckLogLines) *pullrequestv1.ReadCheckLogResponse {
	return &pullrequestv1.ReadCheckLogResponse{
		Lines: xslice.Map(lines.Lines, func(text string) *pullrequestv1.CheckLogLine {
			return &pullrequestv1.CheckLogLine{Text: text}
		}),
		LineCount: int32(lines.LineCount),
	}
}

// CheckLogSearch draws what a search across check logs found as the wire carries it.
func CheckLogSearch(search *pullrequeststore.CheckLogSearch) *pullrequestv1.SearchCheckLogsResponse {
	return &pullrequestv1.SearchCheckLogsResponse{
		Hits: xslice.Map(search.Hits, func(hit pullrequeststore.CheckLogHit) *pullrequestv1.CheckLogHit {
			return &pullrequestv1.CheckLogHit{
				CheckId:    hit.CheckID,
				Line:       int32(hit.Line),
				StepNumber: int32(hit.StepNumber),
				Text:       hit.Text,
				Cut:        hit.Cut,
			}
		}),
		Limited: search.Limited,
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
	case forge.StatusCheckStateUnknown:
		return pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_UNKNOWN
	}

	return pullrequestv1.StatusCheckState_STATUS_CHECK_STATE_UNSPECIFIED
}
