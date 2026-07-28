// Package agent persists the agents themselves, the agents table and the
// per-agent config row that hangs off it.
//
// It owns no other domain's tables. An agent's transcript belongs to the
// transcript package and the workflow an agent sits in belongs to the workflow
// package, so an operation touching both is composed by a caller inside one
// transaction rather than reached for from here.
package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"claude-agents/db/models"
	"claude-agents/internal/store"

	"github.com/jackc/pgx/v5/pgconn"
)

// Agent lifecycle states.
const (
	StateNew      = "new"
	StateThinking = "thinking"
	StateWaiting  = "waiting"
	StateDone     = "done"
)

// Agent is a persisted background agent.
type Agent struct {
	ID   string
	Name string
	// AgentConfigID is this agent's own config row, holding its cwd and flags.
	// Every agent owns exactly one. Cwd and Flags are that row flattened on read.
	AgentConfigID   string
	Pane            string
	Cwd             string
	Flags           string
	Prompt          string
	State           string
	ClaudeSessionID string
	// WorkflowID is the workflow this agent belongs to, and every agent has one,
	// since the workflow is where the directory, flags, and shared context it starts
	// from come from. It holds the workflow's stable id, not its display name.
	WorkflowID string
	// ScheduleID is the schedule that launched this agent, or nil for one a person
	// started. It is provenance only, and nothing about the agent depends on it.
	ScheduleID *string
	// IdempotencyKey is the caller's claim that this agent is the one identified by
	// that key, nil for a create that makes no such claim. A retried launch passing
	// the same key finds this agent rather than spawning a second one.
	IdempotencyKey *string
	Deleted        bool
	// MarkedAt is when the user marked this agent, nil when they have not. Only the
	// user sets and clears it, so it says nothing about the agent's lifecycle.
	MarkedAt *time.Time
	// CreatedAt is when the agent was first recorded, for display.
	CreatedAt time.Time
	// UpdatedAt is when the row last changed, for display. No listing orders by it,
	// since a key that every write moves reshuffles the list under the reader.
	UpdatedAt time.Time
}

// SanitizeName maps a name to the characters tmux allows in a session name,
// which is also the store key so the two always agree.
func SanitizeName(name string) string {
	return strings.NewReplacer(":", "-", ".", "-", " ", "-").Replace(name)
}

// Store reads and writes agents.
type Store struct {
	queries *models.Queries
}

// New builds an agent store over db, which is either the pool or a transaction. A
// caller composing this with another domain passes the transaction, so both land
// together.
func New(db store.DBTX) *Store {
	return &Store{queries: models.New(db)}
}

// List returns every persisted agent, newest first.
func (s *Store) List(ctx context.Context) ([]Agent, error) {
	rows, err := s.queries.ListAgents(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}

	agents := make([]Agent, 0, len(rows))
	for _, row := range rows {
		agents = append(agents, agentFromRow(row.Agent, row.Cwd, row.Flags))
	}

	return agents, nil
}

// Cursor locates an agent within a listing. The id alone is enough, since it is
// unique and never changes, so the order it gives is total and a page resumes
// exactly where the last one ended.
type Cursor struct {
	ID string `json:"i"`
}

// ListPage returns one page of live agents, newest first, resuming after cursor
// when one is given. The key is the id rather than anything the agent can change.
// A uuidv7 is assigned once and sorts by the moment it was assigned, so the newest
// agent goes to the top and stays there. Keying on update time would move an agent
// the reconciler touched mid-scan ahead of where the caller already is, and it
// would be seen twice or missed.
func (s *Store) ListPage(ctx context.Context, limit int64, cursor *Cursor) ([]Agent, error) {
	params := models.ListAgentsPageParams{Lim: int32(limit)}
	if cursor != nil {
		params.CursorID = &cursor.ID
	}

	rows, err := s.queries.ListAgentsPage(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list a page of agents: %w", err)
	}

	agents := make([]Agent, 0, len(rows))
	for _, row := range rows {
		agents = append(agents, agentFromRow(row.Agent, row.Cwd, row.Flags))
	}

	return agents, nil
}

