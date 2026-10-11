// Package agent stores the agents themselves: the row for each one, the state the reconciler
// keeps up to date, and whether it is archived or still on screen.
package agent

import (
	"fmt"
	"strings"
	"time"

	"komrad/db/models"
	"komrad/internal/agenterr"
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
	RoleWriter       Role = Role(models.AgentRoleWriter)
	RoleReviewer     Role = Role(models.AgentRoleReviewer)
	RoleOrchestrator Role = Role(models.AgentRoleOrchestrator)
)

// Roles is every role an agent may hold.
var Roles = []Role{RoleWriter, RoleReviewer, RoleOrchestrator}

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
	Turn           Turn

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
	NudgeSentAt        *time.Time
	SpawnedByAgentID   *string
	RequestedByAgentID *string

	// ParentAgentID is the agent this one is attached to, whose archive, delete and restore it
	// follows.
	ParentAgentID *string

	// TaskID is the task the agent works on, nil only for an orchestrator.
	TaskID        *string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	InterruptedAt *time.Time
	Hold          Hold

	// Alive is whether the agent still owns the pane its row names.
	Alive bool
}

// Turn is how the agent's last turn ended.
type Turn struct {
	// EndedAt is when a turn of the agent's last ended, reported by its own CLI or, when
	// nothing reported one, settled off the pane by the reconciler.
	EndedAt  *time.Time
	FailedAt *time.Time
	Error    string
}

// IsFailed reports whether an API error ended the agent's last turn.
func (t Turn) IsFailed() bool {
	if t.FailedAt == nil {
		return false
	}

	return t.EndedAt == nil || !t.FailedAt.Before(*t.EndedAt)
}

// Hold is who stopped the agent and took its worktree.
type Hold struct {
	// At is when a person stopped this agent and took its worktree, nil while nobody holds it.
	At *time.Time
	// MidTurn says the hold cut a turn that was still running.
	MidTurn bool
	// ByAgentID is the agent that took the hold, nil when the panel took it.
	ByAgentID *string
}

// IsHeld reports whether somebody is holding the agent.
func (h Hold) IsHeld() bool {
	return h.At != nil
}

// ValidateActionable refuses an agent that is archived or that a person is holding.
func (a Agent) ValidateActionable() error {
	if a.ArchivedAt != nil {
		return fmt.Errorf("%w: %q", agenterr.ErrAgentArchived, a.Name)
	}

	if a.Hold.IsHeld() {
		return agenterr.ErrAgentHeld.Saying("%q is paused, unpause it first", a.Name)
	}

	return nil
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

// IsWriter reports whether the agent is its task's writer.
func (a Agent) IsWriter() bool {
	return a.Role == RoleWriter
}

// roleOrDefault is the agent's role, or the writer's when it names none.
func (a Agent) roleOrDefault() Role {
	if a.Role == "" {
		return RoleWriter
	}

	return a.Role
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

// fromRows is the agents the stored rows describe, in the order given.
func fromRows(rows []*models.Agent) []Agent {
	return xslice.Map(rows, func(row *models.Agent) Agent {
		return FromRow(*row)
	})
}

// IDs is the id of each agent, in the order they were given.
func IDs(agents []Agent) []string {
	return xslice.Map(agents, func(agent Agent) string { return agent.ID })
}

// TaskIDs is the tasks the agents work on, each once.
func TaskIDs(agents []Agent) []string {
	linked := xslice.Filter(agents, func(agent Agent) bool { return agent.TaskID != nil })

	ids := xslice.Map(linked, func(agent Agent) string { return *agent.TaskID })

	return xslice.UniqueBy(ids, func(id string) string { return id })
}

// FromRow is the agent one stored row describes.
func FromRow(agent models.Agent) Agent {
	return Agent{
		ID:               agent.ID,
		Name:             agent.Name,
		Pane:             agent.PaneID,
		Cwd:              agent.Cwd,
		Flags:            agent.Flags,
		ProvisionEnabled: agent.ProvisionEnabled,
		Prompt:           agent.Prompt,
		Role:             Role(agent.Role),
		State:            State(agent.State),
		StateChangedAt:   agent.StateChangedAt,
		StateSeenAt:      agent.StateSeenAt,
		Turn: Turn{
			EndedAt:  agent.TurnEndedAt,
			FailedAt: agent.TurnFailedAt,
			Error:    agent.TurnError,
		},
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
		SpawnedByAgentID:   agent.SpawnedByAgentID,
		RequestedByAgentID: agent.RequestedByAgentID,
		ParentAgentID:      agent.ParentAgentID,
		TaskID:             agent.TaskID,
		CreatedAt:          agent.CreatedAt,
		UpdatedAt:          agent.UpdatedAt,
		InterruptedAt:      agent.InterruptedAt,
		Hold: Hold{
			At:        agent.HeldAt,
			MidTurn:   agent.HeldMidTurn,
			ByAgentID: agent.HeldByAgentID,
		},
		Alive: agent.Alive,
	}
}
