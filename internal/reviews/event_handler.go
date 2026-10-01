package reviews

import (
	"context"
	"errors"

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

// Released is a review released on an agent's work.
type Released struct {
	ReviewID string              `json:"review_id"`
	AgentID  string              `json:"agent_id"`
	Ordinal  int32               `json:"ordinal"`
	ByPerson bool                `json:"by_person"`
	Verdict  reviewstore.Verdict `json:"verdict"`
	Comments int                 `json:"comments"`
	Replies  int                 `json:"replies"`
	Reopens  int                 `json:"reopens"`
	Path     string              `json:"path"`
}

// Topic names the handler that tells the agent under review.
func (Released) Topic() string {
	return releasedTopic
}

// Answered is an agent answering in a review of its own work.
type Answered struct {
	AgentID string `json:"agent_id"`
}

// Topic names the handler that tells the agent's reviewer.
func (Answered) Topic() string {
	return answeredTopic
}

// NewReleasedHandler builds the handler the router is given for released reviews.
func NewReleasedHandler() events.HandlerFor[Released] {
	return handleReleased
}

// handleReleased tells the agent under review and stamps the review with the mail that told it.
func handleReleased(ctx context.Context, tx store.DBTX, mailer *mail.MailService, released Released) ([]river.JobArgs, error) {
	sent, err := mailer.SendFromKomrad(ctx, mail.SendFromKomradParams{
		ToAgentID: released.AgentID,
		Body:      releaseMail(released),
		Kind:      mailstore.KindReviewActivity,
		SubjectID: released.ReviewID,
		Events:    []string{released.event()},
	})
	if err != nil {
		return nil, events.DropUnreachable(ctx, released.AgentID, err)
	}

	return nil, reviewstore.NewReviewStore(tx).SetDoorbell(ctx, released.ReviewID, sent.ID)
}

// event is the event the release starts.
func (r Released) event() string {
	switch {
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
func NewAnsweredHandler() events.HandlerFor[Answered] {
	return handleAnswered
}

// handleAnswered tells the author's live reviewer and asks for it to be woken.
func handleAnswered(ctx context.Context, tx store.DBTX, mailer *mail.MailService, answered Answered) ([]river.JobArgs, error) {
	reading, err := reviewstore.NewReviewStore(tx).GetLiveReviewerOf(ctx, answered.AgentID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	author, err := agentstore.NewAgentStore(tx).GetByID(ctx, answered.AgentID)
	if err != nil {
		return nil, err
	}

	err = events.Send(ctx, mailer, mail.SendFromKomradParams{
		ToAgentID: reading.ReviewerID,
		Kind:      mailstore.KindReviewActivity,
		SubjectID: author.ID,
		Body:      answerMail(author.Name),
		Events:    []string{eventprompts.ReviewAnswered},
	})
	if err != nil {
		return nil, err
	}

	return []river.JobArgs{internaljobs.WakeAgentArgs{AgentID: reading.ReviewerID}}, nil
}
