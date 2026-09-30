// Package agent stores the agents themselves: the row for each one, the state the reconciler
// keeps up to date, and whether it is archived or still on screen.
package agent

import (
	"strings"
	"time"

	"komrad/db/models"
	"komrad/internal/store"
	"komrad/lib/xslice"
)

// StateSeenBound is how long a reading of an agent's state stands before it is unconfirmed.
const StateSeenBound = 2 * time.Minute

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
	StateErrored         State = State(models.AgentStateErrored)
)

// DoesPaneDecide reports whether reading the agent's terminal is what decides this state.
func (s State) DoesPaneDecide() bool {
	// Written without a default so a ninth state has to be answered for here rather than
	// falling in with whichever answer the existing eight happen to share.
	switch s {
	case StateIdle, StateThinking, StateWaiting, StateUncertain, StateErrored:
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

// Role is what an agent was launched to do.
type Role string

const (
	RoleImplementer  Role = Role(models.AgentRoleImplementer)
	RoleReviewer     Role = Role(models.AgentRoleReviewer)
	RoleOrchestrator Role = Role(models.AgentRoleOrchestrator)
)

// Roles is every role an agent may hold.
var Roles = []Role{RoleImplementer, RoleReviewer, RoleOrchestrator}

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
	TurnEndedAt  *time.Time
	TurnFailedAt *time.Time
	TurnError    string

	// ReportedReason is what the agent's own CLI last said it was doing, cleared whenever the
	// state changes.
	ReportedReason string

	// FailureReason is why the agent's create or cleanup last failed.
	FailureReason  string
	Harness        string
	SessionID      string
	WorkflowID     string
	ScheduleID     *string
	IdempotencyKey *string
	ArchivedAt     *time.Time

	// ArchiveBatchID is the archive that took this agent, shared by every agent it took.
	ArchiveBatchID *string
	DeletingAt     *time.Time
	NudgedAt       *time.Time

	// NudgeSentAt is when an agent was last told about its mail, which a claimed nudge that
	// typed nothing does not set.
	NudgeSentAt      *time.Time
	Hidden           bool
	SpawnedByAgentID *string

	// ParentAgentID is the agent this one is attached to, whose archive, delete and restore it
	// follows.
	ParentAgentID *string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Branch        *string

	// RepositoryURL is the canonical web address of the repository the checkout pushes to.
	RepositoryURL *string

	// HeadSHA is the commit the checkout has checked out.
	HeadSHA *string

	// AdoptedBranch is the branch an agent was put on rather than one komrad made for it.
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

	// Alive is whether the agent still owns the pane its row names.
	Alive bool
	// MovedSinceRound is the newest round's snapshot while the worktree no longer hashes to it.
	MovedSinceRound string
}

// IsLastTurnFailed reports whether an API error ended the agent's last turn.
func (a Agent) IsLastTurnFailed() bool {
	if a.TurnFailedAt == nil {
		return false
	}

	return a.TurnEndedAt == nil || !a.TurnFailedAt.Before(*a.TurnEndedAt)
}

// IsStateUnconfirmed reports whether the agent's state was last read longer ago than the bound.
func (a Agent) IsStateUnconfirmed() bool {
	return time.Since(a.StateSeenAt) > StateSeenBound
}

// IsProvisionerDisabled reports whether the agent has been told to keep the directory it was given
// rather than be provisioned one.
func (a Agent) IsProvisionerDisabled() bool {
	return IsProvisionerDisabled(a.ProvisionEnabled)
}

// IsProvisionerDisabled reports whether an override tells an agent to keep the directory it was
// given rather than be provisioned one.
func IsProvisionerDisabled(override *bool) bool {
	return override != nil && !*override
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

type AgentStore struct {
	queries *models.Queries
	db      store.DBTX
}

// NewAgentStore builds an agent store over a pool or transaction.
func NewAgentStore(db store.DBTX) *AgentStore {
	return &AgentStore{queries: models.New(db), db: db}
}

// AgentPane names a pane and the agent that owns it.
type AgentPane struct {
	AgentID string
	Pane    string
}

// FromRows is the agents the stored rows describe, in the order given.
func FromRows(rows []*models.Agent) []Agent {
	return xslice.Map(rows, func(row *models.Agent) Agent {
		return FromRow(*row)
	})
}

// IDs is the id of each agent, in the order they were given.
func IDs(agents []Agent) []string {
	return xslice.Map(agents, func(agent Agent) string { return agent.ID })
}

// FromRow is the agent one stored row describes.
func FromRow(agent models.Agent) Agent {
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
		TurnFailedAt:       agent.TurnFailedAt,
		TurnError:          agent.TurnError,
		ReportedReason:     agent.ReportedReason,
		FailureReason:      agent.FailureReason,
		Harness:            agent.Harness,
		SessionID:          agent.SessionID,
		WorkflowID:         agent.WorkflowID,
		ScheduleID:         agent.ScheduleID,
		IdempotencyKey:     agent.IdempotencyKey,
		ArchivedAt:         agent.ArchivedAt,
		ArchiveBatchID:     agent.ArchiveBatchID,
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
		HeadSHA:            agent.HeadSha,
		AdoptedBranch:      agent.AdoptedBranch,
		WorktreeRemoved:    agent.WorktreeRemoved,
		InterruptedAt:      agent.InterruptedAt,
		SequenceInProgress: agent.SequenceInProgress,
		HeldAt:             agent.HeldAt,
		HeldMidTurn:        agent.HeldMidTurn,
		Alive:              agent.Alive,
		MovedSinceRound:    agent.MovedSinceRound,
	}
}
