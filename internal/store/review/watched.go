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
		return watchedReviewFromRow(models.ListReviewsOfAgentsRow(*row))
	}), nil
}

// ListForAgents returns every review of the given agents, as the watch carries them.
func (s *ReviewStore) ListForAgents(ctx context.Context, agentIDs []string) ([]Review, error) {
	storable := store.KeepIDs(agentIDs)
	if len(storable) == 0 {
		return nil, nil
	}

	rows, err := s.queries.ListReviewsOfAgents(ctx, storable)
	if err != nil {
		return nil, fmt.Errorf("failed to list the reviews of %d agents: %w", len(storable), err)
	}

	return xslice.Map(rows, func(row *models.ListReviewsOfAgentsRow) Review {
		return watchedReviewFromRow(*row)
	}), nil
}

func watchedReviewFromRow(row models.ListReviewsOfAgentsRow) Review {
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
