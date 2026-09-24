package review

import (
	"context"
	"fmt"

	"weave/db/models"
	"weave/internal/watch"
	"weave/lib/xslice"
	watchv1 "weave/proto/gen/watch/v1"
)

// WatchedReviews declares the reviews to the watch.
var WatchedReviews = watch.Resource{
	Enum:      watchv1.Resource_RESOURCE_REVIEW,
	Table:     "reviews",
	KeyColumn: "id",
	Feeders: []watch.Feeder{
		{
			Table: "mail",
			Lookup: &watch.Lookup{
				ParentColumn: "doorbell_mail_id",
				ChildColumn:  "id",
				OnChangeOf:   "claimed_at",
			},
		},
		{
			Table: "review_rounds",
			Lookup: &watch.Lookup{
				ParentColumn: "round_id",
				ChildColumn:  "id",
			},
		},
	},
}

// ListWatched returns whichever of keys are still reviews, or every review when keys is nil.
func (s *ReviewStore) ListWatched(ctx context.Context, keys []string) ([]Review, error) {
	rows, err := s.queries.ListWatchedReviews(ctx, models.ListWatchedReviewsParams{Keys: keys})
	if err != nil {
		return nil, fmt.Errorf("failed to list the watched reviews: %w", err)
	}

	return xslice.Map(rows, watchedReviewFromRow), nil
}

// ListForAgents returns every review of the given agents, as the watch carries them.
func (s *ReviewStore) ListForAgents(ctx context.Context, agentIDs []string) ([]Review, error) {
	if len(agentIDs) == 0 {
		return nil, nil
	}

	rows, err := s.queries.ListWatchedReviews(ctx, models.ListWatchedReviewsParams{AgentIds: agentIDs})
	if err != nil {
		return nil, fmt.Errorf("failed to list the reviews of %d agents: %w", len(agentIDs), err)
	}

	return xslice.Map(rows, watchedReviewFromRow), nil
}

func watchedReviewFromRow(row *models.ListWatchedReviewsRow) Review {
	review := reviewFromRow(row.Review)
	review.RoundOrdinal = row.RoundOrdinal
	review.RoundSnapshotSHA = row.RoundSnapshotSha
	review.DeliveredAt = row.DeliveredAt

	return review
}
