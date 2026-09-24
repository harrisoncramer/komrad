package review

import (
	"context"
	"fmt"

	"weave/db/models"
	"weave/internal/store"
	"weave/internal/watch"
	"weave/lib/xptr"
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
	rows, err := s.queries.ListWatchedReviews(ctx, keys)
	if err != nil {
		return nil, fmt.Errorf("failed to list the watched reviews: %w", err)
	}

	return xslice.Map(rows, func(row *models.ListWatchedReviewsRow) Review {
		return watchedReviewFromRow(*row)
	}), nil
}

// ListIDsOfAgents returns the ids of every review of the given agents.
func (s *ReviewStore) ListIDsOfAgents(ctx context.Context, agentIDs []string) ([]string, error) {
	storable := store.KeepIDs(agentIDs)
	if len(storable) == 0 {
		return nil, nil
	}

	ids, err := s.queries.ListReviewIDsOfAgents(ctx, storable)
	if err != nil {
		return nil, fmt.Errorf("failed to list the reviews of %d agents: %w", len(storable), err)
	}

	return ids, nil
}

func watchedReviewFromRow(row models.ListWatchedReviewsRow) Review {
	review := Review{
		ID:               row.ID,
		AgentID:          row.AgentID,
		RoundID:          row.RoundID,
		RoundOrdinal:     row.RoundOrdinal,
		RoundSnapshotSHA: row.RoundSnapshotSha,
		AuthorKind:       AuthorKind(row.AuthorKind),
		AuthorAgentID:    row.AuthorAgentID,
		ReleasedAt:       row.ReleasedAt,
		DeliveredAt:      row.DeliveredAt,
		CreatedAt:        row.CreatedAt,
		UpdatedAt:        row.UpdatedAt,
	}
	if row.Verdict.Valid {
		review.Verdict = xptr.Ptr(Verdict(row.Verdict.ReviewVerdict))
	}

	return review
}
