package review

import (
	"context"
	"fmt"
	"time"

	"komrad/db"
	"komrad/db/models"
	"komrad/internal/fail"
	"komrad/internal/store"
	"komrad/lib/xslice"
)

var (
	// errNotMirrored is a forge reply written on a thread that mirrors no forge thread.
	errNotMirrored = fail.Precondition("only a thread mirrored from the forge can be replied to there")
	// errReplyState is a forge reply asked to move in a way its state does not allow.
	errReplyState = fail.Precondition("the forge reply is not in a state that allows that")
)

// ForgeReplyState is where a forge reply is on its way to the forge.
type ForgeReplyState string

const (
	// ForgeReplyApproved is a reply waiting to be posted.
	ForgeReplyApproved ForgeReplyState = ForgeReplyState(models.ForgeReplyStateApproved)
	// ForgeReplySending is a reply being posted, which may already be on the forge.
	ForgeReplySending ForgeReplyState = ForgeReplyState(models.ForgeReplyStateSending)
	// ForgeReplySent is a reply the forge accepted.
	ForgeReplySent ForgeReplyState = ForgeReplyState(models.ForgeReplyStateSent)
	// ForgeReplyFailed is a reply the forge refused on its latest attempt.
	ForgeReplyFailed ForgeReplyState = ForgeReplyState(models.ForgeReplyStateFailed)
)

// ForgeReply is a reply komrad posts to the forge on a mirrored thread.
type ForgeReply struct {
	ID                     string
	ThreadID               string
	State                  ForgeReplyState
	Body                   string
	ApprovedAt             *time.Time
	SendingStartedAt       *time.Time
	SentAt                 *time.Time
	ForgeCommentProviderID *string
	MirroredCommentID      *string
	Error                  *string
	CreatedAt              time.Time
}

// ForgeReplyTarget is the forge thread a review thread mirrors and the pull request it is on.
type ForgeReplyTarget struct {
	ProviderID    string
	Path          string
	RepositoryURL string
	Number        int
}

