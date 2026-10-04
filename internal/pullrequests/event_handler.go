package pullrequests

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"komrad/internal/events"
	"komrad/internal/forge"
	"komrad/internal/git"
	"komrad/internal/mail"
	"komrad/internal/store"
	mailstore "komrad/internal/store/mail"
	pullrequeststore "komrad/internal/store/pullrequest"

	"github.com/riverqueue/river"
)

// NewEventHandler builds the handler the router is given for pull request changes.
func NewEventHandler() events.HandlerFor[Change] {
	return handleChange
}

// handleChange tells every agent on the pull request's branch where it now stands, starting the checks failing
// event for an agent not yet told of a failed check.
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
	for _, recipient := range recipients {
		failures, err := statusChecks.ListCheckFailures(ctx, pullrequeststore.ListCheckFailuresParams{
			PullRequestID: pullRequest.ID,
			HeadSHA:       pullRequest.MergeReadiness.Checks.HeadSHA,
			AgentID:       recipient,
			Failed:        failedChecks(*pullRequest),
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
		if event := failingEvent(*pullRequest); event != "" && (len(newFailures) > 0 || carriesFailing(held)) {
			recipientStarted = append(slices.Clone(started), event)
		}

		var heldSince *time.Time
		if held != nil {
			heldSince = &held.CreatedAt
		}

		sent, err := events.Send(ctx, mailer, mail.SendFromKomradParams{
			ToAgentID:   recipient,
			Kind:        mailstore.KindPullRequestChanged,
			SubjectID:   pullRequest.ID,
			Body:        render(*pullRequest, failures, heldSince),
			OnlyRewrite: len(recipientStarted) == 0,
			Events:      recipientStarted,
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

func render(pullRequest pullrequeststore.PullRequest, failures []pullrequeststore.CheckFailure, heldSince *time.Time) string {
	now := fieldValues(pullRequest)

	noun := "pull request"
	ref := fmt.Sprintf("#%d", pullRequest.Number)
	if pullRequest.Forge == forge.KindGitLab {
		noun = "merge request"
		ref = fmt.Sprintf("!%d", pullRequest.Number)
	}

	var body strings.Builder
	fmt.Fprintf(&body, "komrad: %s %s (%s) on %s changed. It stands at:\n", noun, ref, pullRequest.URL, pullRequest.HeadBranch)
	for _, field := range fieldOrder {
		fmt.Fprintf(&body, "  %s: %s\n", field, describe(field, now[field], pullRequest))
	}
	var newFailures, stillFailing []pullrequeststore.CheckFailure
	for _, failure := range failures {
		if isNewTo(failure, heldSince) {
			newFailures = append(newFailures, failure)
		} else {
			stillFailing = append(stillFailing, failure)
		}
	}
	writeChecks(&body, "New failures:", newFailures)
	writeChecks(&body, "Still failing:", stillFailing)
	body.WriteString("Review comments reach you through the person's review in komrad, so do not fetch them from the forge.")

	return body.String()
}

// isNewTo says whether a failure is news to an agent holding unread mail written at heldSince: one it has not
// been told of, or was told of only in that mail.
func isNewTo(failure pullrequeststore.CheckFailure, heldSince *time.Time) bool {
	return failure.NotedAt == nil || heldSince != nil && !failure.NotedAt.Before(*heldSince)
}

func writeChecks(body *strings.Builder, heading string, failures []pullrequeststore.CheckFailure) {
	if len(failures) == 0 {
		return
	}

	body.WriteString(heading + "\n")
	for _, failure := range failures {
		fmt.Fprintf(body, "  %s: %s\n", failure.Check.Name, cmp.Or(failure.Check.URL, "no link"))
	}
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

func describe(field field, value string, pullRequest pullrequeststore.PullRequest) string {
	switch {
	case field == fieldState:
		return strings.ToLower(value)
	case field == fieldMergeability && (value == "conflicting" || value == "behind"):
		return value + " with " + pullRequest.BaseBranch
	case field == fieldChecks && value == string(forge.ChecksFailingRunning) && pullRequest.HeadSHA != "":
		return "failing on " + git.ShortSHA(pullRequest.HeadSHA) + ", others still running"
	case field == fieldChecks && value != "" && pullRequest.HeadSHA != "":
		return strings.ReplaceAll(value, "_", " ") + " on " + git.ShortSHA(pullRequest.HeadSHA)
	case field == fieldAutoMerge && value == forge.AutoMergeOn:
		return "on, so whatever is on the branch merges once its checks and reviews pass"
	case value == "":
		return "unknown"
	default:
		return strings.ReplaceAll(value, "_", " ")
	}
}
