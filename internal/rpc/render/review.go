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
		Id:              review.ID,
		TaskId:          review.TaskID,
		SnapshotId:      review.SnapshotID,
		SnapshotOrdinal: review.SnapshotOrdinal,
		SnapshotSha:     review.SnapshotSHA,
		AuthorKind:      reviewAuthorKindOf(review.AuthorKind),
		AuthorAgentId:   review.AuthorAgentID,
		CreatedAt:       dates.Stamp(review.CreatedAt),
		UpdatedAt:       dates.Stamp(review.UpdatedAt),
	}
	if review.Verdict != nil {
		watched.Verdict = reviewVerdictOf(*review.Verdict)
	}
	if at := review.ReleasedAt; at != nil {
		watched.ReleasedAt = dates.Stamp(*at)
	}
	if at := review.SnapshotLandedAt; at != nil {
		watched.SnapshotLandedAt = dates.Stamp(*at)
	}

	return watched
}

// Review draws a review with its body.
func Review(review reviewstore.Review) *agentv1.Review {
	answered := &agentv1.Review{
		Id:              review.ID,
		TaskId:          review.TaskID,
		SnapshotId:      review.SnapshotID,
		SnapshotOrdinal: review.SnapshotOrdinal,
		AuthorKind:      reviewAuthorKindOf(review.AuthorKind),
		AuthorAgentId:   review.AuthorAgentID,
		Body:            review.Body,
		Path:            review.Path,
		OpenThreads:     review.OpenThreads,
		CreatedAt:       dates.Stamp(review.CreatedAt),
		UpdatedAt:       dates.Stamp(review.UpdatedAt),
	}

	if review.Verdict != nil {
		answered.Verdict = reviewVerdictOf(*review.Verdict)
	}
	if at := review.ReleasedAt; at != nil {
		answered.ReleasedAt = dates.Stamp(*at)
	}

	return answered
}

// ReviewSnapshot draws one frozen snapshot of an agent's work.
func ReviewSnapshot(snapshot reviewstore.Snapshot) *agentv1.ReviewSnapshot {
	return &agentv1.ReviewSnapshot{
		Id:        snapshot.ID,
		TaskId:    snapshot.TaskID,
		Ordinal:   snapshot.Ordinal,
		BaseSha:   snapshot.BaseSHA,
		Sha:       snapshot.SHA,
		CreatedAt: dates.Stamp(snapshot.CreatedAt),
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
		Snapshot:  ReviewSnapshot(opened.Snapshot),
		Snapshots: xslice.Map(opened.Snapshots, ReviewSnapshot),
		Threads: xslice.Map(opened.Threads, func(thread reviews.ReviewThreadWithMessages) *agentv1.ReviewThread {
			return ReviewThread(thread.Thread, thread.Messages)
		}),
		Notice:              opened.Notice,
		Resolved:            reviewResolvedSpan(opened.Span),
		Widened:             reviewWidening(opened.Widened),
		ChangedSinceEarlier: int32(opened.ChangedSinceEarlier),
		Traced:              map[string]*agentv1.ReviewTracedPlace{},
	}
	if earlier := opened.Earlier; earlier != nil {
		answered.EarlierSnapshotId = earlier.ID
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
		BaseSha:               span.BaseSHA,
		HeadSha:               span.HeadSHA,
		Replayed:              span.Replayed,
		AnswerBaseSha:         span.AnswerBaseSHA,
		AnswerHeadSha:         span.AnswerHeadSHA,
		OriginBaseSha:         span.OriginBaseSHA,
		OriginHeadSha:         span.OriginHeadSHA,
		OriginSnapshotBaseSha: span.OriginSnapshotBaseSHA,
		Conflicts:             span.Conflicts,
	}
}

func reviewWidening(widened *history.Widening) *agentv1.ReviewWidening {
	if widened == nil {
		return nil
	}

	return &agentv1.ReviewWidening{
		Reason:    reviewWidenedReasonOf(widened.Reason),
		Conflicts: widened.Conflicts,
	}
}

func reviewWidenedReasonOf(reason history.WidenedReason) agentv1.ReviewWidenedReason {
	switch reason {
	case history.WidenedAnswerGone:
		return agentv1.ReviewWidenedReason_REVIEW_WIDENED_REASON_ANSWER_GONE
	case history.WidenedCommentSnapshotGone:
		return agentv1.ReviewWidenedReason_REVIEW_WIDENED_REASON_COMMENT_SNAPSHOT_GONE
	case history.WidenedRangeSnapshotGone:
		return agentv1.ReviewWidenedReason_REVIEW_WIDENED_REASON_RANGE_SNAPSHOT_GONE
	case history.WidenedRangeBackwards:
		return agentv1.ReviewWidenedReason_REVIEW_WIDENED_REASON_RANGE_BACKWARDS
	case history.WidenedAnswerAcrossRebase:
		return agentv1.ReviewWidenedReason_REVIEW_WIDENED_REASON_ANSWER_ACROSS_REBASE
	}

	return agentv1.ReviewWidenedReason_REVIEW_WIDENED_REASON_UNSPECIFIED
}
