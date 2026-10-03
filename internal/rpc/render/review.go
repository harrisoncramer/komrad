package render

import (
	"komrad/internal/reviews"
	"komrad/lib/dates"

	reviewstore "komrad/internal/store/review"
	agentv1 "komrad/proto/gen/agent/v1"
)

// WatchedReview draws a review as the watch carries it, without its body.
func WatchedReview(review reviewstore.Review) *agentv1.WatchedReview {
	watched := &agentv1.WatchedReview{
		Id:               review.ID,
		AgentId:          review.AgentID,
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
		AgentId:       review.AgentID,
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
		AgentId:     round.AgentID,
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
		AgentId:     viewed.AgentID,
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
