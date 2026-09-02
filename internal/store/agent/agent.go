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

const (
	StateThinking        = "thinking"
	StateWaiting         = "waiting"
	StateIdle            = "idle"
	StateSettingUp       = "setting_up"
	StateFailedToCreate  = "failed_to_create"
	StateFailedToCleanup = "failed_to_cleanup"
)

// IsFailed reports whether a state is one of the two an agent reaches by failing.
func IsFailed(state string) bool {
	return state == StateFailedToCreate || state == StateFailedToCleanup
}

type Agent struct {
	ID                string
	Name              string
	Pane              string
	Cwd               string
	Flags             string
	ProvisionAction   *string
	DeprovisionAction *string
	Prompt            string
	State             string
	StateChangedAt    time.Time
	StateSeenAt       time.Time
	// ViewedAt is how much of an agent's conversation the person has read, measured against
	// the last turn the agent reported ending rather than against the clock.
	ViewedAt *time.Time
	// TurnEndedAt is when the agent's own CLI last reported a turn of its ending, which is
	// the only thing that ever moves it.
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
		ID:                agent.ID,
		Name:              agent.Name,
		Pane:              agent.PaneID,
		Cwd:               agent.Cwd,
		Flags:             agent.Flags,
		ProvisionAction:   agent.ProvisionAction,
		DeprovisionAction: agent.DeprovisionAction,
		Prompt:            agent.Prompt,
		State:             agent.State,
		StateChangedAt:    agent.StateChangedAt,
		StateSeenAt:       agent.StateSeenAt,
		ViewedAt:          agent.ViewedAt,
		TurnEndedAt:       agent.TurnEndedAt,
		Harness:           agent.Harness,
		SessionID:         agent.SessionID,
		WorkflowID:        agent.WorkflowID,
		ScheduleID:        agent.ScheduleID,
		IdempotencyKey:    agent.IdempotencyKey,
		ArchivedAt:        agent.ArchivedAt,
		NudgedAt:          agent.NudgedAt,
		Hidden:            agent.IsHidden,
		Tag:               agent.Tag,
		SpawnedByAgentID:  agent.SpawnedByAgentID,
		ParentAgentID:     agent.ParentAgentID,
		CreatedAt:         agent.CreatedAt,
		UpdatedAt:         agent.UpdatedAt,
		Branch:            agent.Branch,
		AdoptedBranch:     agent.AdoptedBranch,
		WorktreeRemoved:   agent.WorktreeRemoved,
		InterruptedAt:     agent.InterruptedAt,
	}
}
