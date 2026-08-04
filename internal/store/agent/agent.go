// Package agent persists background agents.
package agent

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"weave/db"
	"weave/db/models"
	"weave/internal/store"
	"weave/lib/xslice"

	"github.com/jackc/pgx/v5"
)

// ErrTooManyMetadataKeys reports that an agent has reached its metadata limit.
var ErrTooManyMetadataKeys = errors.New("too many metadata keys")

// Bound agent-controlled keys to prevent an action loop from filling the table.
const maxMetadataKeys = 32

// This advisory lock key is stable and reserved for agent slot claims.
const slotsLock int64 = 7_240_116

const (
	StateThinking = "thinking"
	StateWaiting  = "waiting"
	StateIdle     = "idle"
)

// Metadata is a fact an agent records about its work.
type Metadata struct {
	Key   string
	Value string
}

// Agent is a persisted background agent.
type Agent struct {
	ID     string
	Name   string
	Pane   string
	Cwd    string
	Flags  string
	Prompt string
	State  string
	// StateChangedAt changes only when State changes.
	StateChangedAt time.Time
	Harness        string
	SessionID      string
	WorkflowID     string
	ScheduleID     *string
	IdempotencyKey *string
	ArchivedAt     *time.Time
	Hidden         bool
	Mark           *string
	Metadata       []Metadata
	CreatedAt      time.Time
	UpdatedAt      time.Time
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

// Store reads and writes agents.
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

// List returns every persisted agent, newest first.
func (s *Store) List(ctx context.Context) ([]Agent, error) {
	rows, err := s.queries.ListAgents(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}

	return s.hydrateAgents(ctx, rows)
}

// Cursor locates an agent within a paginated listing.
type Cursor struct {
	ID string `json:"i"`
}

// Validate checks that the cursor contains an agent ID.
func (c Cursor) Validate() error {
	if !store.IsID(c.ID) {
		return fmt.Errorf("cursor id %q is not an id", c.ID)
	}

	return nil
}

// ListPage returns one page of live or archived agents, newest first.
func (s *Store) ListPage(ctx context.Context, limit int64, cursor *Cursor, archived bool) ([]Agent, error) {
	params := models.ListAgentsPageParams{Lim: int32(limit), Archived: archived}
	if cursor != nil {
		params.CursorID = &cursor.ID
	}

	rows, err := s.queries.ListAgentsPage(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list a page of agents: %w", err)
	}

	return s.hydrateAgents(ctx, rows)
}

// hydrateAgents is the single loader for fully hydrated agents, metadata included.
// Every list/get method routes through it so callers never stitch metadata back in themselves.
func (s *Store) hydrateAgents(ctx context.Context, rows []*models.Agent) ([]Agent, error) {
	agents := make([]Agent, 0, len(rows))
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		agents = append(agents, agentFromRow(*row))
		ids = append(ids, row.ID)
	}

	metadata, err := s.metadataByAgent(ctx, ids)
	if err != nil {
		return nil, err
	}

	for i := range agents {
		agents[i].Metadata = metadata[agents[i].ID]
	}

	return agents, nil
}

// hydrateAgent is hydrateAgents for the single-row get paths.
func (s *Store) hydrateAgent(ctx context.Context, row *models.Agent) (*Agent, error) {
	agents, err := s.hydrateAgents(ctx, []*models.Agent{row})
	if err != nil {
		return nil, err
	}

	return &agents[0], nil
}

func (s *Store) metadataByAgent(ctx context.Context, ids []string) (map[string][]Metadata, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	rows, err := s.queries.ListAgentMetadata(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to list agent metadata: %w", err)
	}

	byAgent := make(map[string][]Metadata, len(ids))
	for _, row := range rows {
		byAgent[row.AgentID] = append(byAgent[row.AgentID], Metadata{Key: row.Key, Value: row.Value})
	}

	return byAgent, nil
}

// ListAll returns every agent including archived ones, newest first, for search.
func (s *Store) ListAll(ctx context.Context) ([]Agent, error) {
	rows, err := s.queries.ListAllAgents(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list all agents: %w", err)
	}

	return s.hydrateAgents(ctx, rows)
}

// GetInWorkflow returns a live agent by workflow and name.
func (s *Store) GetInWorkflow(ctx context.Context, workflowID, name string) (*Agent, error) {
	row, err := s.queries.GetAgentInWorkflow(ctx, models.GetAgentInWorkflowParams{
		WorkflowID: workflowID,
		Name:       name,
	})
	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get agent %q in workflow %q: %w", name, workflowID, err)
	}

	return s.hydrateAgent(ctx, row)
}

