package reviews

import (
	"context"
	"errors"
	"fmt"

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
	releasedTopic            = "review_released"
	answeredTopic            = "review_answered"
	forgeReplyRequestedTopic = "review_forge_reply_requested"
	forgeReplyDecidedTopic   = "review_forge_reply_decided"
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
	HasBody  bool                `json:"has_body"`
	// ForgeLogin is the forge reviewer whose mirrored comments were released, blank for a review
	// written in komrad.
	ForgeLogin string `json:"forge_login"`
}

// Topic names the handler that tells the agent under review.
func (Released) Topic() string {
	return releasedTopic
}

// answered is an agent answering in a review of its own work.
type answered struct {
	AgentID string `json:"agent_id"`
}

// Topic names the handler that tells the agent's reviewer.
func (answered) Topic() string {
	return answeredTopic
}

// NewReleasedHandler builds the handler the router is given for released reviews.
func NewReleasedHandler() events.HandlerFor[Released] {
	return handleReleased
}

// handleReleased tells the agent under review and stamps the review with the mail that told it.
func handleReleased(ctx context.Context, tx store.DBTX, mailer *mail.MailService, released Released) ([]river.JobArgs, error) {
	sent, err := events.Send(ctx, mailer, mail.SendFromKomradParams{
		ToAgentID: released.AgentID,
		Body:      releaseMail(released),
		Kind:      mailstore.KindReviewActivity,
		SubjectID: released.ReviewID,
		Events:    []string{released.event()},
	})
	if err != nil || sent == nil {
		return nil, err
	}

	return nil, reviewstore.NewReviewStore(tx).SetDoorbell(ctx, released.ReviewID, sent.ID)
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

// handleAnswered tells the author's live reviewer and asks for it to be woken.
func handleAnswered(ctx context.Context, tx store.DBTX, mailer *mail.MailService, answered answered) ([]river.JobArgs, error) {
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

	sent, err := events.Send(ctx, mailer, mail.SendFromKomradParams{
		ToAgentID: reading.ReviewerID,
		Kind:      mailstore.KindReviewActivity,
		SubjectID: author.ID,
		Body:      answerMail(author.Name),
		Events:    []string{eventprompts.ReviewAnswered},
	})
	if err != nil || sent == nil {
		return nil, err
	}

	return []river.JobArgs{internaljobs.WakeAgentArgs{AgentID: reading.ReviewerID}}, nil
}

// forgeReplyRequested is the person asking an agent to draft a reply on a mirrored thread.
type forgeReplyRequested struct {
	AgentID  string `json:"agent_id"`
	ThreadID string `json:"thread_id"`
}

// Topic names the handler that asks the agent for the draft.
func (forgeReplyRequested) Topic() string {
	return forgeReplyRequestedTopic
}

// NewForgeReplyRequestedHandler builds the handler the router is given for requested forge replies.
func NewForgeReplyRequestedHandler() events.HandlerFor[forgeReplyRequested] {
	return handleForgeReplyRequested
}

// handleForgeReplyRequested asks the agent for a draft and asks for it to be woken.
func handleForgeReplyRequested(ctx context.Context, _ store.DBTX, mailer *mail.MailService, requested forgeReplyRequested) ([]river.JobArgs, error) {
	sent, err := events.Send(ctx, mailer, mail.SendFromKomradParams{
		ToAgentID: requested.AgentID,
		Kind:      mailstore.KindReviewActivity,
		SubjectID: requested.ThreadID,
		Body:      forgeReplyRequestMail(requested.ThreadID),
		Events:    []string{eventprompts.ReviewForgeReplyRequested},
	})
	if err != nil || sent == nil {
		return nil, err
	}

	return []river.JobArgs{internaljobs.WakeAgentArgs{AgentID: requested.AgentID}}, nil
}

// forgeReplyRequestMail is the mail asking an agent to draft a forge reply.
func forgeReplyRequestMail(threadID string) string {
	return fmt.Sprintf("komrad: the person asks you to draft a reply on review thread %s, which mirrors a thread "+
		"on your pull request. Read it with list_review_threads and propose the reply with propose_forge_reply.", threadID)
}

// forgeReplyOutcome is what the person did with an agent's draft forge reply.
type forgeReplyOutcome string

const (
	// forgeReplySentAsWritten is a draft posted with the agent's words.
	forgeReplySentAsWritten forgeReplyOutcome = "sent"
	// forgeReplySentEdited is a draft posted after the person changed its words.
	forgeReplySentEdited forgeReplyOutcome = "edited"
	// forgeReplyDiscarded is a draft the person dropped without posting.
	forgeReplyDiscarded forgeReplyOutcome = "discarded"
)

// forgeReplyDecided is the person settling an agent's draft forge reply.
type forgeReplyDecided struct {
	AgentID  string            `json:"agent_id"`
	ThreadID string            `json:"thread_id"`
	ReplyID  string            `json:"reply_id"`
	Outcome  forgeReplyOutcome `json:"outcome"`
	Body     string            `json:"body"`
}

// Topic names the handler that tells the agent what became of its draft.
func (forgeReplyDecided) Topic() string {
	return forgeReplyDecidedTopic
}

// NewForgeReplyDecidedHandler builds the handler the router is given for settled forge replies.
func NewForgeReplyDecidedHandler() events.HandlerFor[forgeReplyDecided] {
	return handleForgeReplyDecided
}

// handleForgeReplyDecided tells the agent what the person did with its draft.
func handleForgeReplyDecided(ctx context.Context, _ store.DBTX, mailer *mail.MailService, decided forgeReplyDecided) ([]river.JobArgs, error) {
	_, err := events.Send(ctx, mailer, mail.SendFromKomradParams{
		ToAgentID: decided.AgentID,
		Kind:      mailstore.KindReviewActivity,
		SubjectID: decided.ReplyID,
		Body:      decided.mail(),
		Events:    []string{eventprompts.ReviewForgeReplyDecided},
	})

	return nil, err
}

// mail is the mail telling the agent what became of its draft.
func (d forgeReplyDecided) mail() string {
	switch d.Outcome {
	case forgeReplySentAsWritten:
		return fmt.Sprintf("komrad: the person posted your draft reply on review thread %s to the forge as you wrote it.", d.ThreadID)
	case forgeReplySentEdited:
		return fmt.Sprintf("komrad: the person edited your draft reply on review thread %s and posted it to the forge. "+
			"What went out:\n\n%s", d.ThreadID, d.Body)
	case forgeReplyDiscarded:
		return fmt.Sprintf("komrad: the person discarded your draft reply on review thread %s. Nothing was posted to the forge.", d.ThreadID)
	default:
		return ""
	}
}
