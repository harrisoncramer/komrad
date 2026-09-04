// Package agent stores the agents themselves: the row for each one, the state the reconciler
// keeps up to date, the metadata an agent records about its own work, the tags a person puts on
// a row, and whether it is archived or still on screen.
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

// DoesPaneDecide reports whether a frame read owns this state, which a launch still in flight
// and an agent that failed both refuse, since neither is a pane the reconciler may judge.
func (s State) DoesPaneDecide() bool {
	// Written without a default so that an eighth state fails the exhaustive check here, which
	// forces it to be answered for but not to be given the same answer as the reconciler, the
	// archive sweep, the resume and the row's own icon happen to share today.
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

// IsTurnUnfinished reports whether a state leaves a turn part-way through, which covers both the
// model still working and a tool call whose question nobody answered.
func (s State) IsTurnUnfinished() bool {
	return s == StateThinking || s == StateWaiting
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
	State            State
	StateChangedAt   time.Time
	StateSeenAt      time.Time
	// ViewedAt is how much of an agent's conversation the person has read, measured against
	// the last turn weave watched end rather than against the clock.
	ViewedAt *time.Time
	// TurnEndedAt is when a turn of the agent's last ended, reported by its own CLI or, when
	// nothing reported one, settled off the pane by the reconciler.
	TurnEndedAt      *time.Time
	Harness          string
	SessionID        string
	WorkflowID       string
	ScheduleID       *string
	IdempotencyKey   *string
	ArchivedAt       *time.Time
	NudgedAt         *time.Time
	Hidden           bool
	Tag              *string
	Metadata         []Metadata
	SpawnedByAgentID *string

	// ParentAgentID is the agent this one is attached to, where SpawnedByAgentID is only who
	// called create, so a child has no worktree of its own and is archived, deleted, and
	// restored with that parent rather than by itself.
	ParentAgentID *string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Branch        *string

	// AdoptedBranch is the branch an agent was put on rather than one weave made for it,
	// so it is what was asked for where Branch is what the cwd is on now.
	AdoptedBranch   string
	WorktreeRemoved bool
	InterruptedAt   *time.Time
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

// AgentPane names a pane and the agent whose it is, which is all that killing one takes: the
// owner has to be checked first, so a pane is never carried around without it.
type AgentPane struct {
	AgentID string
	Pane    string
}

func keepStorableIDs(ids []string) []string {
	return xslice.Filter(ids, store.IsID)
}

func agentFromRow(agent models.Agent) Agent {
	return Agent{
		ID:               agent.ID,
		Name:             agent.Name,
		Pane:             agent.PaneID,
		Cwd:              agent.Cwd,
		Flags:            agent.Flags,
		ProvisionEnabled: agent.ProvisionEnabled,
		Prompt:           agent.Prompt,
		State:            State(agent.State),
		StateChangedAt:   agent.StateChangedAt,
		StateSeenAt:      agent.StateSeenAt,
		ViewedAt:         agent.ViewedAt,
		TurnEndedAt:      agent.TurnEndedAt,
		Harness:          agent.Harness,
		SessionID:        agent.SessionID,
		WorkflowID:       agent.WorkflowID,
		ScheduleID:       agent.ScheduleID,
		IdempotencyKey:   agent.IdempotencyKey,
		ArchivedAt:       agent.ArchivedAt,
		NudgedAt:         agent.NudgedAt,
		Hidden:           agent.IsHidden,
		Tag:              agent.Tag,
		SpawnedByAgentID: agent.SpawnedByAgentID,
		ParentAgentID:    agent.ParentAgentID,
		CreatedAt:        agent.CreatedAt,
		UpdatedAt:        agent.UpdatedAt,
		Branch:           agent.Branch,
		AdoptedBranch:    agent.AdoptedBranch,
		WorktreeRemoved:  agent.WorktreeRemoved,
		InterruptedAt:    agent.InterruptedAt,
	}
}