// GetByID returns a live or archived agent by ID.
func (s *Store) GetByID(ctx context.Context, id string) (*Agent, error) {
	if !store.IsID(id) {
		return nil, nil
	}

	row, err := s.queries.GetAgentByID(ctx, id)
	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get agent %q: %w", id, err)
	}

	return s.hydrateAgent(ctx, row)
}

// ListByID returns live and archived agents matching the given IDs.
func (s *Store) ListByID(ctx context.Context, ids []string) ([]Agent, error) {
	storable := keepStorableIDs(ids)
	if len(storable) == 0 {
		return nil, nil
	}

	rows, err := s.queries.GetAgentsByID(ctx, storable)
	if err != nil {
		return nil, fmt.Errorf("failed to get %d agents: %w", len(storable), err)
	}

	return s.hydrateAgents(ctx, rows)
}

// Exists reports whether any agent holds the given id, including archived ones.
func (s *Store) Exists(ctx context.Context, id string) (bool, error) {
	if !store.IsID(id) {
		return false, nil
	}

	exists, err := s.queries.AgentExists(ctx, id)
	if err != nil {
		return false, fmt.Errorf("failed to check for agent %q: %w", id, err)
	}

	return exists, nil
}

// ActiveNameExists reports whether a live agent holds the name in a workflow.
func (s *Store) ActiveNameExists(ctx context.Context, workflowID, name string) (bool, error) {
	exists, err := s.queries.ActiveNameExists(ctx, models.ActiveNameExistsParams{
		WorkflowID: workflowID,
		Name:       name,
	})
	if err != nil {
		return false, fmt.Errorf("failed to check active name %q in workflow %q: %w", name, workflowID, err)
	}

	return exists, nil
}

// Create inserts an agent and assigns an ID when needed.
func (s *Store) Create(ctx context.Context, agent Agent) error {
	if agent.ID == "" {
		id, err := store.NewID()
		if err != nil {
			return err
		}

		agent.ID = id
	}

	if _, err := s.queries.InsertAgent(ctx, models.InsertAgentParams{
		ID:             agent.ID,
		Name:           agent.Name,
		Cwd:            agent.Cwd,
		Flags:          agent.Flags,
		Prompt:         agent.Prompt,
		PaneID:         agent.Pane,
		State:          agent.State,
		Harness:        agent.Harness,
		SessionID:      agent.SessionID,
		WorkflowID:     agent.WorkflowID,
		ScheduleID:     agent.ScheduleID,
		IdempotencyKey: agent.IdempotencyKey,
		IsHidden:       agent.Hidden,
	}); err != nil {
		return fmt.Errorf("failed to insert agent %q: %w", agent.Name, err)
	}

	return nil
}

// Update writes an agent's editable and runtime fields.
func (s *Store) Update(ctx context.Context, agent Agent) error {
	if err := s.queries.UpdateAgent(ctx, models.UpdateAgentParams{
		ID:         agent.ID,
		Cwd:        agent.Cwd,
		Flags:      agent.Flags,
		Prompt:     agent.Prompt,
		PaneID:     agent.Pane,
		State:      agent.State,
		SessionID:  agent.SessionID,
		WorkflowID: agent.WorkflowID,
	}); err != nil {
		return fmt.Errorf("failed to update agent %q: %w", agent.Name, err)
	}

	return nil
}

// SetStateByID updates an agent's state by ID.
func (s *Store) SetStateByID(ctx context.Context, id, state string) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.SetAgentStateByID(ctx, models.SetAgentStateByIDParams{ID: id, State: state}); err != nil {
		return fmt.Errorf("failed to set state for id %q: %w", id, err)
	}

	return nil
}

// SetSessionID records an agent's conversation when none is already stored.
func (s *Store) SetSessionID(ctx context.Context, id, sessionID string) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.SetAgentSessionID(ctx, models.SetAgentSessionIDParams{ID: id, SessionID: sessionID}); err != nil {
		return fmt.Errorf("failed to set the session for id %q: %w", id, err)
	}

	return nil
}

// ClearPane removes the pane ID stored for an agent.
func (s *Store) ClearPane(ctx context.Context, id string) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.ClearAgentPane(ctx, id); err != nil {
		return fmt.Errorf("failed to clear pane for id %q: %w", id, err)
	}

	return nil
}

