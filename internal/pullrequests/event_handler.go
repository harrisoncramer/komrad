package pullrequests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"komrad/internal/events"
	"komrad/internal/mail"
	"komrad/internal/store"
	mailstore "komrad/internal/store/mail"
	pullrequeststore "komrad/internal/store/pullrequest"

	"github.com/riverqueue/river"
)

// EventHandler keeps one unread mail per agent on a pull request's branch saying where it stands.
type EventHandler struct{}

// NewEventHandler builds the handler the router is given for pull request changes.
func NewEventHandler() *EventHandler {
	return &EventHandler{}
}

// Topic is the topic of a Change.
func (*EventHandler) Topic() string {
	return changeTopic
}

// Handle tells every agent on the pull request's branch where it now stands.
func (*EventHandler) Handle(ctx context.Context, tx store.DBTX, mailer *mail.MailService, payload json.RawMessage) ([]river.JobArgs, error) {
	var change Change
	if err := json.Unmarshal(payload, &change); err != nil {
		return nil, fmt.Errorf("failed to read a pull request change: %w", err)
	}

	pullRequests := pullrequeststore.NewPullRequestStore(tx)
	pullRequest, err := pullRequests.GetByID(ctx, change.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	recipients, err := pullRequests.ListRecipients(ctx, pullRequest.ID)
	if err != nil {
		return nil, err
	}

	if change.Recipient != "" {
		recipients = slices.DeleteFunc(recipients, func(recipient string) bool { return recipient != change.Recipient })
	}

	var followUps []river.JobArgs
	body := render(*pullRequest)
	for _, recipient := range recipients {
		err := events.Send(ctx, mailer, mail.SendFromKomradParams{
			ToAgentID:   recipient,
			Kind:        mailstore.KindPullRequestChanged,
			SubjectID:   pullRequest.ID,
			Body:        body,
			OnlyRewrite: !change.Opens(),
		})
		if errors.Is(err, mail.ErrMailboxFull) && change.Recipient == "" {
			retry, err := events.NewEvent(Change{ID: change.ID, Moves: change.Moves, Recipient: recipient})
			if err != nil {
				return nil, err
			}

			followUps = append(followUps, *retry)

			continue
		}
		if err != nil {
			return nil, err
		}
	}

	return followUps, nil
}

func render(pullRequest pullrequeststore.PullRequest) string {
	now := map[Field]string{
		FieldState:        pullRequest.State,
		FieldDecision:     pullRequest.LastKnownDecision,
		FieldChecks:       pullRequest.LastKnownChecks,
		FieldMergeability: pullRequest.LastKnownMergeability,
	}

	var body strings.Builder
	fmt.Fprintf(&body, "komrad: pull request #%d (%s) on %s changed. It stands at:\n", pullRequest.Number, pullRequest.URL, pullRequest.HeadBranch)
	for _, field := range fieldOrder {
		fmt.Fprintf(&body, "  %s: %s\n", field, describe(field, now[field], pullRequest))
	}
	fmt.Fprintf(&body, "Read it with gh pr view %d and gh pr checks %d.", pullRequest.Number, pullRequest.Number)

	return body.String()
}

func describe(field Field, value string, pullRequest pullrequeststore.PullRequest) string {
	switch {
	case field == FieldState:
		return strings.ToLower(value)
	case field == FieldMergeability && (value == "conflicting" || value == "behind"):
		return value + " with " + pullRequest.BaseBranch
	case value == "":
		return "unknown"
	default:
		return strings.ReplaceAll(value, "_", " ")
	}
}
