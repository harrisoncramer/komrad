package agent

import (
	"time"

	"weave/internal/agents"
	"weave/internal/git"
	"weave/internal/rpc/render"
	"weave/lib/dates"

	agentstore "weave/internal/store/agent"
	mailstore "weave/internal/store/mail"
	reviewstore "weave/internal/store/review"
	transcriptstore "weave/internal/store/transcript"
	"weave/lib/xptr"
	"weave/lib/xslice"
	"weave/lib/xstr"
	agentv1 "weave/proto/gen/agent/v1"
	pullrequestv1 "weave/proto/gen/pullrequest/v1"
	viewv1 "weave/proto/gen/view/v1"
)

type agent struct {
	*agentv1.Agent
	cursor agentstore.Cursor
}

// Cursor returns the keyset cursor locating this agent.
func (s agent) Cursor() (agentstore.Cursor, error) {
	return s.cursor, nil
}

// agentOptions is how much of an agent is drawn.
type agentOptions struct {
	promptHead int
}

type agentOption = render.LoadOption[agentOptions]

// SetPromptHead sets how much of the prompt is drawn.
func (o *agentOptions) SetPromptHead(limit int) {
	o.promptHead = limit
}

// agentViewOptions turns the view a request asked for into rendering options.
func agentViewOptions(view viewv1.View) []agentOption {
	if view == viewv1.View_VIEW_FULL {
		return nil
	}

	return []agentOption{render.WithPromptHead[agentOptions](render.SummaryHead)}
}

func renderAgent(row agents.Row, opts ...agentOption) agent {
	options := agentOptions{}
	for _, opt := range opts {
		opt(&options)
	}

	stored := row.Agent
	metadata := xslice.Map(stored.Metadata, func(entry agentstore.Metadata) *agentv1.AgentMetadata {
		return &agentv1.AgentMetadata{Key: entry.Key, Value: entry.Value}
	})

	prompt, promptTruncated := stored.Prompt, false
	if options.promptHead > 0 {
		prompt, promptTruncated = xstr.Head(stored.Prompt, options.promptHead)
	}

	return agent{
		Agent: &agentv1.Agent{
			Id:                 stored.ID,
			Name:               stored.Name,
			Cwd:                stored.Cwd,
			Flags:              stored.Flags,
			Prompt:             prompt,
			PromptTruncated:    promptTruncated,
			PaneId:             stored.Pane,
			State:              render.AgentStateOf(stored.State),
			Role:               render.AgentRoleOf(stored.Role),
			SessionId:          stored.SessionID,
			Harness:            stored.Harness,
			WorkflowId:         stored.WorkflowID,
			ScheduleId:         stored.ScheduleID,
			CreatedAt:          dates.Stamp(stored.CreatedAt),
			StateChangedAt:     dates.Stamp(stored.StateChangedAt),
			ReportedReason:     stored.ReportedReason,
			ArchivedAt:         dates.StampPtr(stored.ArchivedAt),
			Alive:              xptr.Ptr(row.Alive),
			UnreadMail:         row.UnreadMail,
			Mark:               row.Mark,
			Hidden:             stored.Hidden,
			Metadata:           metadata,
			IdempotencyKey:     stored.IdempotencyKey,
			SpawnedByAgentId:   stored.SpawnedByAgentID,
			ParentAgentId:      stored.ParentAgentID,
			Branch:             stored.Branch,
			RepositoryUrl:      stored.RepositoryURL,
			WorktreeRemoved:    stored.WorktreeRemoved,
			SequenceInProgress: stored.SequenceInProgress,
			FailedRun:          row.FailedRun,
			FailedRunLog:       row.FailedRunLog,
			LocalReviewRounds:  row.LocalRounds,
			LocalReviewVerdict: renderLocalVerdict(row.LocalVerdict),
			LocalReviewTree:    renderStanding(row.LocalTree),
			HeldAt:             heldStamp(stored.HeldAt),
			PullRequest:        renderPullRequestStanding(row),
		},
		cursor: agentstore.Cursor{ID: stored.ID},
	}
}

// renderLocalVerdict is the last local conclusion as the wire carries it, UNSPECIFIED when
// there is none.
func renderLocalVerdict(verdict *reviewstore.Verdict) agentv1.ReviewVerdict {
	if verdict == nil {
		return agentv1.ReviewVerdict_REVIEW_VERDICT_UNSPECIFIED
	}

	return renderReviewVerdict(*verdict)
}

