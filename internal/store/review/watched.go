package review

import (
	"context"
	"fmt"

	"komrad/db/models"
	"komrad/internal/store"
	"komrad/internal/watch"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	watchv1 "komrad/proto/gen/watch/v1"

	"github.com/google/uuid"
)

// WatchedReviews declares the reviews to the watch.
var WatchedReviews = watch.Resource{
	Enum:      watchv1.Resource_RESOURCE_REVIEW,
	Table:     "reviews",
	KeyColumn: "id",
	Feeders: []watch.Feeder{
		{
			Table: "review_snapshots",
			Lookup: &watch.Lookup{
				ParentColumn: "snapshot_id",
				ChildColumn:  "id",
			},
		},
	},
}

// ListWatched returns whichever of keys are still reviews, or every review when keys is nil.
func (s *ReviewStore) ListWatched(ctx context.Context, keys []uuid.UUID) ([]Review, error) {
	rows, err := s.queries.ListWatchedReviews(ctx, keys)
	if err != nil {
		return nil, fmt.Errorf("failed to list the watched reviews: %w", err)
	}

	return xslice.TryMap(rows, func(row *models.ListWatchedReviewsRow) (Review, error) {
		return watchedReviewFromRow(*row)
	})
}

// ListIDsOfTasks returns the ids of every review of the given tasks.
func (s *ReviewStore) ListIDsOfTasks(ctx context.Context, taskIDs []uuid.UUID) ([]uuid.UUID, error) {
	if len(taskIDs) == 0 {
		return nil, nil
	}

	ids, err := s.queries.ListReviewIDsOfTasks(ctx, taskIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to list the reviews of %d tasks: %w", len(taskIDs), err)
	}

	return ids, nil
}

func watchedReviewFromRow(row models.ListWatchedReviewsRow) (Review, error) {
	authorKind, err := fromDBToServiceReviewAuthorKind(row.AuthorKind)
	if err != nil {
		return Review{}, err
	}
	verdict, err := fromDBToServiceNullReviewVerdict(row.Verdict)
	if err != nil {
		return Review{}, err
	}

	return Review{
		ID:               row.ID,
		TaskID:           row.TaskID,
		SnapshotID:       row.SnapshotID,
		SnapshotOrdinal:  row.SnapshotOrdinal,
		SnapshotSHA:      row.SnapshotSha,
		SnapshotLandedAt: row.SnapshotLandedAt,
		AuthorKind:       authorKind,
		AuthorAgentID:    row.AuthorAgentID,
		ReleasedAt:       row.ReleasedAt,
		CreatedAt:        row.CreatedAt,
		UpdatedAt:        row.UpdatedAt,
		Verdict:          verdict,
	}, nil
}

// WatchedReviewThreads declares the threads on live agents to the watch.
var WatchedReviewThreads = watch.Resource{
	Enum:      watchv1.Resource_RESOURCE_REVIEW_THREAD,
	Table:     "review_threads",
	KeyColumn: "id",
	Feeders: []watch.Feeder{
		{
			Table:  "review_messages",
			Column: "thread_id",
		},
		{
			Table:  "review_forge_replies",
			Column: "thread_id",
		},
		{
			Table: "agents",
			Lookup: &watch.Lookup{
				ParentColumn: "task_id",
				ChildColumn:  "task_id",
				OnChangeOf:   "archived_at",
			},
		},
	},
}

// ThreadWithMessages is a thread and everything said in it, oldest first.
type ThreadWithMessages struct {
	Thread
	Messages     []Message
	ForgeReplies []ForgeReply
}

type ThreadStore struct {
	queries *models.Queries
}

// NewThreadStore builds the watch's reader of review threads over a pool or transaction.
func NewThreadStore(db store.DBTX) *ThreadStore {
	return &ThreadStore{queries: models.New(db)}
}

// ListWatched returns whichever of keys are still threads on live agents, or every such thread when keys is nil.
func (s *ThreadStore) ListWatched(ctx context.Context, keys []uuid.UUID) ([]ThreadWithMessages, error) {
	rows, err := s.queries.ListWatchedReviewThreads(ctx, keys)
	if err != nil {
		return nil, fmt.Errorf("failed to list the watched review threads: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}

	ids := xslice.Map(rows, func(row *models.ListWatchedReviewThreadsRow) uuid.UUID { return row.ID })

	messages, err := s.queries.ListReviewMessagesOfThreads(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to list the messages of %d watched review thread(s): %w", len(ids), err)
	}

	byThread := map[uuid.UUID][]Message{}
	for _, row := range messages {
		message, err := messageOfThreadFromRow(*row)
		if err != nil {
			return nil, err
		}
		byThread[row.ReviewMessage.ThreadID] = append(byThread[row.ReviewMessage.ThreadID], *message)
	}

	replies, err := s.queries.ListForgeReplies(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to list the forge replies of %d watched review thread(s): %w", len(ids), err)
	}

	repliesByThread := map[uuid.UUID][]ForgeReply{}
	for _, row := range replies {
		reply, err := listedForgeReplyFromRow(row)
		if err != nil {
			return nil, err
		}
		repliesByThread[reply.ThreadID] = append(repliesByThread[reply.ThreadID], reply)
	}

	return xslice.TryMap(rows, func(row *models.ListWatchedReviewThreadsRow) (ThreadWithMessages, error) {
		thread, err := threadFromRow(models.ReviewThread{
			ID:                  row.ID,
			TaskID:              row.TaskID,
			ReviewID:            row.ReviewID,
			AuthorKind:          row.AuthorKind,
			AuthorAgentID:       row.AuthorAgentID,
			SnapshotID:          row.SnapshotID,
			ForgeThreadID:       row.ForgeThreadID,
			BaseSha:             row.BaseSha,
			FilePath:            row.FilePath,
			OldStart:            row.OldStart,
			OldEnd:              row.OldEnd,
			NewStart:            row.NewStart,
			NewEnd:              row.NewEnd,
			QuotedLines:         row.QuotedLines,
			LastAgentActivityAt: row.LastAgentActivityAt,
			HumanReadAt:         row.HumanReadAt,
			ResolvedAt:          row.ResolvedAt,
			ResolvedByKind:      row.ResolvedByKind,
			ResolvedByAgentID:   row.ResolvedByAgentID,
			CreatedAt:           row.CreatedAt,
			UpdatedAt:           row.UpdatedAt,
		})
		if err != nil {
			return ThreadWithMessages{}, err
		}
		thread.SnapshotOrdinal = row.SnapshotOrdinal
		thread.UnreadByHuman = xptr.Deref(row.UnreadByHuman)

		return ThreadWithMessages{
			Thread:       *thread,
			Messages:     byThread[row.ID],
			ForgeReplies: repliesByThread[row.ID],
		}, nil
	})
}

// ListIDsOfTasks returns the ids of every review thread on the given tasks.
func (s *ThreadStore) ListIDsOfTasks(ctx context.Context, taskIDs []uuid.UUID) ([]uuid.UUID, error) {
	if len(taskIDs) == 0 {
		return nil, nil
	}

	ids, err := s.queries.ListReviewThreadIDsOfTasks(ctx, taskIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to list the review threads of %d tasks: %w", len(taskIDs), err)
	}

	return ids, nil
}
