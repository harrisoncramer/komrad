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
	mailstore "komrad/internal/store/mail"
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
	ByPerson bool   `json:"by_person"`
	// ReviewerID is the agent that wrote the review, blank for a person or a forge reviewer.
	ReviewerID string              `json:"reviewer_id"`
	Verdict    reviewstore.Verdict `json:"verdict"`
	Comments   int                 `json:"comments"`
	Replies    int                 `json:"replies"`
	Reopens    int                 `json:"reopens"`
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
	AgentID    string `json:"agent_id"`
	ReviewerID string `json:"reviewer_id"`
}

// Topic names the handler that tells the reviewer.
func (answered) Topic() string {
	return answeredTopic
}

// NewReleasedHandler builds the handler the router is given for released reviews.
func NewReleasedHandler() events.HandlerFor[Released] {
	return handleReleased
}

// handleReleased tells the task's live writer and stamps the review with the mail that told it,
// then puts away a reviewer that approved.
func handleReleased(ctx context.Context, tx store.DBTX, mailer *mail.MailService, released Released) ([]river.JobArgs, error) {
	if err := tellWriter(ctx, tx, mailer, released); err != nil {
		return nil, err
	}

	if released.Verdict != reviewstore.VerdictApproved || released.ReviewerID == "" {
		return nil, nil
	}

	return []river.JobArgs{internaljobs.ArchiveAgentArgs{AgentID: released.ReviewerID}}, nil
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

	sent, err := events.Send(ctx, mailer, mail.SendFromKomradParams{
		ToAgentID: writer.ID,
		Body:      releaseMail(released),
		Kind:      mailstore.KindReviewActivity,
		SubjectID: released.ReviewID,
		Events:    []string{released.event()},
	})
	if err != nil || sent == nil {
		return err
	}

	return reviewstore.NewReviewStore(tx).SetDoorbell(ctx, released.ReviewID, sent.ID)
}

// event is the event the release starts.
func (r Released) event() string {
	switch {
	case r.ForgeLogin != "":
		return eventprompts.ReviewForgeCommented
	case r.ByPerson:
		return eventprompts.ReviewCommentedByPerson
	case r.Verdict == reviewstore.VerdictApproved:
		return eventprompts.ReviewApproved
	case r.Verdict == reviewstore.VerdictChangesRequested:
		return eventprompts.ReviewChangesRequested
	default:
		return eventprompts.ReviewCommented
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

	author, err := agentstore.NewAgentStore(tx).GetByID(ctx, answered.AgentID)
	if err != nil {
		return nil, err
	}

	sent, err := events.Send(ctx, mailer, mail.SendFromKomradParams{
		ToAgentID: answered.ReviewerID,
		Kind:      mailstore.KindReviewActivity,
		SubjectID: author.ID,
		Body:      answerMail(author.Name),
		Events:    []string{eventprompts.ReviewAnswered},
	})
	if err != nil || sent == nil {
		return nil, err
	}

	return []river.JobArgs{internaljobs.WakeAgentArgs{AgentID: answered.ReviewerID}}, nil
}