// ListAll returns every agent including soft-deleted ones, newest first. It
// backs search, which spans active and deleted agents.
func (s *Store) ListAll(ctx context.Context) ([]Agent, error) {
	rows, err := s.queries.ListAllAgents(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list all agents: %w", err)
	}

	agents := make([]Agent, 0, len(rows))
	for _, row := range rows {
		agents = append(agents, agentFromRow(row.Agent, row.Cwd, row.Flags))
	}

	return agents, nil
}

// Get returns an agent by name, nil when none exists, or an error for a real
// failure. No-rows is the nil case. Every other error is surfaced so a caller does
// not report a genuine DB failure as not found.
func (s *Store) Get(ctx context.Context, name string) (*Agent, error) {
	row, err := s.queries.GetAgent(ctx, name)
	if store.NoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get agent %q: %w", name, err)
	}

	agent := agentFromRow(row.Agent, row.Cwd, row.Flags)
	return &agent, nil
}

// Exists reports whether any agent holds the given id, including soft-deleted
// ones, so a caller can tell an unknown id from an agent that has nothing stored
// yet.
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

// ActiveNameExists reports whether a live, non-deleted agent already holds the
// name. New agents consult this so an auto-generated name never collides with an
// agent whose row still owns it, including a dormant one whose tmux session is
// gone but whose record survives.
func (s *Store) ActiveNameExists(ctx context.Context, name string) (bool, error) {
	exists, err := s.queries.ActiveNameExists(ctx, name)
	if err != nil {
		return false, fmt.Errorf("failed to check active name %q: %w", name, err)
	}

	return exists, nil
}

// Upsert inserts a new agent or updates an existing one. A blank id means a fresh
// agent, whose config row is inserted alongside it. A set id means an existing
// agent whose linked config and runtime fields are updated in place, never
// inserting a second config row.
//
// Both halves must land together, so a caller outside a transaction gets one
// through InTx. Inside one, this joins whatever is already open.
func (s *Store) Upsert(ctx context.Context, agent Agent) error {
	if agent.ID == "" {
		return s.insert(ctx, agent)
	}

	return s.update(ctx, agent)
}

// insert creates a fresh agent and its own config row. The database generates both
// ids, so the config is inserted first and the agent points at what it returned.
func (s *Store) insert(ctx context.Context, agent Agent) error {
	configID, err := s.queries.InsertAgentConfig(ctx, models.InsertAgentConfigParams{
		Cwd:   agent.Cwd,
		Flags: agent.Flags,
	})
	if err != nil {
		return fmt.Errorf("failed to insert config for %q: %w", agent.Name, err)
	}

	if _, err := s.queries.InsertAgent(ctx, models.InsertAgentParams{
		Name:            agent.Name,
		AgentConfigID:   configID,
		Prompt:          agent.Prompt,
		PaneID:          agent.Pane,
		State:           agent.State,
		ClaudeSessionID: agent.ClaudeSessionID,
		WorkflowID:      agent.WorkflowID,
		ScheduleID:      agent.ScheduleID,
		IdempotencyKey:  agent.IdempotencyKey,
	}); err != nil {
		return fmt.Errorf("failed to insert agent %q: %w", agent.Name, err)
	}

	return nil
}

// update rewrites an existing agent's linked config and runtime fields, keyed by
// its stable id.
func (s *Store) update(ctx context.Context, agent Agent) error {
	if agent.AgentConfigID != "" {
		if err := s.queries.UpdateAgentConfig(ctx, models.UpdateAgentConfigParams{
			ID:    agent.AgentConfigID,
			Cwd:   agent.Cwd,
			Flags: agent.Flags,
		}); err != nil {
			return fmt.Errorf("failed to update config for %q: %w", agent.Name, err)
		}
	}

	if err := s.queries.UpdateAgent(ctx, models.UpdateAgentParams{
		ID:              agent.ID,
		Prompt:          agent.Prompt,
		PaneID:          agent.Pane,
		State:           agent.State,
		ClaudeSessionID: agent.ClaudeSessionID,
		WorkflowID:      agent.WorkflowID,
	}); err != nil {
		return fmt.Errorf("failed to update agent %q: %w", agent.Name, err)
	}

	return nil
}

