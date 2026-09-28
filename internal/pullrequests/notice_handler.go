package pullrequests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"komrad/internal/mail"
	"komrad/internal/notices"
	"komrad/internal/store"
	mailstore "komrad/internal/store/mail"
	pullrequeststore "komrad/internal/store/pullrequest"

	"github.com/riverqueue/river"
)

const shortSHA = 7

type span struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// NoticeHandler keeps one unread notice per agent on a pull request's branch saying what moved since
// it last read one.
type NoticeHandler struct{}

// NewNoticeHandler builds the handler the router is given for pull request changes.
func NewNoticeHandler() *NoticeHandler {
	return &NoticeHandler{}
}

// Topic is the topic of a Change.
func (*NoticeHandler) Topic() string {
	return changeTopic
}

// Handle tells every agent on the pull request's branch about the change.
func (*NoticeHandler) Handle(ctx context.Context, tx store.DBTX, mailer *mail.MailService, payload json.RawMessage) ([]river.JobArgs, error) {
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

	decide := decider(*pullRequest, change)
	for _, recipient := range recipients {
		key := notices.Key{ToAgentID: recipient, Kind: mailstore.KindPullRequestChanged, SubjectID: pullRequest.ID}
		if err := notices.Rewrite(ctx, mailer, key, decide); err != nil {
			return nil, err
		}
	}

	return nil, nil
}

func decider(pullRequest pullrequeststore.PullRequest, change Change) func(map[Field]span, bool) (notices.Draft[map[Field]span], bool) {
	return func(earlier map[Field]span, held bool) (notices.Draft[map[Field]span], bool) {
		if !held && !change.Opens() {
			return notices.Draft[map[Field]span]{}, false
		}

		spans := map[Field]span{}
		for field, told := range earlier {
			spans[field] = told
		}
		for _, move := range change.Moves {
			if _, ok := spans[move.Field]; !ok {
				spans[move.Field] = span{From: move.From}
			}
		}

		now := currentValues(pullRequest)
		for field, told := range spans {
			told.To = now[field]
			if told.From == told.To {
				delete(spans, field)

				continue
			}
			spans[field] = told
		}

		return notices.Draft[map[Field]span]{Body: render(pullRequest, spans), State: spans}, worthTelling(spans)
	}
}

func currentValues(pullRequest pullrequeststore.PullRequest) map[Field]string {
	return map[Field]string{
		FieldState:        pullRequest.State,
		FieldDecision:     pullRequest.LastKnownDecision,
		FieldChecks:       pullRequest.LastKnownChecks,
		FieldMergeability: pullRequest.LastKnownMergeability,
		FieldHead:         pullRequest.HeadSHA,
	}
}

func worthTelling(spans map[Field]span) bool {
	for field := range spans {
		if field != FieldHead {
			return true
		}
	}

	return false
}

func render(pullRequest pullrequeststore.PullRequest, spans map[Field]span) string {
	var body strings.Builder
	fmt.Fprintf(&body, "komrad: pull request #%d (%s) on %s changed since you last heard.\n", pullRequest.Number, pullRequest.URL, pullRequest.HeadBranch)
	for _, field := range fieldOrder {
		told, ok := spans[field]
		if !ok {
			continue
		}
		fmt.Fprintf(&body, "  %s: %s → %s\n", field, describe(field, told.From, pullRequest), describe(field, told.To, pullRequest))
	}
	fmt.Fprintf(&body, "Read it with gh pr view %d and gh pr checks %d.", pullRequest.Number, pullRequest.Number)

	return body.String()
}

func describe(field Field, value string, pullRequest pullrequeststore.PullRequest) string {
	switch {
	case field == FieldHead && len(value) > shortSHA:
		return value[:shortSHA]
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
