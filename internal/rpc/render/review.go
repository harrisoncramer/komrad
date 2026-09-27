package render

import (
	"komrad/lib/dates"

	reviewstore "komrad/internal/store/review"
	agentv1 "komrad/proto/gen/agent/v1"
)

// WatchedReview draws a review as the watch carries it, without its body.
func WatchedReview(review reviewstore.Review) *agentv1.WatchedReview {
	kind := agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN
	if review.AuthorKind == reviewstore.AuthorAgent {
		kind = agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT
	}

	watched := &agentv1.WatchedReview{
		Id:               review.ID,
		AgentId:          review.AgentID,
		RoundId:          review.RoundID,
		RoundOrdinal:     review.RoundOrdinal,
		RoundSnapshotSha: review.RoundSnapshotSHA,
		AuthorKind:       kind,
		AuthorAgentId:    review.AuthorAgentID,
		CreatedAt:        dates.Stamp(review.CreatedAt),
		UpdatedAt:        dates.Stamp(review.UpdatedAt),
	}
	if review.Verdict != nil {
		watched.Verdict = ReviewVerdictOf(*review.Verdict)
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
