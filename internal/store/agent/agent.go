// Package agent stores the agents themselves: the row for each one, the state the reconciler
// keeps up to date, the metadata an agent records about its own work, and whether it is archived
// or still on screen.
package agent

import (
	"context"
	"strings"
	"time"

	"weave/db/models"
	"weave/internal/store"
	"weave/lib/xslice"

	"github.com/jackc/pgx/v5"
)

// State is what an agent is doing.
type State string

const (
	StateThinking        State = State(models.AgentStateThinking)
	StateWaiting         State = State(models.AgentStateWaiting)
	StateIdle            State = State(models.AgentStateIdle)
	StateSettingUp       State = State(models.AgentStateSettingUp)
	StateFailedToCreate  State = State(models.AgentStateFailedToCreate)
	StateFailedToCleanup State = State(models.AgentStateFailedToCleanup)
	StateUncertain       State = State(models.AgentStateUncertain)
)

// DoesPaneDecide reports whether reading the agent's terminal is what decides this state.
func (s State) DoesPaneDecide() bool {
	// Written without a default so an eighth state has to be answered for here rather than
	// falling in with whichever answer the existing seven happen to share.
	switch s {
	case StateIdle, StateThinking, StateWaiting, StateUncertain:
		return true
	case StateSettingUp, StateFailedToCreate, StateFailedToCleanup:
		return false
	}

	return false
}

// IsFailed reports whether a state is one of the two an agent reaches by failing.
func (s State) IsFailed() bool {
	return s == StateFailedToCreate || s == StateFailedToCleanup
}

// IsTurnUnfinished reports whether a state leaves a turn part-way through.
func (s State) IsTurnUnfinished() bool {
	return s == StateThinking || s == StateWaiting
}

// IsAtRest reports whether a state says the agent is doing nothing weave could disturb.
func (s State) IsAtRest() bool {
	// Written without a default so an eighth state has to be answered for here rather than
	// falling in with whichever answer the existing seven happen to share.
	switch s {
	case StateIdle, StateFailedToCreate, StateFailedToCleanup:
		return true
	case StateThinking, StateWaiting, StateSettingUp, StateUncertain:
		return false
	}

	return false
}

// Role is what an agent was launched to do.
type Role string

const (
	RoleImplementer  Role = Role(models.AgentRoleImplementer)
	RoleReviewer     Role = Role(models.AgentRoleReviewer)
	RoleOrchestrator Role = Role(models.AgentRoleOrchestrator)
)

// orDefault is the role an agent that names none is given.
func (r Role) orDefault() Role {
	if r == "" {
		return RoleImplementer
	}

	return r
}

type Agent struct {
	ID    string
	Name  string
	Pane  string
	Cwd   string
	Flags string
	// ProvisionEnabled overrides what the workflow does about the agent's directory: unset
	// inherits it, and false means this agent keeps the directory it was given.
	ProvisionEnabled *bool
	Prompt           string

	// Role is what the agent was launched to do, and decides which review briefing it is sent.
	Role           Role
	State          State
	StateChangedAt time.Time
	StateSeenAt    time.Time
	// TurnEndedAt is when a turn of the agent's last ended, reported by its own CLI or, when
	// nothing reported one, settled off the pane by the reconciler.
	TurnEndedAt *time.Time

	// ReportedReason is what the agent's own CLI last said it was doing, cleared whenever the
	// state changes.
	ReportedReason string
	Harness        string
	SessionID      string
	WorkflowID     string
	ScheduleID     *string
	IdempotencyKey *string
	ArchivedAt     *time.Time
	DeletingAt     *time.Time
	NudgedAt       *time.Time

	// NudgeSentAt is when an agent was last told about its mail, which a claimed nudge that
	// typed nothing does not set.
	NudgeSentAt      *time.Time
	Hidden           bool
	Metadata         []Metadata
	SpawnedByAgentID *string

	// ParentAgentID is the agent this one is attached to, whose archive, delete and restore it
	// follows.
	ParentAgentID *string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Branch        *string

	// RepositoryURL is the canonical web address of the repository the checkout pushes to.
	RepositoryURL *string

	// AdoptedBranch is the branch an agent was put on rather than one weave made for it.
	AdoptedBranch   string
	WorktreeRemoved bool
	InterruptedAt   *time.Time

	// SequenceInProgress is whether a merge, a rebase or a cherry-pick is part-way through the
	// agent's checkout and waiting on somebody.
	SequenceInProgress bool

	// HeldAt is when a person stopped this agent and took its worktree, nil while nobody holds it.
	HeldAt *time.Time
	// HeldMidTurn says the hold cut a turn that was still running.
	HeldMidTurn bool
}

// SanitizeName maps a name to characters tmux accepts in session names.
func SanitizeName(name string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r < 0x20, r == 0x7f, r == ':', r == '.', r == ' ':
			return '-'
		default:
			return r
		}
	}, name)
}

type Store struct {
	queries *models.Queries
	db      store.DBTX
}

// New builds an agent store over a pool or transaction.
func New(db store.DBTX) *Store {
	return &Store{queries: models.New(db), db: db}
}

type txBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// AgentPane names a pane and the agent that owns it.
type AgentPane struct {
	AgentID string
	Pane    string
}

// IDs is the id of each agent, in the order they were given.
func IDs(agents []Agent) []string {
	return xslice.Map(agents, func(agent Agent) string { return agent.ID })
}

func agentFromRow(agent models.Agent) Agent {
	return Agent{
		ID:                 agent.ID,
		Name:               agent.Name,
		Pane:               agent.PaneID,
		Cwd:                agent.Cwd,
		Flags:              agent.Flags,
		ProvisionEnabled:   agent.ProvisionEnabled,
		Prompt:             agent.Prompt,
		Role:               Role(agent.Role),
		State:              State(agent.State),
		StateChangedAt:     agent.StateChangedAt,
		StateSeenAt:        agent.StateSeenAt,
		TurnEndedAt:        agent.TurnEndedAt,
		ReportedReason:     agent.ReportedReason,
		Harness:            agent.Harness,
		SessionID:          agent.SessionID,
		WorkflowID:         agent.WorkflowID,
		ScheduleID:         agent.ScheduleID,
		IdempotencyKey:     agent.IdempotencyKey,
		ArchivedAt:         agent.ArchivedAt,
		DeletingAt:         agent.DeletingAt,
		NudgedAt:           agent.NudgedAt,
		NudgeSentAt:        agent.NudgeSentAt,
		Hidden:             agent.IsHidden,
		SpawnedByAgentID:   agent.SpawnedByAgentID,
		ParentAgentID:      agent.ParentAgentID,
		CreatedAt:          agent.CreatedAt,
		UpdatedAt:          agent.UpdatedAt,
		Branch:             agent.Branch,
		RepositoryURL:      agent.RepositoryUrl,
		AdoptedBranch:      agent.AdoptedBranch,
		WorktreeRemoved:    agent.WorktreeRemoved,
		InterruptedAt:      agent.InterruptedAt,
		SequenceInProgress: agent.SequenceInProgress,
		HeldAt:             agent.HeldAt,
		HeldMidTurn:        agent.HeldMidTurn,
	}
}
