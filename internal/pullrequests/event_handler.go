package pullrequests

import (
	"context"
	"errors"
	"slices"
	"time"

	"komrad/internal/eventmail"
	"komrad/internal/events"
	"komrad/internal/forge"
	"komrad/internal/mail"
	"komrad/internal/store"
	mailstore "komrad/internal/store/mail"
	pullrequeststore "komrad/internal/store/pullrequest"
	"komrad/lib/xslice"

	"github.com/riverqueue/river"
)

// NewEventHandler builds the handler the router calls for pull request changes.
func NewEventHandler() events.HandlerFor[Change] {
	return handleChange
}

// handleChange tells the writer of each task on the pull request's branch where it now stands, starting the checks failing
// event when an agent has not been told of a failed check or a run that was failing finishes.
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

	statusChecks := pullrequeststore.NewStatusCheckStore(tx)
	mails := mailstore.NewMailStore(tx)

	var followUps []river.JobArgs
	started, holding := eventsOf(change, *pullRequest)
	failed := failedChecks(*pullRequest)
	failing := failingEvent(*pullRequest)
	finished := slices.ContainsFunc(change.Moves, finishesFailing)
	for _, recipient := range recipients {
		failures, err := statusChecks.ListCheckFailures(ctx, pullrequeststore.ListCheckFailuresParams{
			PullRequestID: pullRequest.ID,
			HeadSHA:       pullRequest.MergeReadiness.Checks.HeadSHA,
			AgentID:       recipient,
			Failed:        failed,
		})
		if err != nil {
			return nil, err
		}

		held, err := mails.GetUnreadAbout(ctx, recipient, mailstore.KindPullRequestChanged, pullRequest.ID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}

		newFailures := untold(failures)
		recipientStarted := started
		if failing != "" && (len(newFailures) > 0 || finished || carriesFailing(held)) {
			recipientStarted = append(slices.Clone(started), failing)
		}

		var heldSince *time.Time
		if held != nil {
			heldSince = &held.CreatedAt
		}

		sent, err := events.Send(ctx, mailer, mail.SendFromKomradParams{
			ToAgentID: recipient,
			Mail: eventmail.PullRequest{
				PullRequest: *pullRequest,
				Fields:      listFields(*pullRequest),
				Failures:    failures,
				HeldSince:   heldSince,
				Started:     recipientStarted,
			},
			SubjectID:   pullRequest.ID,
			OnlyRewrite: len(recipientStarted) == 0,
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
		if sent == nil {
			continue
		}

		if err := statusChecks.RecordCheckFailures(ctx, pullrequeststore.RecordCheckFailuresParams{
			PullRequestID: pullRequest.ID,
			HeadSHA:       pullRequest.MergeReadiness.Checks.HeadSHA,
			AgentID:       recipient,
			Told:          newFailures,
		}); err != nil {
			return nil, err
		}
	}

	return followUps, nil
}

func carriesFailing(held *mailstore.Mail) bool {
	if held == nil {
		return false
	}

	for _, event := range eventOf[fieldChecks] {
		if slices.Contains(held.Events, event) {
			return true
		}
	}

	return false
}

func untold(failures []pullrequeststore.CheckFailure) []forge.StatusCheck {
	var checks []forge.StatusCheck
	for _, failure := range failures {
		if failure.NotedAt == nil {
			checks = append(checks, failure.Check)
		}
	}

	return checks
}

func failedChecks(pullRequest pullrequeststore.PullRequest) []forge.StatusCheck {
	if slices.Contains([]forge.Checks{forge.ChecksFailing, forge.ChecksFailingRunning}, pullRequest.MergeReadiness.DisplayForHead(pullRequest.HeadSHA).Checks.Value) {
		return forge.FailedStatusChecks(pullRequest.HeadStatusChecks)
	}

	return nil
}

// fieldValues is the value each field of the pull request stands at.
func fieldValues(pullRequest pullrequeststore.PullRequest) map[field]string {
	displayed := pullRequest.MergeReadiness.DisplayForHead(pullRequest.HeadSHA)

	return map[field]string{
		fieldState:        pullRequest.State,
		fieldDecision:     string(displayed.Decision.Value),
		fieldChecks:       string(displayed.Checks.Value),
		fieldMergeability: string(displayed.Mergeability.Value),
		fieldAutoMerge:    forge.DescribeAutoMerge(pullRequest.MergeReadiness.AutoMerge),
	}
}

// listFields is each field of the pull request in the order the mail lists them.
func listFields(pullRequest pullrequeststore.PullRequest) []eventmail.PullRequestField {
	now := fieldValues(pullRequest)

	return xslice.Map(fieldOrder, func(field field) eventmail.PullRequestField {
		return eventmail.PullRequestField{Name: string(field), Value: now[field]}
	})
}
