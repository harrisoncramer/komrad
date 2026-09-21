package agent

import (
	"weave/internal/agents"
	"weave/internal/rpc/render"
	"weave/lib/dates"

	agentstore "weave/internal/store/agent"
	mailstore "weave/internal/store/mail"
	transcriptstore "weave/internal/store/transcript"
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

	return agent{
		Agent:  render.Agent(row, render.AgentOptions{PromptHead: options.promptHead}),
		cursor: agentstore.Cursor{ID: row.ID},
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