// SetState updates just an agent's state, keyed by name. It reports whether a row
// was updated, so naming an agent that does not exist is distinguishable from
// setting a state it already had. The query returns the ids it touched, so the
// count is known without a second read.
func (s *Store) SetState(ctx context.Context, name, state string) (bool, error) {
	updated, err := s.queries.SetAgentState(ctx, models.SetAgentStateParams{Name: name, State: state})
	if err != nil {
		return false, fmt.Errorf("failed to set state for %q: %w", name, err)
	}

	return len(updated) > 0, nil
}

// SetStateByID updates just an agent's state, keyed by its stable id. The
// reconciler uses this so a write decided from a snapshot always lands on the
// exact row it observed, even if that row's name was reused by another agent in
// the meantime.
func (s *Store) SetStateByID(ctx context.Context, id, state string) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.SetAgentStateByID(ctx, models.SetAgentStateByIDParams{ID: id, State: state}); err != nil {
		return fmt.Errorf("failed to set state for id %q: %w", id, err)
	}

	return nil
}

// SetMarked marks or unmarks an agent, keyed by id. Marking stamps the moment it
// happened and unmarking clears the stamp. The database supplies the clock so two
// writers cannot disagree about now. It reports whether a row was touched, which is
// how a caller tells an id that matches nothing from one that does.
func (s *Store) SetMarked(ctx context.Context, id string, marked bool) (bool, error) {
	if !store.IsID(id) {
		return false, nil
	}

	updated, err := s.queries.SetAgentMarked(ctx, models.SetAgentMarkedParams{ID: id, Marked: marked})
	if err != nil {
		return false, fmt.Errorf("failed to mark agent %q: %w", id, err)
	}

	return len(updated) > 0, nil
}

// Target names the rows a hard delete has to remove. The agent itself, the config
// it owns, and the transcript keyed to its id. The transcript is another package's
// table, so this reports the id rather than deleting it.
type Target struct {
	ID            string
	AgentConfigID string
}

// GetByIdempotencyKey returns the agent recorded under a caller's key, or nil
// when none is. It spans archived agents deliberately. The key exists so a retry
// finds what it already created, and an agent archived since is still that agent,
// so returning nil here would spawn a duplicate.
func (s *Store) GetByIdempotencyKey(ctx context.Context, key string) (*Agent, error) {
	if key == "" {
		return nil, nil
	}

	row, err := s.queries.GetAgentByIdempotencyKey(ctx, &key)
	if store.NoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get the agent for idempotency key %q: %w", key, err)
	}

	agent := agentFromRow(row.Agent, row.Cwd, row.Flags)

	return &agent, nil
}

// CountLiveForSchedule reports how many agents a schedule has that have not
// finished, which is what tells a firing that the last one is still going.
//
// Anything not done counts, including an agent sitting on a confirmation prompt:
// it is unfinished work someone still has to answer, and starting a second run
// beside it would only add another.
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

// Find resolves an agent by name to the ids a delete needs, nil when no agent has
// that name.
func (s *Store) Find(ctx context.Context, name string) (*Target, error) {
	row, err := s.queries.GetAgentForDelete(ctx, name)
	if store.NoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to find agent %q: %w", name, err)
	}

	return &Target{ID: row.ID, AgentConfigID: row.AgentConfigID}, nil
}

// Delete removes an agent and the config it owns. The transcript keyed to the same
// id is the transcript package's to remove, in the same transaction, since an
// agent deleted without its transcript is a half-state worth avoiding.
func (s *Store) Delete(ctx context.Context, target Target) error {
	if err := s.queries.DeleteAgentByID(ctx, target.ID); err != nil {
		return fmt.Errorf("failed to delete agent %q: %w", target.ID, err)
	}

	if err := s.queries.DeleteAgentConfig(ctx, target.AgentConfigID); err != nil {
		return fmt.Errorf("failed to delete config for %q: %w", target.ID, err)
	}

	return nil
}

