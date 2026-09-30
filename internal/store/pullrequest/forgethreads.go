package pullrequest

import (
	"context"
	"fmt"
	"time"

	"komrad/db"
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

// CommentSyncTarget is an open pull request a live agent is linked to.
type CommentSyncTarget struct {
	ID            string
	RepositoryURL string
	Number        int
}

// ForgeThread is one review thread as the forge holds it, with its comments oldest first.
type ForgeThread struct {
	ID            string
	PullRequestID string
	ProviderID    string
	Path          string
	Line          int
	OldSide       bool
	CommitSHA     string
	Quote         string
	URL           string
	Resolved      bool
	WrittenAt     time.Time
	Dismissed     bool
	Comments      []ForgeComment
}

// ForgeComment is one comment in a forge review thread.
type ForgeComment struct {
	ID          string
	ProviderID  string
	AuthorLogin string
	AuthorIsBot bool
	Yours       bool
	Body        string
	WrittenAt   time.Time
	EditedAt    *time.Time
}

// SetThreadsParams is what the forge answered for one pull request's threads, read as Viewer.
type SetThreadsParams struct {
	PullRequestID string
	Viewer        string
	Threads       []forge.ReviewThread
	Prune         bool
}

// ForgeThreadStore holds the forge's copy of the review threads on the pull requests agents are linked to.
type ForgeThreadStore struct {
	queries *models.Queries
}

// NewForgeThreadStore builds the store over a pool or transaction.
func NewForgeThreadStore(db store.DBTX) *ForgeThreadStore {
	return &ForgeThreadStore{queries: models.New(db)}
}

// ListCommentSyncTargets returns every open GitHub pull request a live agent is linked to, drafts included.
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
		}
	}), nil
}

// SetThreads writes every thread and comment the forge answered on a pull request, and when Prune is set removes the ones it no longer holds.
func (s *ForgeThreadStore) SetThreads(ctx context.Context, params SetThreadsParams) error {
	for _, thread := range params.Threads {
		if err := s.queries.UpsertForgeReviewThread(ctx, toThreadUpsert(params.PullRequestID, thread)); err != nil {
			return fmt.Errorf("failed to write forge thread %s: %w", thread.ProviderID, err)
		}
	}

	if params.Prune {
		if err := s.queries.DeleteForgeReviewThreadsExcept(ctx, models.DeleteForgeReviewThreadsExceptParams{
			PullRequestID: params.PullRequestID,
			Kept:          xslice.Map(params.Threads, func(thread forge.ReviewThread) string { return thread.ProviderID }),
		}); err != nil {
			return fmt.Errorf("failed to remove the forge threads of pull request %s: %w", params.PullRequestID, err)
		}
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
		if err := s.setComments(ctx, setCommentsParams{
			ThreadID: ids[thread.ProviderID],
			Comments: thread.Comments,
			Viewer:   params.Viewer,
			Prune:    params.Prune,
		}); err != nil {
			return err
		}
	}

	return nil
}

type setCommentsParams struct {
	ThreadID string
	Comments []forge.ReviewComment
	Viewer   string
	Prune    bool
}

func (s *ForgeThreadStore) setComments(ctx context.Context, params setCommentsParams) error {
	for _, comment := range params.Comments {
		if err := s.queries.UpsertForgeReviewComment(ctx, models.UpsertForgeReviewCommentParams{
			ThreadID:    params.ThreadID,
			ProviderID:  comment.ProviderID,
			AuthorLogin: comment.Login,
			AuthorIsBot: comment.Bot,
			Yours:       comment.Login != "" && comment.Login == params.Viewer,
			Body:        comment.Body,
			WrittenAt:   comment.WrittenAt,
			EditedAt:    comment.EditedAt,
		}); err != nil {
			return fmt.Errorf("failed to write forge comment %s: %w", comment.ProviderID, err)
		}
	}

	if !params.Prune {
		return nil
	}

	if err := s.queries.DeleteForgeReviewCommentsExcept(ctx, models.DeleteForgeReviewCommentsExceptParams{
		ThreadID: params.ThreadID,
		Kept:     xslice.Map(params.Comments, func(comment forge.ReviewComment) string { return comment.ProviderID }),
	}); err != nil {
		return fmt.Errorf("failed to remove the comments of forge thread %s: %w", params.ThreadID, err)
	}

	return nil
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
	stored := map[string]time.Time{}
	for _, row := range comments {
		stored[row.ThreadID] = latest(stored[row.ThreadID], row.CreatedAt)
		byThread[row.ThreadID] = append(byThread[row.ThreadID], ForgeComment{
			ID:          row.ID,
			ProviderID:  row.ProviderID,
			AuthorLogin: row.AuthorLogin,
			AuthorIsBot: row.AuthorIsBot,
			Yours:       row.Yours,
			Body:        row.Body,
			WrittenAt:   row.WrittenAt,
			EditedAt:    row.EditedAt,
		})
	}

	return xslice.Map(rows, func(row *models.ForgeReviewThread) ForgeThread {
		return threadFromRow(row, byThread[row.ID], stored[row.ID])
	}), nil
}

func toThreadUpsert(pullRequestID string, thread forge.ReviewThread) models.UpsertForgeReviewThreadParams {
	params := models.UpsertForgeReviewThreadParams{
		PullRequestID: pullRequestID,
		ProviderID:    thread.ProviderID,
		Path:          thread.Path,
		OldSide:       thread.OldSide,
		CommitSha:     thread.CommitSHA,
		Quote:         thread.Quote,
		Url:           thread.URL,
		Resolved:      thread.Resolved,
	}
	if thread.Line > 0 {
		params.Line = xptr.Ptr(int32(thread.Line))
	}
	if len(thread.Comments) > 0 {
		params.WrittenAt = thread.Comments[0].WrittenAt
	}

	return params
}

func threadFromRow(row *models.ForgeReviewThread, comments []ForgeComment, stored time.Time) ForgeThread {
	return ForgeThread{
		ID:            row.ID,
		PullRequestID: row.PullRequestID,
		ProviderID:    row.ProviderID,
		Path:          row.Path,
		Line:          int(xptr.Deref(row.Line)),
		OldSide:       row.OldSide,
		CommitSHA:     row.CommitSha,
		Quote:         row.Quote,
		URL:           row.Url,
		Resolved:      row.Resolved,
		WrittenAt:     row.WrittenAt,
		Dismissed:     row.DismissedAt != nil && !stored.After(*row.DismissedAt),
		Comments:      comments,
	}
}

// latest is the later of two times.
func latest(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}

	return a
}

// MarkDismissed stamps a forge thread dismissed, so the review stops counting it until a comment is written on it after now, or ErrNotFound.
func (s *ForgeThreadStore) MarkDismissed(ctx context.Context, id string) error {
	if !store.IsID(id) {
		return store.ErrNotFound.Saying("no forge thread %s", id)
	}

	_, err := s.queries.DismissForgeReviewThread(ctx, id)
	if db.IsNoRows(err) {
		return store.ErrNotFound.Saying("no forge thread %s", id)
	}
	if err != nil {
		return fmt.Errorf("failed to dismiss forge thread %s: %w", id, err)
	}

	return nil
}
