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
		Id:           reading.CheckIDs[stored.ProviderID].String(),
		ProviderId:   stored.ProviderID,
		Kind:         stored.Kind,
		Source:       stored.Source,
		Name:         stored.Name,
		State:        stored.State,
		Conclusion:   stored.Conclusion,
		StartedAt:    dates.StampPtr(stored.StartedAt),
		FinishedAt:   dates.StampPtr(stored.FinishedAt),
		Required:     stored.Required,
		Url:          stored.URL,
		Steps:        xslice.Map(stored.Steps, statusCheckStep),
		RunEvent:     stored.RunEvent,
		RunStartedAt: dates.StampPtr(stored.RunStartedAt),
		Log:          checkLog(reading.GetLogOf(stored)),
		Passed:       stored.IsPassing(),
	}
}

func checkLog(state pullrequestv1.CheckLogState, stored *pullrequeststore.CheckLog) *pullrequestv1.CheckLog {
	log := &pullrequestv1.CheckLog{State: state}
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
		Passed:     forge.IsPassingConclusion(step.Conclusion),
	}
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
				CheckId:    hit.CheckID.String(),
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
		State:      stored.State,
		Conclusion: stored.Conclusion,
		StartedAt:  dates.StampPtr(stored.StartedAt),
		FinishedAt: dates.StampPtr(stored.FinishedAt),
		Passed:     stored.IsPassing(),
	}
}