// CreateForgeReply records the person's reply, ready to post.
func (s *ReviewStore) CreateForgeReply(ctx context.Context, threadID, body string) (*ForgeReply, error) {
	if !store.IsID(threadID) {
		return nil, notMirrored(threadID)
	}

	row, err := s.queries.InsertForgeReply(ctx, models.InsertForgeReplyParams{
		ThreadID: threadID,
		Body:     body,
	})
	if db.IsNoRows(err) {
		return nil, notMirrored(threadID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to write a forge reply on review thread %q: %w", threadID, err)
	}

	return forgeReplyFromRow(row), nil
}

// BeginForgeReplySend marks an approved or failed reply as being posted.
func (s *ReviewStore) BeginForgeReplySend(ctx context.Context, id string) (*ForgeReply, error) {
	if !store.IsID(id) {
		return nil, forgeReplyNotFound(id)
	}

	row, err := s.queries.BeginForgeReplySend(ctx, id)

	return s.finishForgeReplyMove(ctx, id, row, err)
}

// MarkForgeReplySent records the comment the forge made of a reply being posted.
func (s *ReviewStore) MarkForgeReplySent(ctx context.Context, id, forgeCommentProviderID string) (*ForgeReply, error) {
	if !store.IsID(id) {
		return nil, forgeReplyNotFound(id)
	}

	row, err := s.queries.MarkForgeReplySent(ctx, models.MarkForgeReplySentParams{
		ID:                     id,
		ForgeCommentProviderID: forgeCommentProviderID,
	})

	return s.finishForgeReplyMove(ctx, id, row, err)
}

// MarkForgeReplyFailed records what the forge said when it refused a reply being posted.
func (s *ReviewStore) MarkForgeReplyFailed(ctx context.Context, id, forgeError string) (*ForgeReply, error) {
	if !store.IsID(id) {
		return nil, forgeReplyNotFound(id)
	}

	row, err := s.queries.MarkForgeReplyFailed(ctx, models.MarkForgeReplyFailedParams{
		ID:    id,
		Error: forgeError,
	})

	return s.finishForgeReplyMove(ctx, id, row, err)
}

// DeleteForgeReply deletes a failed reply.
func (s *ReviewStore) DeleteForgeReply(ctx context.Context, id string) error {
	if !store.IsID(id) {
		return forgeReplyNotFound(id)
	}

	deleted, err := s.queries.DeleteForgeReply(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to delete the forge reply %q: %w", id, err)
	}
	if deleted == 0 {
		return s.explainForgeReplyRefusal(ctx, id)
	}

	return nil
}

// GetForgeReply reads one forge reply.
func (s *ReviewStore) GetForgeReply(ctx context.Context, id string) (*ForgeReply, error) {
	if !store.IsID(id) {
		return nil, forgeReplyNotFound(id)
	}

	row, err := s.queries.GetForgeReply(ctx, id)
	if db.IsNoRows(err) {
		return nil, forgeReplyNotFound(id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read the forge reply %q: %w", id, err)
	}

	return forgeReplyFromRow(row), nil
}

// ListForgeReplies is every forge reply on the given threads, oldest first within each.
func (s *ReviewStore) ListForgeReplies(ctx context.Context, threadIDs []string) ([]ForgeReply, error) {
	if len(threadIDs) == 0 {
		return nil, nil
	}

	rows, err := s.queries.ListForgeReplies(ctx, threadIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to list the forge replies of %d review thread(s): %w", len(threadIDs), err)
	}

	return xslice.Map(rows, listedForgeReplyFromRow), nil
}

// ListSendingForgeReplies is every reply whose posting was begun and never settled.
func (s *ReviewStore) ListSendingForgeReplies(ctx context.Context) ([]ForgeReply, error) {
	rows, err := s.queries.ListSendingForgeReplies(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the forge replies being sent: %w", err)
	}

	return xslice.Map(rows, func(row *models.ReviewForgeReply) ForgeReply {
		return *forgeReplyFromRow(row)
	}), nil
}

// GetForgeReplyTarget returns where a reply on a mirrored review thread goes on the forge.
func (s *ReviewStore) GetForgeReplyTarget(ctx context.Context, threadID string) (*ForgeReplyTarget, error) {
	if !store.IsID(threadID) {
		return nil, notMirrored(threadID)
	}

	row, err := s.queries.GetForgeReplyTarget(ctx, threadID)
	if db.IsNoRows(err) {
		return nil, notMirrored(threadID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to find the forge thread review thread %q mirrors: %w", threadID, err)
	}

	return &ForgeReplyTarget{
		ProviderID:    row.ProviderID,
		Path:          row.Path,
		RepositoryURL: row.RepositoryUrl,
		Number:        int(row.Number),
	}, nil
}

func (s *ReviewStore) finishForgeReplyMove(ctx context.Context, id string, row *models.ReviewForgeReply, err error) (*ForgeReply, error) {
	if db.IsNoRows(err) {
		return nil, s.explainForgeReplyRefusal(ctx, id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to move the forge reply %q: %w", id, err)
	}

	return forgeReplyFromRow(row), nil
}

func (s *ReviewStore) explainForgeReplyRefusal(ctx context.Context, id string) error {
	reply, err := s.GetForgeReply(ctx, id)
	if err != nil {
		return err
	}

	return fmt.Errorf("%w: forge reply %q is %s", errReplyState, id, reply.State)
}

func forgeReplyFromRow(row *models.ReviewForgeReply) *ForgeReply {
	return &ForgeReply{
		ID:                     row.ID,
		ThreadID:               row.ThreadID,
		State:                  ForgeReplyState(row.State),
		Body:                   row.Body,
		ApprovedAt:             row.ApprovedAt,
		SendingStartedAt:       row.SendingStartedAt,
		SentAt:                 row.SentAt,
		ForgeCommentProviderID: row.ForgeCommentProviderID,
		Error:                  row.Error,
		CreatedAt:              row.CreatedAt,
	}
}

func notMirrored(threadID string) error {
	return fmt.Errorf("%w: review thread %q", errNotMirrored, threadID)
}

func forgeReplyNotFound(id string) error {
	return fmt.Errorf("%w: forge reply %q", store.ErrNotFound, id)
}

func listedForgeReplyFromRow(row *models.ListForgeRepliesRow) ForgeReply {
	reply := forgeReplyFromRow(&row.ReviewForgeReply)
	reply.MirroredCommentID = row.MirroredCommentID

	return *reply
}
