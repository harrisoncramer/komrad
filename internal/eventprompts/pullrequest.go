package eventprompts

import (
	"cmp"
	"fmt"
	"strings"
	"time"

	"komrad/internal/forge"
	mailstore "komrad/internal/store/mail"
	pullrequeststore "komrad/internal/store/pullrequest"
	"komrad/lib/xsha"
	eventv1 "komrad/proto/gen/event/v1"
)

// PullRequestFieldName names one field of a pull request the mail lists.
type PullRequestFieldName string

const (
	// PullRequestFieldState is whether the pull request is open, merged or closed.
	PullRequestFieldState PullRequestFieldName = "state"
	// PullRequestFieldDecision is where the reviews stand.
	PullRequestFieldDecision = PullRequestFieldName(forge.MergeReadinessFieldDecision)
	// PullRequestFieldChecks is where the checks on the head stand.
	PullRequestFieldChecks = PullRequestFieldName(forge.MergeReadinessFieldChecks)
	// PullRequestFieldMergeability is where the branch stands against its base.
	PullRequestFieldMergeability = PullRequestFieldName(forge.MergeReadinessFieldMergeability)
	// PullRequestFieldAutoMerge is whether the forge will merge it on its own once its checks and reviews pass.
	PullRequestFieldAutoMerge = PullRequestFieldName(forge.MergeReadinessFieldAutoMerge)
)

// PullRequestField is one field of a pull request the mail lists, and the value it stands at.
type PullRequestField struct {
	Name  PullRequestFieldName
	Value string
}

// PullRequest tells an agent where a pull request on its branch now stands.
type PullRequest struct {
	PullRequest pullrequeststore.PullRequest
	Fields      []PullRequestField
	Failures    []pullrequeststore.CheckFailure
	// HeldSince is when the unread mail this rewrites was written, nil when there is none.
	HeldSince *time.Time
	Started   []eventv1.Event
}

// Kind is the kind the pull request mail is filed under.
func (PullRequest) Kind() mailstore.Kind {
	return mailstore.KindPullRequestChanged
}

// Events is the events the change starts.
func (m PullRequest) Events() []eventv1.Event {
	return m.Started
}

// Body lists every field of the pull request and the checks failing on it.
func (m PullRequest) Body() string {
	pullRequest := m.PullRequest

	noun := "pull request"
	ref := fmt.Sprintf("#%d", pullRequest.Number)
	if pullRequest.Forge == forge.KindGitLab {
		noun = "merge request"
		ref = fmt.Sprintf("!%d", pullRequest.Number)
	}

	var body strings.Builder
	fmt.Fprintf(&body, "komrad: %s %s (%s) on %s changed. It stands at:\n", noun, ref, pullRequest.URL, pullRequest.HeadBranch)
	for _, field := range m.Fields {
		fmt.Fprintf(&body, "  %s: %s\n", field.Name, describe(field, pullRequest))
	}
	var newFailures, stillFailing []pullrequeststore.CheckFailure
	for _, failure := range m.Failures {
		if isNewTo(failure, m.HeldSince) {
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

func describe(field PullRequestField, pullRequest pullrequeststore.PullRequest) string {
	value := field.Value
	switch {
	case field.Name == PullRequestFieldState:
		return strings.ToLower(value)
	case field.Name == PullRequestFieldMergeability && (value == "conflicting" || value == "behind"):
		return value + " with " + pullRequest.BaseBranch
	case field.Name == PullRequestFieldChecks && value == string(forge.ChecksFailingRunning) && pullRequest.HeadSHA != "":
		return "failing on " + xsha.Short(pullRequest.HeadSHA) + ", others still running"
	case field.Name == PullRequestFieldChecks && value != "" && pullRequest.HeadSHA != "":
		return strings.ReplaceAll(value, "_", " ") + " on " + xsha.Short(pullRequest.HeadSHA)
	case field.Name == PullRequestFieldAutoMerge && value == forge.AutoMergeOn:
		return "on, so whatever is on the branch merges once its checks and reviews pass"
	case value == "":
		return "unknown"
	default:
		return strings.ReplaceAll(value, "_", " ")
	}
}
