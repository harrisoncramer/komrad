package agents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"komrad/internal/agenterr"
	"komrad/internal/fail"
	"komrad/internal/logging"
	"komrad/internal/mail"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	mailstore "komrad/internal/store/mail"
	"komrad/lib/xptr"
)

// ErrNotHeld is a release of an agent nobody was holding.
var ErrNotHeld = fail.Precondition("nobody is holding this agent")

// errAlreadyHeld is a hold on an agent somebody is already holding.
var errAlreadyHeld = fail.AlreadyExists("this agent is already held")

// errHoldSelf is an agent holding itself, which would stop it with nobody who knows to let it go.
var errHoldSelf = fail.InvalidArgument("an agent cannot hold itself")

type mailSender interface {
	SendMail(ctx context.Context, params mail.SendMailParams) (*mailstore.Mail, error)
}

// NewHolderParams is what a holder is built over.
type NewHolderParams struct {
	Agents     *agentstore.AgentStore
	MailSender mailSender
	Settler    *Settler
	Clock      func() time.Time
}

// Holder stops an agent on a person's behalf and lets it go again.
type Holder struct {
	agents     *agentstore.AgentStore
	mailSender mailSender
	settler    *Settler
	now        func() time.Time
}

// NewHolder builds the holder over the store, the mail that reaches an agent and the settler that
// stops it.
func NewHolder(params NewHolderParams) *Holder {
	now := params.Clock
	if now == nil {
		now = time.Now
	}

	return &Holder{
		agents:     params.Agents,
		mailSender: params.MailSender,
		settler:    params.Settler,
		now:        now,
	}
}

// HoldParams is the agent being held and the agent holding it.
type HoldParams struct {
	AgentID string
	// ByAgentID is the agent taking the hold, empty for the panel.
	ByAgentID string
}

// Hold stops an agent and records who has it, refusing unless it observes the agent come to rest.
func (h *Holder) Hold(ctx context.Context, params HoldParams) (*agentstore.Agent, error) {
	if params.ByAgentID != "" && params.ByAgentID == params.AgentID {
		return nil, errHoldSelf
	}

	agent, err := h.get(ctx, params.AgentID)
	if err != nil {
		return nil, err
	}

	if agent.Hold.IsHeld() {
		return nil, errAlreadyHeld.Saying("%q is already paused", agent.Name)
	}

	if agent.Pane == "" {
		return nil, agenterr.ErrUnsettled.Saying("%q is not in a pane", agent.Name)
	}

	interrupted, err := h.settler.Settle(ctx, *agent)
	if err != nil {
		return nil, err
	}

	held, err := h.agents.Hold(ctx, agentstore.HoldParams{
		ID:        agent.ID,
		At:        h.now(),
		MidTurn:   interrupted,
		ByAgentID: xptr.NonEmpty(params.ByAgentID),
	})
	if err != nil {
		return nil, err
	}
	if !held {
		return nil, errAlreadyHeld.Saying("%q is already paused", agent.Name)
	}

	return h.get(ctx, params.AgentID)
}

// ReleaseParams is the agent being let go and what to tell it happened while it was held.
type ReleaseParams struct {
	AgentID string
	// ByAgentID is the agent letting it go, empty for the panel.
	ByAgentID string
	// Said is mailed to the agent as it starts again, empty to let it go in silence.
	Said string
}

// Release lets go of an agent, mailing it what happened only when the hold cut a turn short.
func (h *Holder) Release(ctx context.Context, params ReleaseParams) (*agentstore.Agent, error) {
	agent, err := h.get(ctx, params.AgentID)
	if err != nil {
		return nil, err
	}

	if !agent.Hold.IsHeld() {
		return nil, ErrNotHeld.Saying("nobody is holding %q", agent.Name)
	}

	released, err := h.agents.Release(ctx, agent.ID)
	if err != nil {
		return nil, err
	}
	if !released {
		return nil, ErrNotHeld.Saying("nobody is holding %q", agent.Name)
	}

	if agent.Hold.MidTurn && params.Said != "" {
		h.tell(ctx, *agent, params)
	}

	return h.get(ctx, params.AgentID)
}

func (h *Holder) get(ctx context.Context, agentID string) (*agentstore.Agent, error) {
	agent, err := h.agents.GetByID(ctx, agentID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: %q", agenterr.ErrAgentNotFound, agentID)
	}
	if err != nil {
		return nil, err
	}

	return agent, nil
}

// tell mails a released agent what changed while it was held, reporting rather than failing over.
func (h *Holder) tell(ctx context.Context, agent agentstore.Agent, params ReleaseParams) {
	if _, err := h.mailSender.SendMail(ctx, mail.SendMailParams{
		From:      params.ByAgentID,
		ToAgentID: agent.ID,
		Body:      params.Said,
	}); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "released a held agent without telling it what changed",
			append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)
	}
}
