package render

import (
	"komrad/internal/reviews"
	"komrad/internal/reviews/history"
	"komrad/lib/dates"
	"komrad/lib/xslice"

	reviewstore "komrad/internal/store/review"
	agentv1 "komrad/proto/gen/agent/v1"
)

// WatchedReview draws a review as the watch carries it, without its body.
func WatchedReview(review reviewstore.Review) *agentv1.WatchedReview {
	watched := &agentv1.WatchedReview{
		Id:               review.ID,
		TaskId:           review.TaskID,
		RoundId:          review.RoundID,
		RoundOrdinal:     review.RoundOrdinal,
		RoundSnapshotSha: review.RoundSnapshotSHA,
		AuthorKind:       reviewAuthorKindOf(review.AuthorKind),
		AuthorAgentId:    review.AuthorAgentID,
		CreatedAt:        dates.Stamp(review.CreatedAt),
		UpdatedAt:        dates.Stamp(review.UpdatedAt),
	}
	if review.Verdict != nil {
		watched.Verdict = reviewVerdictOf(*review.Verdict)
	}
	if at := review.ReleasedAt; at != nil {
		watched.ReleasedAt = dates.Stamp(*at)
	}
	if at := review.DeliveredAt; at != nil {
		watched.DeliveredAt = dates.Stamp(*at)
	}
	if at := review.RoundLandedAt; at != nil {
		watched.RoundLandedAt = dates.Stamp(*at)
	}

	return watched
}

// Review draws a review with its body.
func Review(review reviewstore.Review) *agentv1.Review {
	answered := &agentv1.Review{
		Id:            review.ID,
		TaskId:        review.TaskID,
		RoundId:       review.RoundID,
		RoundOrdinal:  review.RoundOrdinal,
		AuthorKind:    reviewAuthorKindOf(review.AuthorKind),
		AuthorAgentId: review.AuthorAgentID,
		Body:          review.Body,
		Path:          review.Path,
		OpenThreads:   review.OpenThreads,
		CreatedAt:     dates.Stamp(review.CreatedAt),
		UpdatedAt:     dates.Stamp(review.UpdatedAt),
	}

	if review.Verdict != nil {
		answered.Verdict = reviewVerdictOf(*review.Verdict)
	}
	if at := review.ReleasedAt; at != nil {
		answered.ReleasedAt = dates.Stamp(*at)
	}
	if at := review.DeliveredAt; at != nil {
		answered.DeliveredAt = dates.Stamp(*at)
	}

	return answered
}

// ReviewRound draws one frozen round of an agent's work.
func ReviewRound(round reviewstore.Round) *agentv1.ReviewRound {
	return &agentv1.ReviewRound{
		Id:          round.ID,
		TaskId:      round.TaskID,
		Ordinal:     round.Ordinal,
		BaseSha:     round.BaseSHA,
		SnapshotSha: round.SnapshotSHA,
		CreatedAt:   dates.Stamp(round.CreatedAt),
	}
}

// ReviewThreadInContext draws a thread with the snapshot lines around it.
func ReviewThreadInContext(thread reviews.ReviewThreadInContext) *agentv1.ReviewThreadInContext {
	return &agentv1.ReviewThreadInContext{
		Thread:  ReviewThread(thread.Thread, thread.Messages),
		Context: thread.Context,
	}
}

// ReviewViewedFile draws one file of a diff marked read.
func ReviewViewedFile(viewed reviewstore.ViewedFile) *agentv1.ReviewViewedFile {
	return &agentv1.ReviewViewedFile{
		TaskId:      viewed.TaskID,
		FilePath:    viewed.FilePath,
		Fingerprint: viewed.Fingerprint,
		ViewedAt:    dates.Stamp(viewed.ViewedAt),
	}
}

func reviewAuthorKindOf(kind reviewstore.AuthorKind) agentv1.ReviewAuthorKind {
	if kind == reviewstore.AuthorAgent {
		return agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT
	}

	return agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN
}

// ReviewHistory draws a review as the panel opens it.
func ReviewHistory(opened reviews.ReviewHistory) *agentv1.OpenReviewHistoryResponse {
	answered := &agentv1.OpenReviewHistoryResponse{
		Round:  ReviewRound(opened.Round),
		Rounds: xslice.Map(opened.Rounds, ReviewRound),
		Threads: xslice.Map(opened.Threads, func(thread reviews.ReviewThreadWithMessages) *agentv1.ReviewThread {
			return ReviewThread(thread.Thread, thread.Messages)
		}),
		Notice:              opened.Notice,
		Resolved:            reviewResolvedSpan(opened.Span),
		Widened:             opened.Widened,
		Reviews:             xslice.Map(opened.Reviews, Review),
		ChangedSinceEarlier: int32(opened.ChangedSinceEarlier),
		Traced:              map[string]*agentv1.ReviewTracedPlace{},
	}
	if earlier := opened.Earlier; earlier != nil {
		answered.EarlierRoundId = earlier.ID
	}
	if at := opened.EarlierReleasedAt; at != nil {
		answered.EarlierReleasedAt = dates.Stamp(*at)
	}
	for id, place := range opened.Traced {
		answered.Traced[id] = &agentv1.ReviewTracedPlace{
			FilePath: place.FilePath,
			NewStart: place.NewStart,
			FileOnly: place.FileOnly,
			Exact:    place.Exact,
		}
	}

	return answered
}

func reviewResolvedSpan(span history.ResolvedSpan) *agentv1.ReviewResolvedSpan {
	return &agentv1.ReviewResolvedSpan{
		BaseSha:            span.BaseSHA,
		HeadSha:            span.HeadSHA,
		Replayed:           span.Replayed,
		AnswerBaseSha:      span.AnswerBaseSHA,
		AnswerHeadSha:      span.AnswerHeadSHA,
		OriginBaseSha:      span.OriginBaseSHA,
		OriginHeadSha:      span.OriginHeadSHA,
		OriginRoundBaseSha: span.OriginRoundBaseSHA,
		Conflicts:          span.Conflicts,
	}
}
