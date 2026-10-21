package reviews

import (
	"context"
	"errors"
	"slices"

	"komrad/internal/eventprompts"
	"komrad/internal/events"
	"komrad/internal/mail"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	mailstore "komrad/internal/store/mail"
	reviewstore "komrad/internal/store/review"
	taskstore "komrad/internal/store/task"
	"komrad/lib/xslice"

	"github.com/jackc/pgx/v5"
)

// mailCommentParams is a person's comment and the thread it sits in.
type mailCommentParams struct {
	Thread reviewstore.Thread
	// Message carries the comment's id, body and suggestion.
	Message reviewstore.Message
	// Reply is whether the comment answers in a thread rather than opening it.
	Reply bool
	// ForgeLogin is the forge user whose comment the person passed on, blank for their own words.
	ForgeLogin string
}

// mailComment sends a person's comment, whole, to the task's writer in the transaction the comment was written in.
func (s *ReviewService) mailComment(ctx context.Context, tx pgx.Tx, params mailCommentParams) error {
	writer, err := agentstore.NewAgentStore(tx).GetTaskWriter(ctx, params.Thread.TaskID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}

	reviews := reviewstore.NewReviewStore(tx)
	snapshot, err := reviews.GetSnapshot(ctx, params.Thread.SnapshotID)
	if err != nil {
		return err
	}

	prompt := eventprompts.ReviewComment{
		ThreadID:   params.Thread.ID,
		Ordinal:    snapshot.Ordinal,
		FilePath:   params.Thread.FilePath,
		OldStart:   params.Thread.OldStart,
		OldEnd:     params.Thread.OldEnd,
		NewStart:   params.Thread.NewStart,
		NewEnd:     params.Thread.NewEnd,
		Suggestion: params.Message.Suggestion,
		Said:       params.Message.Body,
		Reply:      params.Reply,
		ForgeLogin: params.ForgeLogin,
	}
	if !params.Reply {
		prompt.QuotedLines = params.Thread.QuotedLines
	}

	sent, err := events.Send(ctx, s.mailer.InTx(tx), mail.SendFromKomradParams{
		ToAgentID: writer.ID,
		Mail:      prompt,
		SubjectID: params.Message.ID,
	})
	if err != nil || sent == nil {
		return err
	}

	return reviews.SetMessageMail(ctx, params.Message.ID, sent.ID)
}

// ReadMail claims an agent's waiting mail, taking the snapshot a waiting comment is read on first.
func (s *ReviewService) ReadMail(ctx context.Context, agentID string) ([]mailstore.Mail, error) {
	waiting, err := s.mailer.ListWaitingMail(ctx, agentID)
	if err != nil {
		return nil, err
	}

	var readSnapshotID *string
	if slices.ContainsFunc(waiting, isReviewComment) {
		snapshot, err := s.takeReadSnapshot(ctx, agentID)
		if err != nil && !errors.Is(err, ErrReviewNoSnapshot) {
			return nil, err
		}
		if snapshot != nil {
			readSnapshotID = &snapshot.ID
		}
	}

	return s.mailer.ClaimMail(ctx, mail.ClaimMailParams{
		AgentID:        agentID,
		IDs:            xslice.Map(waiting, func(m mailstore.Mail) string { return m.ID }),
		ReadSnapshotID: readSnapshotID,
	})
}

// isReviewComment says whether mail carries a person's comment on the recipient's work.
func isReviewComment(m mailstore.Mail) bool {
	return m.Kind == mailstore.KindReviewComment
}

// takeReadSnapshot freezes an agent's task as it reads a person's comment, which an addressed reply is measured from.
func (s *ReviewService) takeReadSnapshot(ctx context.Context, agentID string) (*reviewstore.Snapshot, error) {
	agent, err := s.getAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}

	task, err := s.tasks.GetLinked(ctx, agent.TaskID)
	if errors.Is(err, taskstore.ErrNoTask) {
		return nil, ErrReviewNoSnapshot
	}
	if err != nil {
		return nil, err
	}

	snapshot, err := s.takeTaskSnapshot(ctx, *task)
	if errors.Is(err, errReviewWorktreeRemoved) {
		return nil, ErrReviewNoSnapshot
	}

	return snapshot, err
}
