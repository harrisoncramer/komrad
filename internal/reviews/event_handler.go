package reviews

import (
	"context"
	"errors"
	"slices"

	"komrad/internal/eventprompts"
	"komrad/internal/events"
	"komrad/internal/jobs/internaljobs"
	"komrad/internal/mail"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	reviewstore "komrad/internal/store/review"

	"github.com/riverqueue/river"
)

const (
	releasedTopic = "review_released"
	answeredTopic = "review_answered"
)

// Released is a review released on a task's work.
type Released struct {
	ReviewID string `json:"review_id"`
	TaskID   string `json:"task_id"`
	Ordinal  int32  `json:"ordinal"`
	// ReviewerID is the agent that wrote the review, blank for a forge reviewer.
	ReviewerID string              `json:"reviewer_id"`
	Verdict    reviewstore.Verdict `json:"verdict"`
	Comments   int                 `json:"comments"`
	Replies    int                 `json:"replies"`
	Path       string              `json:"path"`
	HasBody    bool                `json:"has_body"`
	// ForgeLogin is the forge reviewer whose mirrored comments were released, blank for a review
	// written in komrad.
	ForgeLogin string `json:"forge_login"`
}

// Topic names the handler that tells the task's writer.
func (Released) Topic() string {
	return releasedTopic
}

// answered is a task's writer answering a thread a reviewer opened on its work.
type answered struct {
	TaskID     string `json:"task_id"`
	WriterID   string `json:"writer_id"`
	ReviewerID string `json:"reviewer_id"`
}

// Topic names the handler that tells the reviewer.
func (answered) Topic() string {
	return answeredTopic
}

// NewReleasedHandlerParams is what the handler for released reviews is built from.
type NewReleasedHandlerParams struct {
	ArchiveOnApproval bool
}

// NewReleasedHandler builds the handler that tells a task's live writer of a released review and
// archives a reviewer that approved when ArchiveOnApproval is set.
func NewReleasedHandler(params NewReleasedHandlerParams) events.HandlerFor[Released] {
	return func(ctx context.Context, tx store.DBTX, mailer *mail.MailService, released Released) ([]river.JobArgs, error) {
		if err := tellWriter(ctx, tx, mailer, released); err != nil {
			return nil, err
		}

		if !params.ArchiveOnApproval || released.Verdict != reviewstore.VerdictApproved || released.ReviewerID == "" {
			return nil, nil
		}

		return []river.JobArgs{internaljobs.ArchiveAgentArgs{AgentID: released.ReviewerID}}, nil
	}
}

// tellWriter mails the task's live writer about the release, sending nothing when the task has
// no writer.
func tellWriter(ctx context.Context, tx store.DBTX, mailer *mail.MailService, released Released) error {
	writer, err := agentstore.NewAgentStore(tx).GetTaskWriter(ctx, released.TaskID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}

	_, err = events.Send(ctx, mailer, mail.SendFromKomradParams{
		ToAgentID: writer.ID,
		Mail:      released.mail(),
		SubjectID: released.ReviewID,
	})

	return err
}

// mail is what the writer is told of the release.
func (r Released) mail() eventprompts.ReviewReleased {
	return eventprompts.ReviewReleased{
		ReviewID:   r.ReviewID,
		Ordinal:    r.Ordinal,
		Verdict:    r.Verdict,
		Comments:   r.Comments,
		Replies:    r.Replies,
		Path:       r.Path,
		HasBody:    r.HasBody,
		ForgeLogin: r.ForgeLogin,
	}
}

// NewAnsweredHandler builds the handler the router is given for answers to a review.
func NewAnsweredHandler() events.HandlerFor[answered] {
	return handleAnswered
}

// handleAnswered tells the reviewer whose thread was answered and asks for it to be woken, unless
// it has approved or gone.
func handleAnswered(ctx context.Context, tx store.DBTX, mailer *mail.MailService, answered answered) ([]river.JobArgs, error) {
	live, err := reviewstore.NewReviewStore(tx).ListLiveReviewers(ctx, answered.TaskID)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(live, answered.ReviewerID) {
		return nil, nil
	}

	writer, err := agentstore.NewAgentStore(tx).GetByID(ctx, answered.WriterID)
	if err != nil {
		return nil, err
	}

	sent, err := events.Send(ctx, mailer, mail.SendFromKomradParams{
		ToAgentID: answered.ReviewerID,
		Mail:      eventprompts.ThreadAnswered{WriterName: writer.Name},
		SubjectID: writer.ID,
	})
	if err != nil || sent == nil {
		return nil, err
	}

	return []river.JobArgs{internaljobs.WakeAgentArgs{AgentID: answered.ReviewerID}}, nil
}
