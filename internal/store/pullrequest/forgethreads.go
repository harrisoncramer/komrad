package pullrequest

import (
	"context"
	"fmt"
	"time"

	"komrad/db/models"
	"komrad/internal/forge"
	"komrad/internal/store"
	"komrad/internal/watch"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	watchv1 "komrad/proto/gen/watch/v1"
)

// WatchedForgeThreads declares the forge's review threads on watched pull requests to the watch.
var WatchedForgeThreads = watch.Resource{
	Enum:      watchv1.Resource_RESOURCE_FORGE_THREAD,
	Table:     "forge_review_threads",
	KeyColumn: "id",
	Feeders: []watch.Feeder{
		{
			Table:  "forge_review_comments",
			Column: "thread_id",
		},
	},
}

// CommentSyncTarget is an open pull request a live agent is linked to, and what the comment sync last read of it.
type CommentSyncTarget struct {
	ID            string
	RepositoryURL string
	Number        int
	Mark          string
	ReadAt        *time.Time
}

// ForgeThread is one review thread as the forge holds it, with its comments oldest first.
type ForgeThread struct {
	ID            string
	PullRequestID string
	ProviderID    string
	Path          string
	Side          forge.Side
	CommitSHA     string
	BaseSHA       string
	StartLine     int
	EndLine       int
	QuotedLines   []string
	Resolved      bool
	Outdated      bool
	WrittenAt     time.Time
	Comments      []ForgeComment
}

// ForgeComment is one comment in a forge review thread.
type ForgeComment struct {
	ID          string
	ProviderID  string
	AuthorLogin string
	AuthorIsBot bool
	Body        string
	WrittenAt   time.Time
	EditedAt    *time.Time
}

// SetThreadsParams is every thread the forge holds on one pull request, and the mark it was read under.
type SetThreadsParams struct {
	PullRequestID string
	Mark          string
	ReadAt        time.Time
	Threads       []forge.ReviewThread
}

// ForgeThreadStore holds the forge's copy of the review threads on the pull requests agents are linked to.
type ForgeThreadStore struct {
	queries *models.Queries
}

// NewForgeThreadStore builds the store over a pool or transaction.
func NewForgeThreadStore(db store.DBTX) *ForgeThreadStore {
	return &ForgeThreadStore{queries: models.New(db)}
}

// ListCommentSyncTargets returns every open pull request a live agent is linked to, drafts included.
func (s *ForgeThreadStore) ListCommentSyncTargets(ctx context.Context) ([]CommentSyncTarget, error) {
	rows, err := s.queries.ListCommentSyncPullRequests(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the pull requests to read comments on: %w", err)
	}

	return xslice.Map(rows, func(row *models.ListCommentSyncPullRequestsRow) CommentSyncTarget {
		return CommentSyncTarget{
			ID:            row.ID,
			RepositoryURL: row.RepositoryUrl,
			Number:        int(row.Number),
			Mark:          xptr.Deref(row.CommentsMark),
			ReadAt:        row.CommentsReadAt,
		}
	}), nil
}

// SetThreads writes every thread and comment the forge holds on a pull request, removes the ones it no longer does, and records the read.
func (s *ForgeThreadStore) SetThreads(ctx context.Context, params SetThreadsParams) error {
	kept := make([]string, 0, len(params.Threads))
	for _, thread := range params.Threads {
		kept = append(kept, thread.ProviderID)
		if err := s.queries.UpsertForgeReviewThread(ctx, toThreadUpsert(params.PullRequestID, thread)); err != nil {
			return fmt.Errorf("failed to write forge thread %s: %w", thread.ProviderID, err)
		}
	}

	if err := s.queries.DeleteForgeReviewThreadsExcept(ctx, models.DeleteForgeReviewThreadsExceptParams{
		PullRequestID: params.PullRequestID,
		Kept:          kept,
	}); err != nil {
		return fmt.Errorf("failed to remove the forge threads of pull request %s: %w", params.PullRequestID, err)
	}

	held, err := s.queries.ListForgeReviewThreadIDs(ctx, params.PullRequestID)
	if err != nil {
		return fmt.Errorf("failed to list the forge threads of pull request %s: %w", params.PullRequestID, err)
	}

	ids := make(map[string]string, len(held))
	for _, row := range held {
		ids[row.ProviderID] = row.ID
	}

	for _, thread := range params.Threads {
		if err := s.replaceComments(ctx, ids[thread.ProviderID], thread.Comments); err != nil {
			return err
		}
	}

	if err := s.queries.MarkPullRequestCommentsRead(ctx, models.MarkPullRequestCommentsReadParams{
		ID:           params.PullRequestID,
		CommentsMark: &params.Mark,
		ReadAt:       &params.ReadAt,
	}); err != nil {
		return fmt.Errorf("failed to record the comment read of pull request %s: %w", params.PullRequestID, err)
	}

	return nil
}

