package pullrequests

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

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
		_, err := events.Send(ctx, mailer, mail.SendFromKomradParams{
			ToAgentID:   recipient,
			Kind:        mailstore.KindPullRequestChanged,
			SubjectID:   pullRequest.ID,
			Body:        body,
			OnlyRewrite: !change.opens(),
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
	now := fieldValues(pullRequest)

	noun := "pull request"
	ref := fmt.Sprintf("#%d", pullRequest.Number)
	checks := fmt.Sprintf("gh pr checks %d", pullRequest.Number)
	if pullRequest.Forge == forge.KindGitLab {
		noun = "merge request"
		ref = fmt.Sprintf("!%d", pullRequest.Number)
		checks = "glab ci status --branch " + pullRequest.HeadBranch
	}

	var body strings.Builder
	fmt.Fprintf(&body, "komrad: %s %s (%s) on %s changed. It stands at:\n", noun, ref, pullRequest.URL, pullRequest.HeadBranch)
	for _, field := range fieldOrder {
		fmt.Fprintf(&body, "  %s: %s\n", field, describe(field, now[field], pullRequest))
	}
	fmt.Fprintf(&body, "Read its checks with %s. Review comments reach you through the person's review in komrad, so do not fetch them from the forge.", checks)

	return body.String()
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