// SetMarks updates the mark on the given agents and returns the number changed.
func (s *Store) SetMarks(ctx context.Context, ids []string, mark *string) (int, error) {
	storable := keepStorableIDs(ids)
	if len(storable) == 0 {
		return 0, nil
	}

	updated, err := s.queries.SetAgentMarks(ctx, models.SetAgentMarksParams{Ids: storable, Mark: mark})
	if err != nil {
		return 0, fmt.Errorf("failed to mark %d agents: %w", len(storable), err)
	}

	return len(updated), nil
}

// SetHidden updates a live agent's hidden state.
func (s *Store) SetHidden(ctx context.Context, id string, hidden bool) (bool, error) {
	if !store.IsID(id) {
		return false, nil
	}

	updated, err := s.queries.SetAgentHidden(ctx, models.SetAgentHiddenParams{ID: id, Hidden: hidden})
	if err != nil {
		return false, fmt.Errorf("failed to hide agent %q: %w", id, err)
	}

	return len(updated) > 0, nil
}

// SetMetadataParams identifies a metadata value to set or clear.
type SetMetadataParams struct {
	ID    string
	Key   string
	Value *string
}

// SetMetadata sets or clears one metadata value and reports whether a row changed.
func (s *Store) SetMetadata(ctx context.Context, params SetMetadataParams) (bool, error) {
	if !store.IsID(params.ID) {
		return false, nil
	}

	if params.Value == nil {
		deleted, err := s.queries.DeleteAgentMetadata(ctx, models.DeleteAgentMetadataParams{AgentID: params.ID, Key: params.Key})
		if err != nil {
			return false, fmt.Errorf("failed to clear metadata %q on agent %q: %w", params.Key, params.ID, err)
		}

		return len(deleted) > 0, nil
	}

	beginner, ok := s.db.(txBeginner)
	if !ok {
		return false, fmt.Errorf("agent store has no transaction-capable connection to record metadata %q on agent %q", params.Key, params.ID)
	}

	tx, err := beginner.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to begin metadata transaction for agent %q: %w", params.ID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := models.New(tx)

	// Locking here, before counting, is what keeps two callers writing different new keys
	// from both reading the same count and both slipping past the limit.
	if err := queries.LockAgentMetadata(ctx, params.ID); err != nil {
		return false, fmt.Errorf("failed to lock metadata for agent %q: %w", params.ID, err)
	}

	// Existing keys remain writable after the agent reaches the key limit.
	held, err := queries.ListAgentMetadata(ctx, []string{params.ID})
	if err != nil {
		return false, fmt.Errorf("failed to read metadata on agent %q: %w", params.ID, err)
	}

	revising := slices.ContainsFunc(held, func(row *models.ListAgentMetadataRow) bool {
		return row.Key == params.Key
	})
	if len(held) >= maxMetadataKeys && !revising {
		return false, ErrTooManyMetadataKeys
	}

	written, err := queries.SetAgentMetadata(ctx, models.SetAgentMetadataParams{
		AgentID: params.ID,
		Key:     params.Key,
		Value:   *params.Value,
	})
	if err != nil {
		return false, fmt.Errorf("failed to record metadata %q on agent %q: %w", params.Key, params.ID, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("failed to commit metadata for agent %q: %w", params.ID, err)
	}

	return len(written) > 0, nil
}

// GetByIdempotencyKey returns the live or archived agent recorded under a key.
func (s *Store) GetByIdempotencyKey(ctx context.Context, key string) (*Agent, error) {
	if key == "" {
		return nil, nil
	}

	row, err := s.queries.GetAgentByIdempotencyKey(ctx, &key)
	if db.IsNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get the agent for idempotency key %q: %w", key, err)
	}

	return s.hydrateAgent(ctx, row)
}

// CountLive returns the number of unarchived agents.
func (s *Store) CountLive(ctx context.Context) (int64, error) {
	count, err := s.queries.CountLiveAgents(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to count the live agents: %w", err)
	}

	return count, nil
}

// LockSlots serializes agent slot claims for the current transaction.
func (s *Store) LockSlots(ctx context.Context) error {
	if err := s.queries.LockAgentSlots(ctx, slotsLock); err != nil {
		return fmt.Errorf("failed to lock the agent slots: %w", err)
	}

	return nil
}