// ErrNameTaken reports that a rename collided with a live agent's name. The
// unique index is scoped to live rows, so this only ever means an agent currently
// listed answers to that name.
var ErrNameTaken = errors.New("name already taken")

// uniqueViolation is the SQLSTATE Postgres raises for a broken unique constraint,
// which is how a taken name is told apart from a real failure without reading the
// driver's message text.
const uniqueViolation = "23505"

// Rename changes an agent's display name, keyed by id. It reports whether a row
// was renamed, so an id that matches nothing is distinguishable from a rename to
// the name an agent already had.
func (s *Store) Rename(ctx context.Context, id, name string) (bool, error) {
	if !store.IsID(id) {
		return false, nil
	}

	renamed, err := s.queries.RenameAgent(ctx, models.RenameAgentParams{ID: id, Name: name})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return false, ErrNameTaken
		}

		return false, fmt.Errorf("failed to rename agent %q: %w", id, err)
	}

	return len(renamed) > 0, nil
}

// Archive hides an agent from listings without dropping the row.
func (s *Store) Archive(ctx context.Context, name string) error {
	if err := s.queries.MarkAgentDeleted(ctx, name); err != nil {
		return fmt.Errorf("failed to archive agent %q: %w", name, err)
	}

	return nil
}

// Restore clears an agent's archived flag by id, so an agent found in search
// returns to the normal listing. It is keyed by id, not name, because an
// archived row can share a name with a live agent, so the id is the only
// unambiguous handle to the exact row to restore.
func (s *Store) Restore(ctx context.Context, id string) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.RestoreAgent(ctx, id); err != nil {
		return fmt.Errorf("failed to restore agent %q: %w", id, err)
	}

	return nil
}

// MoveToWorkflow reassigns an agent to another workflow, keyed by the agent's
// id. There is nowhere to move an agent to but a workflow, so this replaces one
// with another rather than being able to clear it.
func (s *Store) MoveToWorkflow(ctx context.Context, id, workflowID string) error {
	if !store.IsID(id) {
		return nil
	}

	if err := s.queries.SetAgentWorkflow(ctx, models.SetAgentWorkflowParams{ID: id, WorkflowID: workflowID}); err != nil {
		return fmt.Errorf("failed to move agent %q to workflow: %w", id, err)
	}

	return nil
}

// ArchiveWorkflowAgents archives every live agent in a workflow, keeping their
// workflow id so they still list under it in search.
func (s *Store) ArchiveWorkflowAgents(ctx context.Context, workflowID string) error {
	if err := s.queries.MarkWorkflowAgentsDeleted(ctx, workflowID); err != nil {
		return fmt.Errorf("failed to archive agents of workflow %q: %w", workflowID, err)
	}

	return nil
}

// agentFromRow builds an Agent from an agent row and the config joined onto it.
// Every agent read selects sqlc.embed(a), so they all carry the same models.Agent
// whatever else their row type holds, leaving the cwd and flags as the only loose
// columns to pass.
func agentFromRow(agent models.Agent, cwd, flags string) Agent {
	return Agent{
		ID:              agent.ID,
		Name:            agent.Name,
		AgentConfigID:   agent.AgentConfigID,
		Pane:            agent.PaneID,
		Cwd:             cwd,
		Flags:           flags,
		Prompt:          agent.Prompt,
		State:           agent.State,
		ClaudeSessionID: agent.ClaudeSessionID,
		WorkflowID:      agent.WorkflowID,
		ScheduleID:      agent.ScheduleID,
		IdempotencyKey:  agent.IdempotencyKey,
		Deleted:         agent.IsDeleted,
		MarkedAt:        agent.MarkedAt,
		CreatedAt:       agent.CreatedAt,
		UpdatedAt:       agent.UpdatedAt,
	}
}
