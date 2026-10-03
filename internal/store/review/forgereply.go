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
	// errProposalOpen is a second agent proposal on a thread whose first has not reached the forge.
	errProposalOpen = fail.AlreadyExists("the agent already has a reply on that thread that has not reached the forge")
	// errReplyState is a forge reply asked to move in a way its state does not allow.
	errReplyState = fail.Precondition("the forge reply is not in a state that allows that")
)

// ForgeReplyState is where a forge reply is on its way to the forge.
type ForgeReplyState string

const (
	// ForgeReplyDraft is an agent's proposal waiting for the person.
	ForgeReplyDraft ForgeReplyState = ForgeReplyState(models.ForgeReplyStateDraft)
	// ForgeReplyApproved is a reply waiting to be posted.
	ForgeReplyApproved ForgeReplyState = ForgeReplyState(models.ForgeReplyStateApproved)
	// ForgeReplySending is a reply being posted, which may already be on the forge.
	ForgeReplySending ForgeReplyState = ForgeReplyState(models.ForgeReplyStateSending)
	// ForgeReplySent is a reply the forge accepted.
	ForgeReplySent ForgeReplyState = ForgeReplyState(models.ForgeReplyStateSent)
	// ForgeReplyFailed is a reply the forge refused on its latest attempt.
	ForgeReplyFailed ForgeReplyState = ForgeReplyState(models.ForgeReplyStateFailed)
)

// ForgeReplyApproverKind is who approved a forge reply for posting.
type ForgeReplyApproverKind string

// ForgeReplyApproverHuman is the person approving a reply.
const ForgeReplyApproverHuman ForgeReplyApproverKind = ForgeReplyApproverKind(models.ForgeReplyApproverKindHuman)

// ForgeReply is a reply komrad posts to the forge on a mirrored thread.
type ForgeReply struct {
	ID            string
	ThreadID      string
	State         ForgeReplyState
	AuthorKind    AuthorKind
	AuthorAgentID *string
	// ProposedBody is the agent's text as it proposed it, nil on a reply the person wrote.
	ProposedBody *string
	// Body is the text to post, and once sent the text the forge received.
	Body string
	// ApproverKind is empty while the reply is a draft.
	ApproverKind     ForgeReplyApproverKind
	ApprovedAt       *time.Time
	SendingStartedAt *time.Time
	SentAt           *time.Time
	// ForgeCommentProviderID is the forge's own id for the comment the reply became.
	ForgeCommentProviderID *string
	// Error is what the forge said when the latest attempt failed.
	Error     *string
	CreatedAt time.Time
}

// CreateAgentForgeReplyParams is an agent's proposed reply on a mirrored thread.
type CreateAgentForgeReplyParams struct {
	ThreadID string
	AgentID  string
	Body     string
}

// CreateAgentForgeReply drafts the agent's reply for the person to approve.
func (s *ReviewStore) CreateAgentForgeReply(ctx context.Context, params CreateAgentForgeReplyParams) (*ForgeReply, error) {
	if !store.IsID(params.ThreadID) {
		return nil, notMirrored(params.ThreadID)
	}

	row, err := s.queries.InsertAgentForgeReply(ctx, models.InsertAgentForgeReplyParams{
		ThreadID:      params.ThreadID,
		AuthorAgentID: params.AgentID,
		Body:          params.Body,
	})
	if db.IsNoRows(err) {
		return nil, notMirrored(params.ThreadID)
	}
	if db.IsUniqueViolation(err) {
		return nil, fmt.Errorf("%w: review thread %q", errProposalOpen, params.ThreadID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to propose a forge reply on review thread %q: %w", params.ThreadID, err)
	}

	return forgeReplyFromRow(row), nil
}

// CreateHumanForgeReply records the person's own reply, approved as it is written.
func (s *ReviewStore) CreateHumanForgeReply(ctx context.Context, threadID, body string) (*ForgeReply, error) {
	if !store.IsID(threadID) {
		return nil, notMirrored(threadID)
	}

	row, err := s.queries.InsertHumanForgeReply(ctx, models.InsertHumanForgeReplyParams{
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

// MarkForgeReplyApprovedParams is an approval of a draft, with the text to post.
type MarkForgeReplyApprovedParams struct {
	ID string
	// Body is the text to post, which may differ from the agent's proposal.
	Body         string
	ApproverKind ForgeReplyApproverKind
}

// MarkForgeReplyApproved approves a draft for posting.
func (s *ReviewStore) MarkForgeReplyApproved(ctx context.Context, params MarkForgeReplyApprovedParams) (*ForgeReply, error) {
	if !store.IsID(params.ID) {
		return nil, forgeReplyNotFound(params.ID)
	}

	row, err := s.queries.ApproveForgeReply(ctx, models.ApproveForgeReplyParams{
		ID:           params.ID,
		Body:         params.Body,
		ApproverKind: models.ForgeReplyApproverKind(params.ApproverKind),
	})

	return s.moved(ctx, params.ID, row, err)
}

// BeginForgeReplySend marks an approved or failed reply as being posted.
func (s *ReviewStore) BeginForgeReplySend(ctx context.Context, id string) (*ForgeReply, error) {
	if !store.IsID(id) {
		return nil, forgeReplyNotFound(id)
	}

	row, err := s.queries.BeginForgeReplySend(ctx, id)

	return s.moved(ctx, id, row, err)
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

	return s.moved(ctx, id, row, err)
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

	return s.moved(ctx, id, row, err)
}

// DeleteForgeReply deletes a draft or failed reply.
func (s *ReviewStore) DeleteForgeReply(ctx context.Context, id string) error {
	if !store.IsID(id) {
		return forgeReplyNotFound(id)
	}

	deleted, err := s.queries.DiscardForgeReply(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to discard the forge reply %q: %w", id, err)
	}
	if deleted == 0 {
		return s.refused(ctx, id)
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

	return xslice.Map(rows, func(row *models.ReviewForgeReply) ForgeReply {
		return *forgeReplyFromRow(row)
	}), nil
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

func (s *ReviewStore) moved(ctx context.Context, id string, row *models.ReviewForgeReply, err error) (*ForgeReply, error) {
	if db.IsNoRows(err) {
		return nil, s.refused(ctx, id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to move the forge reply %q: %w", id, err)
	}

	return forgeReplyFromRow(row), nil
}

func (s *ReviewStore) refused(ctx context.Context, id string) error {
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
		AuthorKind:             AuthorKind(row.AuthorKind),
		AuthorAgentID:          row.AuthorAgentID,
		ProposedBody:           row.ProposedBody,
		Body:                   row.Body,
		ApproverKind:           ForgeReplyApproverKind(row.ApproverKind.ForgeReplyApproverKind),
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