// CountLiveForSchedule returns the number of non-idle agents launched by a schedule.
func (s *Store) CountLiveForSchedule(ctx context.Context, scheduleID string) (int64, error) {
	if !store.IsID(scheduleID) {
		return 0, nil
	}

	count, err := s.queries.CountLiveScheduleAgents(ctx, &scheduleID)
	if err != nil {
		return 0, fmt.Errorf("failed to count the live agents of schedule %q: %w", scheduleID, err)
	}

	return count, nil
}

// Delete removes live or archived agents by ID.
func (s *Store) Delete(ctx context.Context, ids []string) error {
	storable := keepStorableIDs(ids)
	if len(storable) == 0 {
		return nil
	}

	if err := s.queries.DeleteAgentsByID(ctx, storable); err != nil {
		return fmt.Errorf("failed to delete %d agents: %w", len(storable), err)
	}

	return nil
}

// Rename changes an agent's display name and reports whether a row changed.
func (s *Store) Rename(ctx context.Context, id, name string) (bool, error) {
	if !store.IsID(id) {
		return false, nil
	}

	renamed, err := s.queries.RenameAgent(ctx, models.RenameAgentParams{ID: id, Name: name})
	if db.IsUniqueViolation(err) {
		return false, store.ErrNameTaken
	}
	if err != nil {
		return false, fmt.Errorf("failed to rename agent %q: %w", id, err)
	}

	return len(renamed) > 0, nil
}

// Archive moves agents out of live listings.
func (s *Store) Archive(ctx context.Context, ids []string) error {
	storable := keepStorableIDs(ids)
	if len(storable) == 0 {
		return nil
	}

	if err := s.queries.MarkAgentsArchived(ctx, storable); err != nil {
		return fmt.Errorf("failed to archive %d agents: %w", len(storable), err)
	}

	return nil
}

// Restore returns archived agents to live listings.
func (s *Store) Restore(ctx context.Context, ids []string) error {
	storable := keepStorableIDs(ids)
	if len(storable) == 0 {
		return nil
	}

	if err := s.queries.RestoreAgents(ctx, storable); err != nil {
		return fmt.Errorf("failed to restore %d agents: %w", len(storable), err)
	}

	return nil
}

// ArchiveWorkflowAgents archives every live agent in a workflow.
func (s *Store) ArchiveWorkflowAgents(ctx context.Context, workflowID string) error {
	if err := s.queries.MarkWorkflowAgentsArchived(ctx, workflowID); err != nil {
		return fmt.Errorf("failed to archive agents of workflow %q: %w", workflowID, err)
	}

	return nil
}

// RestoreWorkflowAgents restores every archived agent in a workflow.
func (s *Store) RestoreWorkflowAgents(ctx context.Context, workflowID string) error {
	if err := s.queries.MarkWorkflowAgentsRestored(ctx, workflowID); err != nil {
		return fmt.Errorf("failed to restore agents of workflow %q: %w", workflowID, err)
	}

	return nil
}

// ListLiveInWorkflow returns the live agents in a workflow.
func (s *Store) ListLiveInWorkflow(ctx context.Context, workflowID string) ([]Agent, error) {
	rows, err := s.queries.ListLiveWorkflowAgents(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to list live agents of workflow %q: %w", workflowID, err)
	}

	return s.hydrateAgents(ctx, rows)
}

// ListArchivedInWorkflow returns the archived agents in a workflow.
func (s *Store) ListArchivedInWorkflow(ctx context.Context, workflowID string) ([]Agent, error) {
	rows, err := s.queries.ListArchivedWorkflowAgents(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to list archived agents of workflow %q: %w", workflowID, err)
	}

	return s.hydrateAgents(ctx, rows)
}

// PostgreSQL rejects the entire UUID array when any element is invalid.
func keepStorableIDs(ids []string) []string {
	return xslice.Filter(ids, store.IsID)
}

func agentFromRow(agent models.Agent) Agent {
	return Agent{
		ID:             agent.ID,
		Name:           agent.Name,
		Pane:           agent.PaneID,
		Cwd:            agent.Cwd,
		Flags:          agent.Flags,
		Prompt:         agent.Prompt,
		State:          agent.State,
		StateChangedAt: agent.StateChangedAt,
		Harness:        agent.Harness,
		SessionID:      agent.SessionID,
		WorkflowID:     agent.WorkflowID,
		ScheduleID:     agent.ScheduleID,
		IdempotencyKey: agent.IdempotencyKey,
		ArchivedAt:     agent.ArchivedAt,
		Hidden:         agent.IsHidden,
		Mark:           agent.Mark,
		CreatedAt:      agent.CreatedAt,
		UpdatedAt:      agent.UpdatedAt,
	}
}
