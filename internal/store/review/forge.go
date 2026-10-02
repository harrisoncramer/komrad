package review

import (
	"context"
	"fmt"
	"time"

	"komrad/db/models"
	"komrad/internal/store"
	"komrad/lib/xslice"
)

// OpenForgeReviewParams is one forge reviewer's comments from one sync, gathered into a review.
type OpenForgeReviewParams struct {
	AgentID string
	RoundID string
	// Login is the forge login of whoever wrote the comments.
	Login string
}

// OpenForgeReview starts a review holding comments mirrored from the forge.
func (s *ReviewStore) OpenForgeReview(ctx context.Context, params OpenForgeReviewParams) (*Review, error) {
	row, err := s.queries.InsertForgeReview(ctx, models.InsertForgeReviewParams{
		AgentID:       params.AgentID,
		RoundID:       params.RoundID,
		ExternalLogin: &params.Login,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open a forge review on agent %q: %w", params.AgentID, err)
	}

	review := reviewFromRow(*row)

	return &review, nil
}

// UpdateForgeMessageParams is a forge comment's edit.
type UpdateForgeMessageParams struct {
	ID       string
	Body     string
	EditedAt *time.Time
	// Held is whether the author could not write to the repository when they made the edit.
	Held bool
}

// UpdateForgeMessage rewrites a mirrored comment the forge reports was edited.
func (s *ReviewStore) UpdateForgeMessage(ctx context.Context, params UpdateForgeMessageParams) error {
	err := s.queries.UpdateForgeReviewMessage(ctx, models.UpdateForgeReviewMessageParams{
		ID:            params.ID,
		Body:          params.Body,
		ForgeEditedAt: params.EditedAt,
		Held:          params.Held,
	})
	if err != nil {
		return fmt.Errorf("failed to update the forge message %q: %w", params.ID, err)
	}

	return nil
}

// SetForgeResolved records whether the forge last said a mirrored thread was resolved.
func (s *ReviewStore) SetForgeResolved(ctx context.Context, threadID string, resolved bool) error {
	err := s.queries.SetForgeThreadResolved(ctx, models.SetForgeThreadResolvedParams{
		ID:            threadID,
		ForgeResolved: resolved,
	})
	if err != nil {
		return fmt.Errorf("failed to record the forge's resolution of thread %q: %w", threadID, err)
	}

	return nil
}

// ForgeMirrored is a thread mirrored from the forge with one of the forge comments in it.
type ForgeMirrored struct {
	ThreadID       string
	ForgeThreadID  string
	ResolvedAt     *time.Time
	ResolvedByKind AuthorKind
	// ForgeResolved is whether the forge said the thread was resolved as of the last sync.
	ForgeResolved bool
	// MessageID is nil on a thread whose forge comments have all been deleted.
	MessageID      *string
	ForgeCommentID *string
	ForgeEditedAt  *time.Time
}

// ListForgeMirrored is every forge comment already mirrored onto an agent, by thread.
func (s *ReviewStore) ListForgeMirrored(ctx context.Context, agentID string) ([]ForgeMirrored, error) {
	if !store.IsID(agentID) {
		return nil, nil
	}

	rows, err := s.queries.ListForgeMirroredMessages(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to list the forge comments mirrored onto agent %q: %w", agentID, err)
	}

	return xslice.Map(rows, func(row *models.ListForgeMirroredMessagesRow) ForgeMirrored {
		return ForgeMirrored{
			ThreadID:       row.ThreadID,
			ForgeThreadID:  row.ForgeThreadID,
			ResolvedAt:     row.ResolvedAt,
			ResolvedByKind: AuthorKind(row.ResolvedByKind.ReviewAuthorKind),
			ForgeResolved:  row.ForgeResolved,
			MessageID:      row.MessageID,
			ForgeCommentID: row.ForgeCommentID,
			ForgeEditedAt:  row.ForgeEditedAt,
		}
	}), nil
}

// ForgeMirrorTarget is an agent on a pull request that forge comments are mirrored onto.
type ForgeMirrorTarget struct {
	AgentID string
	// Cwd is the agent's checkout, where comments are traced to its latest round.
	Cwd         string
	RoundID     string
	BaseSHA     string
	SnapshotSHA string
}

// ListForgeMirrorTargets is every live agent on a pull request that has a round to mirror onto.
func (s *ReviewStore) ListForgeMirrorTargets(ctx context.Context, pullRequestID string) ([]ForgeMirrorTarget, error) {
	if !store.IsID(pullRequestID) {
		return nil, nil
	}

	rows, err := s.queries.ListForgeMirrorAgents(ctx, pullRequestID)
	if err != nil {
		return nil, fmt.Errorf("failed to list the agents on pull request %q: %w", pullRequestID, err)
	}

	return xslice.Map(rows, func(row *models.ListForgeMirrorAgentsRow) ForgeMirrorTarget {
		return ForgeMirrorTarget{
			AgentID:     row.AgentID,
			Cwd:         row.Cwd,
			RoundID:     row.RoundID,
			BaseSHA:     row.BaseSha,
			SnapshotSHA: row.SnapshotSha,
		}
	}), nil
}
