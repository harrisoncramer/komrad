package pullrequests

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"komrad/internal/events"
	"komrad/internal/mail"
	"komrad/internal/store"
	mailstore "komrad/internal/store/mail"
	pullrequeststore "komrad/internal/store/pullrequest"
	"komrad/lib/xptr"

	"github.com/riverqueue/river"
)

// NewEventHandler builds the handler the router is given for pull request changes.
func NewEventHandler() events.HandlerFor[Change] {
	return handleChange
}

// handleChange tells every agent on the pull request's branch where it now stands.
func handleChange(ctx context.Context, tx store.DBTX, mailer *mail.MailService, change Change) ([]river.JobArgs, error) {
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
	started, holding := eventsOf(change, *pullRequest)
	for _, recipient := range recipients {
		err := events.Send(ctx, mailer, mail.SendFromKomradParams{
			ToAgentID:   recipient,
			Kind:        mailstore.KindPullRequestChanged,
			SubjectID:   pullRequest.ID,
			Body:        body,
			OnlyRewrite: !change.Opens(),
			Events:      started,
			Holding:     holding,
		})
		if errors.Is(err, mail.ErrMailboxFull) && change.Recipient == "" {
			retry, err := events.NewEvent(Change{
				ID:        change.ID,
				Moves:     change.Moves,
				Recipient: recipient,
			})
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
		FieldAutoMerge:    xptr.Deref(autoMergeWord(pullRequest.AutoMerge)),
	}

	var body strings.Builder
	fmt.Fprintf(&body, "komrad: pull request #%d (%s) on %s changed. It stands at:\n", pullRequest.Number, pullRequest.URL, pullRequest.HeadBranch)
	for _, field := range fieldOrder {
		fmt.Fprintf(&body, "  %s: %s\n", field, describe(field, now[field], pullRequest))
	}
	fmt.Fprintf(&body, "Read its checks with gh pr checks %d. Review comments reach you through the person's review in komrad, so do not fetch them from the forge.", pullRequest.Number)

	return body.String()
}

func describe(field Field, value string, pullRequest pullrequeststore.PullRequest) string {
	switch {
	case field == FieldState:
		return strings.ToLower(value)
	case field == FieldMergeability && (value == "conflicting" || value == "behind"):
		return value + " with " + pullRequest.BaseBranch
	case field == FieldAutoMerge && value == autoMergeOn:
		return "on, so whatever is on the branch merges once its checks and reviews pass"
	case value == "":
		return "unknown"
	default:
		return strings.ReplaceAll(value, "_", " ")
	}
}