// renderPullRequestStanding is where the agent's pull request stands as the wire carries it:
// unread until the sync has looked at the repository, none when it has and found nothing on
// the branch, else the current one and its state.
func renderPullRequestStanding(row agents.Row) *agentv1.PullRequestStanding {
	if row.PullRequest == nil {
		state := pullrequestv1.PullRequestState_PULL_REQUEST_STATE_UNREAD
		if row.RepositoryRead {
			state = pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE
		}

		return &agentv1.PullRequestStanding{State: state}
	}

	return &agentv1.PullRequestStanding{
		State:  render.PullRequestStateOf(*row.PullRequest),
		Number: xptr.Ptr(int32(row.PullRequest.Number)),
		Url:    xptr.Ptr(row.PullRequest.URL),
		Title:  xptr.Ptr(row.PullRequest.Title),
	}
}

// renderStanding is a standing as the wire carries it.
func renderStanding(standing git.Standing) agentv1.ReviewTreeStanding {
	switch standing {
	case git.StandingMoved:
		return agentv1.ReviewTreeStanding_REVIEW_TREE_STANDING_MOVED
	case git.StandingLanded:
		return agentv1.ReviewTreeStanding_REVIEW_TREE_STANDING_LANDED
	case git.StandingLandedMoved:
		return agentv1.ReviewTreeStanding_REVIEW_TREE_STANDING_LANDED_MOVED
	case git.StandingSame:
	}

	return agentv1.ReviewTreeStanding_REVIEW_TREE_STANDING_UNSPECIFIED
}

func renderMail(stored mailstore.Mail) *agentv1.Mail {
	return &agentv1.Mail{
		Id:          stored.ID,
		FromAgentId: stored.FromAgentID,
		FromName:    stored.FromName,
		ToAgentId:   stored.ToAgentID,
		Body:        stored.Body,
		ReplyTo:     stored.ReplyTo,
		CreatedAt:   dates.Stamp(stored.CreatedAt),
		ClaimedAt:   dates.StampPtr(stored.ClaimedAt),
		ExpiresAt:   dates.Stamp(stored.ExpiresAt),
		Kind:        renderMailKind(stored.Kind),
	}
}

func renderMailKind(kind mailstore.Kind) agentv1.MailKind {
	switch kind {
	case mailstore.KindPlain:
		return agentv1.MailKind_MAIL_KIND_PLAIN
	case mailstore.KindReviewActivity:
		return agentv1.MailKind_MAIL_KIND_REVIEW_ACTIVITY
	case mailstore.KindExchangeCapped:
		return agentv1.MailKind_MAIL_KIND_EXCHANGE_CAPPED
	}

	return agentv1.MailKind_MAIL_KIND_UNSPECIFIED
}

type mailPage struct {
	*agentv1.Mail
	cursor mailstore.Cursor
}

// Cursor returns the keyset cursor locating this mail.
func (m mailPage) Cursor() (mailstore.Cursor, error) {
	return m.cursor, nil
}

func renderMailPage(stored mailstore.Mail) mailPage {
	return mailPage{Mail: renderMail(stored), cursor: mailstore.Cursor{ID: stored.ID}}
}

// searchPage lets the paginator decode a search cursor, which locates a page of both
// sections rather than a single hit the way every other listing's cursor does.
type searchPage struct {
	cursor transcriptstore.Cursor
}

// Cursor returns the keyset cursor locating this page.
func (p searchPage) Cursor() (transcriptstore.Cursor, error) {
	return p.cursor, nil
}

func renderSearchHit(hit transcriptstore.SearchHit) *agentv1.SearchHit {
	return &agentv1.SearchHit{
		AgentId:       hit.AgentID,
		Name:          hit.Name,
		Archived:      hit.Archived,
		Snippet:       hit.Snippet,
		Score:         hit.Rank,
		Seq:           hit.Seq,
		Role:          hit.Role,
		ContentOffset: hit.ContentOffset,
		ContentLength: hit.ContentLength,
	}
}

// heldStamp is when a person took the agent's worktree, empty while nobody has.
func heldStamp(at *time.Time) string {
	if at == nil {
		return ""
	}

	return dates.Stamp(*at)
}
