package agent

import (
	"time"

	"weave/internal/rpc/render"
	"weave/lib/dates"

	agentstore "weave/internal/store/agent"
	mailstore "weave/internal/store/mail"
	transcriptstore "weave/internal/store/transcript"
	"weave/lib/xslice"
	"weave/lib/xstr"
	agentv1 "weave/proto/gen/agent/v1"
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

// agentFlags are the facts about an agent that are not on its own row: whether it is running,
// whether it has mail it has not read, and whether it has said anything the person has not.
type agentFlags struct {
	alive      bool
	unreadMail bool
	unviewed   bool
	failedRun  bool
	// mark is the letter a person types to jump to this row, which lives beside the agent rather
	// than on it.
	mark *string
	// failedRunLog is the log the failure went to.
	failedRunLog *int32
	// localRounds is how many rounds of local review this agent's own work has been through.
	localRounds int32
	// localVerdict is what the last released local review of this agent's newest round concluded,
	// empty when that round carries none.
	localVerdict string
	// inLocalReview reports whether a reviewer child is still owed an answer about this agent's
	// work.
	inLocalReview bool
}

// agentOptions is how much of an agent renderAgent draws.
type agentOptions struct {
	promptHead int
}

type agentOption = render.LoadOption[agentOptions]

// withPromptHead draws the opening of the prompt rather than the whole of it.
func withPromptHead(limit int) agentOption {
	return func(o *agentOptions) { o.promptHead = limit }
}

// agentViewOptions turns the view a request asked for into the options renderAgent takes, so the
// enum stops at the handler rather than reaching the renderer.
func agentViewOptions(view viewv1.View) []agentOption {
	if view == viewv1.View_VIEW_FULL {
		return nil
	}

	return []agentOption{withPromptHead(render.SummaryHead)}
}

func renderAgent(stored agentstore.Agent, flags agentFlags, opts ...agentOption) agent {
	options := agentOptions{}
	for _, opt := range opts {
		opt(&options)
	}

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
			State:              string(stored.State),
			Role:               string(stored.Role),
			SessionId:          stored.SessionID,
			Harness:            stored.Harness,
			WorkflowId:         stored.WorkflowID,
			ScheduleId:         stored.ScheduleID,
			CreatedAt:          dates.Stamp(stored.CreatedAt),
			StateChangedAt:     dates.Stamp(stored.StateChangedAt),
			ReportedReason:     stored.ReportedReason,
			ArchivedAt:         dates.StampPtr(stored.ArchivedAt),
			Alive:              flags.alive,
			UnreadMail:         flags.unreadMail,
			Mark:               flags.mark,
			Hidden:             stored.Hidden,
			Metadata:           metadata,
			IdempotencyKey:     stored.IdempotencyKey,
			SpawnedByAgentId:   stored.SpawnedByAgentID,
			ParentAgentId:      stored.ParentAgentID,
			Branch:             stored.Branch,
			WorktreeRemoved:    stored.WorktreeRemoved,
			Unviewed:           flags.unviewed,
			FailedRun:          flags.failedRun,
			FailedRunLog:       flags.failedRunLog,
			LocalReviewRounds:  flags.localRounds,
			LocalReviewVerdict: flags.localVerdict,
			InLocalReview:      flags.inLocalReview,
			HeldAt:             heldStamp(stored.HeldAt),
		},
		cursor: agentstore.Cursor{ID: stored.ID},
	}
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

type mail struct {
	*agentv1.Mail
	cursor mailstore.Cursor
}

// Cursor returns the keyset cursor locating this mail.
func (m mail) Cursor() (mailstore.Cursor, error) {
	return m.cursor, nil
}

func renderMailPage(stored mailstore.Mail) mail {
	return mail{Mail: renderMail(stored), cursor: mailstore.Cursor{ID: stored.ID}}
}

// searchPage lets the paginator decode a search cursor, which locates a page of both
// sections rather than a single hit the way every other listing's cursor does.
type searchPage struct {
	cursor transcriptstore.Cursor
}

// Cursor returns the keyset cursor locating this page.
func (p searchPage) Cursor() (transcriptstore.Cursor, error) { return p.cursor, nil }

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
