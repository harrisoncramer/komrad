package agent

import (
	"strings"
	"time"

	"komrad/db/models"
	"komrad/internal/store"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"

	"github.com/google/uuid"
)

// stateSeenBound is how long a reading of an agent's state stands before it is unconfirmed.
const stateSeenBound = 2 * time.Minute

// DoesPaneDecide reports whether reading the agent's terminal is what decides this state.
func DoesPaneDecide(state agentv1.AgentState) bool {
	switch state {
	case agentv1.AgentState_AGENT_STATE_IDLE, agentv1.AgentState_AGENT_STATE_THINKING, agentv1.AgentState_AGENT_STATE_WAITING,
		agentv1.AgentState_AGENT_STATE_UNCERTAIN, agentv1.AgentState_AGENT_STATE_ERRORED:
		return true
	case agentv1.AgentState_AGENT_STATE_SETTING_UP, agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE,
		agentv1.AgentState_AGENT_STATE_FAILED_TO_CLEANUP, agentv1.AgentState_AGENT_STATE_UNSPECIFIED:
		return false
	}

	return false
}

// IsTurnUnfinished reports whether a state leaves a turn part-way through.
func IsTurnUnfinished(state agentv1.AgentState) bool {
	return state == agentv1.AgentState_AGENT_STATE_THINKING || state == agentv1.AgentState_AGENT_STATE_WAITING
}

type Agent struct {
	ID    uuid.UUID
	Name  string
	Pane  string
	Cwd   string
	Flags string
	// ProvisionEnabled overrides what the workflow does about the agent's directory: unset
	// inherits it, and false means this agent keeps the directory it was given.
	ProvisionEnabled *bool
	Prompt           string

	// Role is what the agent was launched to do, and decides which review briefing it is sent.
	Role           agentv1.AgentRole
	State          agentv1.AgentState
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
	WorkflowID     uuid.UUID
	ScheduleID     *uuid.UUID
	IdempotencyKey *string
	ArchivedAt     *time.Time

	// ArchiveBatchID is the archive that took this agent, shared by every agent it took.
	ArchiveBatchID     *uuid.UUID
	DeletingAt         *time.Time
	SpawnedByAgentID   *uuid.UUID
	RequestedByAgentID *uuid.UUID

	// TaskID is the task the agent works on, nil only for an orchestrator.
	TaskID        *uuid.UUID
	CreatedAt     time.Time
	UpdatedAt     time.Time
	InterruptedAt *time.Time

	// Alive is whether the agent still owns the pane its row names.
	Alive bool

	// Hooks is the hooks the agent's latest launch installed, empty when that launch failed or never ran.
	Hooks []string
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

// IsStateUnconfirmed reports whether the agent's state was last read longer ago than the bound.
func (a Agent) IsStateUnconfirmed() bool {
	return time.Since(a.StateSeenAt) > stateSeenBound
}

// IsProvisionerDisabled reports whether the agent has been told to keep the directory it was given
// rather than be provisioned one.
func (a Agent) IsProvisionerDisabled() bool {
	return IsProvisionerDisabled(a.ProvisionEnabled)
}

// IsWriter reports whether the agent is its task's writer.
func (a Agent) IsWriter() bool {
	return a.Role == agentv1.AgentRole_AGENT_ROLE_WRITER
}

// roleOrDefault is the agent's role, or the writer's when it names none.
func (a Agent) roleOrDefault() agentv1.AgentRole {
	if a.Role == agentv1.AgentRole_AGENT_ROLE_UNSPECIFIED {
		return agentv1.AgentRole_AGENT_ROLE_WRITER
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
	AgentID uuid.UUID
	Pane    string
}

// fromRows is the agents the stored rows describe, in the order given.
func fromRows(rows []*models.Agent) ([]Agent, error) {
	return xslice.TryMap(rows, func(row *models.Agent) (Agent, error) {
		agent, err := FromRow(*row)
		if err != nil {
			return Agent{}, err
		}

		return *agent, nil
	})
}

// IDs returns the id of each agent, in the order given.
func IDs(agents []Agent) []uuid.UUID {
	return xslice.Map(agents, func(agent Agent) uuid.UUID { return agent.ID })
}

// TaskIDs is the tasks the agents work on, each once.
func TaskIDs(agents []Agent) []uuid.UUID {
	linked := xslice.Filter(agents, func(agent Agent) bool { return agent.TaskID != nil })

	ids := xslice.Map(linked, func(agent Agent) uuid.UUID { return *agent.TaskID })

	return xslice.UniqueBy(ids, func(id uuid.UUID) uuid.UUID { return id })
}

// FromRow is the agent one stored row describes.
func FromRow(agent models.Agent) (*Agent, error) {
	role, err := fromDBToServiceAgentRole(agent.Role)
	if err != nil {
		return nil, err
	}
	state, err := fromDBToServiceAgentState(agent.State)
	if err != nil {
		return nil, err
	}

	return &Agent{
		ID:               agent.ID,
		Name:             agent.Name,
		Pane:             agent.PaneID,
		Cwd:              agent.Cwd,
		Flags:            agent.Flags,
		ProvisionEnabled: agent.ProvisionEnabled,
		Prompt:           agent.Prompt,
		Role:             role,
		State:            state,
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
		SpawnedByAgentID:   agent.SpawnedByAgentID,
		RequestedByAgentID: agent.RequestedByAgentID,
		TaskID:             agent.TaskID,
		CreatedAt:          agent.CreatedAt,
		UpdatedAt:          agent.UpdatedAt,
		InterruptedAt:      agent.InterruptedAt,
		Alive:              agent.Alive,
		Hooks:              agent.Hooks,
	}, nil
}