func (s *ForgeThreadStore) replaceComments(ctx context.Context, threadID string, comments []forge.ReviewComment) error {
	kept := make([]string, 0, len(comments))
	for _, comment := range comments {
		kept = append(kept, comment.ProviderID)
		if err := s.queries.UpsertForgeReviewComment(ctx, models.UpsertForgeReviewCommentParams{
			ThreadID:    threadID,
			ProviderID:  comment.ProviderID,
			AuthorLogin: comment.Login,
			AuthorIsBot: comment.Bot,
			Body:        comment.Body,
			WrittenAt:   comment.WrittenAt,
			EditedAt:    comment.EditedAt,
		}); err != nil {
			return fmt.Errorf("failed to write forge comment %s: %w", comment.ProviderID, err)
		}
	}

	if err := s.queries.DeleteForgeReviewCommentsExcept(ctx, models.DeleteForgeReviewCommentsExceptParams{
		ThreadID: threadID,
		Kept:     kept,
	}); err != nil {
		return fmt.Errorf("failed to remove the comments of forge thread %s: %w", threadID, err)
	}

	return nil
}

func toThreadUpsert(pullRequestID string, thread forge.ReviewThread) models.UpsertForgeReviewThreadParams {
	params := models.UpsertForgeReviewThreadParams{
		PullRequestID: pullRequestID,
		ProviderID:    thread.ProviderID,
		Path:          thread.Path,
		Side:          string(thread.Side),
		CommitSha:     thread.CommitSHA,
		BaseSha:       thread.BaseSHA,
		DiffHunk:      thread.DiffHunk,
		QuotedLines:   thread.QuotedLines(),
		Resolved:      thread.Resolved,
		Outdated:      thread.Outdated,
	}
	if params.QuotedLines == nil {
		params.QuotedLines = []string{}
	}
	if thread.StartLine > 0 {
		params.StartLine = xptr.Ptr(int32(thread.StartLine))
	}
	if thread.EndLine > 0 {
		params.EndLine = xptr.Ptr(int32(thread.EndLine))
	}
	if len(thread.Comments) > 0 {
		params.WrittenAt = thread.Comments[0].WrittenAt
	}

	return params
}

// ListWatched returns whichever of keys are still forge threads on a watched pull request, or every one when keys is nil.
func (s *ForgeThreadStore) ListWatched(ctx context.Context, keys []string) ([]ForgeThread, error) {
	if keys != nil {
		keys = store.KeepIDs(keys)
	}

	rows, err := s.queries.ListWatchedForgeReviewThreads(ctx, keys)
	if err != nil {
		return nil, fmt.Errorf("failed to list the watched forge threads: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}

	ids := xslice.Map(rows, func(row *models.ForgeReviewThread) string { return row.ID })

	comments, err := s.queries.ListForgeReviewCommentsOfThreads(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to list the comments of %d forge thread(s): %w", len(ids), err)
	}

	byThread := map[string][]ForgeComment{}
	for _, row := range comments {
		byThread[row.ThreadID] = append(byThread[row.ThreadID], ForgeComment{
			ID:          row.ID,
			ProviderID:  row.ProviderID,
			AuthorLogin: row.AuthorLogin,
			AuthorIsBot: row.AuthorIsBot,
			Body:        row.Body,
			WrittenAt:   row.WrittenAt,
			EditedAt:    row.EditedAt,
		})
	}

	return xslice.Map(rows, func(row *models.ForgeReviewThread) ForgeThread {
		return ForgeThread{
			ID:            row.ID,
			PullRequestID: row.PullRequestID,
			ProviderID:    row.ProviderID,
			Path:          row.Path,
			Side:          forge.Side(row.Side),
			CommitSHA:     row.CommitSha,
			StartLine:     int(xptr.Deref(row.StartLine)),
			EndLine:       int(xptr.Deref(row.EndLine)),
			QuotedLines:   row.QuotedLines,
			Resolved:      row.Resolved,
			Outdated:      row.Outdated,
			WrittenAt:     row.WrittenAt,
			Comments:      byThread[row.ID],
		}
	}), nil
}
