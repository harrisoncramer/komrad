package agent

import (
	"time"

	agentstore "weave/internal/store/agent"
	mailstore "weave/internal/store/mail"
	transcriptstore "weave/internal/store/transcript"
	"weave/lib/xslice"
	agentv1 "weave/proto/gen/agent/v1"
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
	// mark is the letter a person addresses this row by, which lives beside the agent rather
	// than on it, since a letter names an agent or a workflow.
	mark *string
	// failedRunLog is the log the failure went to, which is what opens the pane on the run
	// that failed rather than on whatever ran last.
	failedRunLog *int32
}

func renderAgent(stored agentstore.Agent, flags agentFlags) agent {
	created := ""
	if !stored.CreatedAt.IsZero() {
		created = stored.CreatedAt.Format(time.RFC3339)
	}

	stateChanged := ""
	if !stored.StateChangedAt.IsZero() {
		stateChanged = stored.StateChangedAt.Format(time.RFC3339)
	}

	var archived *string
	if stored.ArchivedAt != nil {
		formatted := stored.ArchivedAt.Format(time.RFC3339)
		archived = &formatted
	}

	metadata := xslice.Map(stored.Metadata, func(entry agentstore.Metadata) *agentv1.AgentMetadata {
		return &agentv1.AgentMetadata{Key: entry.Key, Value: entry.Value}
	})

	return agent{
		Agent: &agentv1.Agent{
			Id:               stored.ID,
			Name:             stored.Name,
			Cwd:              stored.Cwd,
			Flags:            stored.Flags,
			Prompt:           stored.Prompt,
			PaneId:           stored.Pane,
			State:            string(stored.State),
			SessionId:        stored.SessionID,
			Harness:          stored.Harness,
			WorkflowId:       stored.WorkflowID,
			ScheduleId:       stored.ScheduleID,
			CreatedAt:        created,
			StateChangedAt:   stateChanged,
			ReportedReason:   stored.ReportedReason,
			ArchivedAt:       archived,
			Alive:            flags.alive,
			UnreadMail:       flags.unreadMail,
			Mark:             flags.mark,
			Hidden:           stored.Hidden,
			Metadata:         metadata,
			IdempotencyKey:   stored.IdempotencyKey,
			SpawnedByAgentId: stored.SpawnedByAgentID,
			ParentAgentId:    stored.ParentAgentID,
			Branch:           stored.Branch,
			WorktreeRemoved:  stored.WorktreeRemoved,
			Unviewed:         flags.unviewed,
			FailedRun:        flags.failedRun,
			FailedRunLog:     flags.failedRunLog,
		},
		cursor: agentstore.Cursor{ID: stored.ID},
	}
}

func renderMail(stored mailstore.Mail) *agentv1.Mail {
	created := ""
	if !stored.CreatedAt.IsZero() {
		created = stored.CreatedAt.Format(time.RFC3339)
	}

	var claimed *string
	if stored.ClaimedAt != nil {
		formatted := stored.ClaimedAt.Format(time.RFC3339)
		claimed = &formatted
	}

	expires := ""
	if !stored.ExpiresAt.IsZero() {
		expires = stored.ExpiresAt.Format(time.RFC3339)
	}

	return &agentv1.Mail{
		Id:          stored.ID,
		FromAgentId: stored.FromAgentID,
		FromName:    stored.FromName,
		ToAgentId:   stored.ToAgentID,
		Body:        stored.Body,
		ReplyTo:     stored.ReplyTo,
		CreatedAt:   created,
		ClaimedAt:   claimed,
		ExpiresAt:   expires,
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
