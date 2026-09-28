package reviews

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"komrad/internal/jobs/internaljobs"
	"komrad/internal/mail"
	"komrad/internal/notices"
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

// ReleaseNoticeHandler tells an agent a review of its work was released and records that mail on the review.
type ReleaseNoticeHandler struct{}

// NewReleaseNoticeHandler builds the handler the router is given for released reviews.
func NewReleaseNoticeHandler() *ReleaseNoticeHandler {
	return &ReleaseNoticeHandler{}
}

// Topic is the topic of Released.
func (*ReleaseNoticeHandler) Topic() string {
	return releasedTopic
}

// Handle tells the agent under review and stamps the review with the mail that told it.
func (*ReleaseNoticeHandler) Handle(ctx context.Context, tx store.DBTX, mailer *mail.MailService, payload json.RawMessage) ([]river.JobArgs, error) {
	var released Released
	if err := json.Unmarshal(payload, &released); err != nil {
		return nil, fmt.Errorf("failed to read a released review: %w", err)
	}

	sent, err := mailer.SendNotice(ctx, mail.SendNoticeParams{
		ToAgentID: released.AgentID,
		Body:      reviewNotice(released),
		Kind:      mailstore.KindReviewActivity,
		SubjectID: released.ReviewID,
	})
	if err != nil {
		return nil, notices.DropUnreachable(ctx, released.AgentID, err)
	}

	return nil, reviewstore.NewReviewStore(tx).SetDoorbell(ctx, released.ReviewID, sent.ID)
}

// AnswerNoticeHandler keeps one unread notice per author telling its reviewer it answered, and wakes the reviewer.
type AnswerNoticeHandler struct{}

// NewAnswerNoticeHandler builds the handler the router is given for answers to a review.
func NewAnswerNoticeHandler() *AnswerNoticeHandler {
	return &AnswerNoticeHandler{}
}

// Topic is the topic of Answered.
func (*AnswerNoticeHandler) Topic() string {
	return answeredTopic
}

// Handle tells the author's live reviewer and asks for it to be woken.
func (*AnswerNoticeHandler) Handle(ctx context.Context, tx store.DBTX, mailer *mail.MailService, payload json.RawMessage) ([]river.JobArgs, error) {
	var answered Answered
	if err := json.Unmarshal(payload, &answered); err != nil {
		return nil, fmt.Errorf("failed to read an answer to a review: %w", err)
	}

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

	err = notices.Write(ctx, mailer, notices.Notice{
		ToAgentID: reading.ReviewerID,
		Kind:      mailstore.KindReviewActivity,
		SubjectID: author.ID,
		Body:      answerNotice(author.Name),
	})
	if err != nil {
		return nil, err
	}

	return []river.JobArgs{internaljobs.WakeAgentArgs{AgentID: reading.ReviewerID}}, nil
}
