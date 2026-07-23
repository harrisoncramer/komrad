// Package store persists Claude agents in SQLite via the sqlc-generated
// queries, so they survive tmux exits and restarts.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"claude-agents/db"
	"claude-agents/db/models"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
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
	// SessionConfigID is the id of this agent's own config row, which holds its
	// cwd and flags. Every agent owns exactly one. Cwd and Flags below are the
	// flattened view of that row, joined in on read.
	SessionConfigID string
	Pane            string
	Cwd             string
	Flags           string
	Prompt          string
	State           string
	ClaudeSessionID string
	// WorkflowID is the workflow this chat belongs to, or nil when unassigned.
	// It is the workflow's stable id; the workflow's name is only a display
	// label.
	WorkflowID *string
	Deleted    bool
	// CreatedAt is when the chat was first recorded, for display.
	CreatedAt time.Time
}

// Workflow is a named bucket of related chats. It runs nothing, but it carries
// a default config (Cwd, Flags) used as the blueprint prefilled into the
// new-chat form for chats created inside it.
type Workflow struct {
	ID    string
	Name  string
	Cwd   string
	Flags string
}

// SanitizeName maps a name to the characters tmux allows in a session name,
// which is also the store key so the two always agree.
func SanitizeName(name string) string {
	return strings.NewReplacer(":", "-", ".", "-", " ", "-").Replace(name)
}

// Store is the SQLite-backed agent store.
type Store struct {
	conn    *sql.DB
	queries *models.Queries
}

// Open connects to the SQLite database at path, applying migrations.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create db dir: %w", err)
	}

	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open db: %w", err)
	}

	// WAL plus a busy timeout so the frequent, independent opens (the once-a-second
	// status render, notify calls, the panel, the serve process) don't trip over
	// each other.
	if _, err := conn.Exec("PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;"); err != nil {
		return nil, fmt.Errorf("failed to set pragmas: %w", err)
	}

	if err := db.Migrate(conn); err != nil {
		return nil, fmt.Errorf("failed to migrate db: %w", err)
	}

	return &Store{conn: conn, queries: models.New(conn)}, nil
}

// Close releases the database connection.
func (s *Store) Close() error {
	return s.conn.Close()
}

// List returns every persisted agent, most recently updated first.
func (s *Store) List(ctx context.Context) ([]Agent, error) {
	rows, err := s.queries.ListSessions(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}

	agents := make([]Agent, 0, len(rows))
	for _, row := range rows {
		agents = append(agents, agentFromRow(row.ID, row.Name, row.SessionConfigID, row.PaneID, row.Cwd, row.Flags, row.Prompt, row.State, row.ClaudeSessionID, row.WorkflowID, row.IsDeleted, row.CreatedAt))
	}

	return agents, nil
}

// ListAll returns every agent including soft-deleted ones, most recently
// updated first. It backs search, which spans active and deleted chats.
func (s *Store) ListAll(ctx context.Context) ([]Agent, error) {
	rows, err := s.queries.ListAllSessions(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list all sessions: %w", err)
	}

	agents := make([]Agent, 0, len(rows))
	for _, row := range rows {
		agents = append(agents, agentFromRow(row.ID, row.Name, row.SessionConfigID, row.PaneID, row.Cwd, row.Flags, row.Prompt, row.State, row.ClaudeSessionID, row.WorkflowID, row.IsDeleted, row.CreatedAt))
	}

	return agents, nil
}

// ListByWorkflow returns the live agents in one workflow, most recently updated
// first. It backs the workflow-scoped chats view.
func (s *Store) ListByWorkflow(ctx context.Context, workflowID string) ([]Agent, error) {
	rows, err := s.queries.ListSessionsByWorkflow(ctx, &workflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions for workflow %q: %w", workflowID, err)
	}

	agents := make([]Agent, 0, len(rows))
	for _, row := range rows {
		agents = append(agents, agentFromRow(row.ID, row.Name, row.SessionConfigID, row.PaneID, row.Cwd, row.Flags, row.Prompt, row.State, row.ClaudeSessionID, row.WorkflowID, row.IsDeleted, row.CreatedAt))
	}

	return agents, nil
}

// ListAllByWorkflow returns every agent in one workflow including soft-deleted
// ones. It backs workflow-scoped search, which spans active and deleted chats.
func (s *Store) ListAllByWorkflow(ctx context.Context, workflowID string) ([]Agent, error) {
	rows, err := s.queries.ListAllSessionsByWorkflow(ctx, &workflowID)
	if err != nil {
		return nil, fmt.Errorf("failed to list all sessions for workflow %q: %w", workflowID, err)
	}

	agents := make([]Agent, 0, len(rows))
	for _, row := range rows {
		agents = append(agents, agentFromRow(row.ID, row.Name, row.SessionConfigID, row.PaneID, row.Cwd, row.Flags, row.Prompt, row.State, row.ClaudeSessionID, row.WorkflowID, row.IsDeleted, row.CreatedAt))
	}

	return agents, nil
}

// Upsert inserts a new agent or updates an existing one. A blank id means a
// fresh agent: a session id and a separate config id are generated, and the
// config row is inserted alongside the session in one transaction. A set id
// means an existing agent (the restore path): its linked config and runtime
// fields are updated in place, never inserting a second config row.
func (s *Store) Upsert(ctx context.Context, agent Agent) error {
	if agent.ID == "" {
		return s.insertAgent(ctx, agent)
	}

	return s.updateAgent(ctx, agent)
}

// insertAgent creates a fresh agent and its own config row in one transaction.
func (s *Store) insertAgent(ctx context.Context, agent Agent) error {
	sessionID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("failed to generate session id: %w", err)
	}
	configID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("failed to generate config id: %w", err)
	}

	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin insert-session tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	queries := s.queries.WithTx(tx)
	if err := queries.InsertSessionConfig(ctx, models.InsertSessionConfigParams{
		ID:    configID.String(),
		Cwd:   agent.Cwd,
		Flags: agent.Flags,
	}); err != nil {
		return fmt.Errorf("failed to insert config for %q: %w", agent.Name, err)
	}

	if err := queries.InsertSession(ctx, models.InsertSessionParams{
		ID:              sessionID.String(),
		Name:            agent.Name,
		SessionConfigID: configID.String(),
		Prompt:          agent.Prompt,
		PaneID:          agent.Pane,
		State:           agent.State,
		ClaudeSessionID: agent.ClaudeSessionID,
		WorkflowID:      agent.WorkflowID,
	}); err != nil {
		return fmt.Errorf("failed to insert session %q: %w", agent.Name, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit insert of session %q: %w", agent.Name, err)
	}

	return nil
}

// updateAgent updates an existing agent's linked config and runtime fields in
// one transaction, keyed by its stable id.
func (s *Store) updateAgent(ctx context.Context, agent Agent) error {
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin update-session tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	queries := s.queries.WithTx(tx)
	if agent.SessionConfigID != "" {
		if err := queries.UpdateSessionConfig(ctx, models.UpdateSessionConfigParams{
			ID:    agent.SessionConfigID,
			Cwd:   agent.Cwd,
			Flags: agent.Flags,
		}); err != nil {
			return fmt.Errorf("failed to update config for %q: %w", agent.Name, err)
		}
	}

	if err := queries.UpdateSession(ctx, models.UpdateSessionParams{
		ID:              agent.ID,
		Prompt:          agent.Prompt,
		PaneID:          agent.Pane,
		State:           agent.State,
		ClaudeSessionID: agent.ClaudeSessionID,
		WorkflowID:      agent.WorkflowID,
	}); err != nil {
		return fmt.Errorf("failed to update session %q: %w", agent.Name, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit update of session %q: %w", agent.Name, err)
	}

	return nil
}

// UpdateState records an agent's state and pane on an existing row, keyed by
// name. It never inserts. A notify call from a session the tool did not create
// finds no row and is a no-op, so an outside Claude session never leaks into the
// store. It also never touches cwd/flags/prompt, so a state write does not wipe
// the metadata captured at launch.
func (s *Store) UpdateState(ctx context.Context, name, pane, state, claudeSessionID string) error {
	if err := s.queries.UpdateSessionState(ctx, models.UpdateSessionStateParams{
		Name:            name,
		Pane:            pane,
		State:           state,
		ClaudeSessionID: claudeSessionID,
	}); err != nil {
		return fmt.Errorf("failed to update state for %q: %w", name, err)
	}

	return nil
}

// Get returns an agent by name, nil when none exists, or an error for a real
// failure. No-rows is the nil case; every other error is surfaced so a caller
// does not report a genuine DB failure as "not found".
func (s *Store) Get(ctx context.Context, name string) (*Agent, error) {
	row, err := s.queries.GetSession(ctx, name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get session %q: %w", name, err)
	}

	agent := agentFromRow(row.ID, row.Name, row.SessionConfigID, row.PaneID, row.Cwd, row.Flags, row.Prompt, row.State, row.ClaudeSessionID, row.WorkflowID, row.IsDeleted, row.CreatedAt)
	return &agent, nil
}

// SetState updates just an agent's state, keyed by name.
func (s *Store) SetState(ctx context.Context, name, state string) error {
	if err := s.queries.SetSessionState(ctx, models.SetSessionStateParams{Name: name, State: state}); err != nil {
		return fmt.Errorf("failed to set state for %q: %w", name, err)
	}

	return nil
}

// SetStateByID updates just an agent's state, keyed by its stable id. The
// reconciler uses this so a write decided from a snapshot always lands on the
// exact row it observed, even if that row's name was reused by another agent in
// the meantime.
func (s *Store) SetStateByID(ctx context.Context, id, state string) error {
	if err := s.queries.SetSessionStateByID(ctx, models.SetSessionStateByIDParams{ID: id, State: state}); err != nil {
		return fmt.Errorf("failed to set state for id %q: %w", id, err)
	}

	return nil
}

// Delete removes an agent by name.
func (s *Store) Delete(ctx context.Context, name string) error {
	if err := s.queries.DeleteSession(ctx, name); err != nil {
		return fmt.Errorf("failed to delete session %q: %w", name, err)
	}

	return nil
}

// Rename changes an agent's name (title), keyed by id.
func (s *Store) Rename(ctx context.Context, id, name string) error {
	if err := s.queries.RenameSession(ctx, models.RenameSessionParams{ID: id, Name: name}); err != nil {
		return fmt.Errorf("failed to rename session %q: %w", id, err)
	}

	return nil
}

// SoftDelete hides an agent from listings without dropping the row.
func (s *Store) SoftDelete(ctx context.Context, name string) error {
	if err := s.queries.MarkSessionDeleted(ctx, name); err != nil {
		return fmt.Errorf("failed to soft-delete session %q: %w", name, err)
	}

	return nil
}

// Undelete clears an agent's soft-deleted flag by id, so restoring a chat found
// in search returns it to the normal listing. It is keyed by id, not name,
// because a soft-deleted row can share a name with a live agent, so the id is
// the only unambiguous handle to the exact row to restore.
func (s *Store) Undelete(ctx context.Context, id string) error {
	if err := s.queries.RestoreSession(ctx, id); err != nil {
		return fmt.Errorf("failed to undelete session %q: %w", id, err)
	}

	return nil
}

// ActiveNameExists reports whether a live (non-deleted) agent already holds the
// name. New sessions consult this so an auto-generated name never collides with
// an agent whose row still owns the name, including a dormant one whose tmux
// session is gone but whose record survives.
func (s *Store) ActiveNameExists(ctx context.Context, name string) (bool, error) {
	exists, err := s.queries.ActiveNameExists(ctx, name)
	if err != nil {
		return false, fmt.Errorf("failed to check active name %q: %w", name, err)
	}

	return exists != 0, nil
}

// MoveToWorkflow reassigns a chat to a workflow, keyed by the chat's id. A nil
// workflowID unassigns the chat, returning it to the root listing.
func (s *Store) MoveToWorkflow(ctx context.Context, id string, workflowID *string) error {
	if err := s.queries.SetSessionWorkflow(ctx, models.SetSessionWorkflowParams{ID: id, WorkflowID: workflowID}); err != nil {
		return fmt.Errorf("failed to move session %q to workflow: %w", id, err)
	}

	return nil
}

// ListWorkflows returns every live workflow, most recently updated first.
func (s *Store) ListWorkflows(ctx context.Context) ([]Workflow, error) {
	rows, err := s.queries.ListWorkflows(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list workflows: %w", err)
	}

	workflows := make([]Workflow, 0, len(rows))
	for _, row := range rows {
		workflows = append(workflows, workflowFromRow(row.ID, row.Name, row.Cwd, row.Flags))
	}

	return workflows, nil
}

// GetWorkflow returns a workflow by id, nil when none exists, or an error for a
// real failure. No-rows is the nil case; every other error is surfaced.
func (s *Store) GetWorkflow(ctx context.Context, id string) (*Workflow, error) {
	row, err := s.queries.GetWorkflow(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get workflow %q: %w", id, err)
	}

	workflow := workflowFromRow(row.ID, row.Name, row.Cwd, row.Flags)
	return &workflow, nil
}

// CreateWorkflow inserts a workflow with a fresh uuidv7 id, together with its
// own config row holding the given cwd and flags, and returns it. The config is
// the blueprint prefilled into new chats created inside the workflow.
func (s *Store) CreateWorkflow(ctx context.Context, name, cwd, flags string) (*Workflow, error) {
	workflowID, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("failed to generate workflow id: %w", err)
	}
	configID, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("failed to generate workflow config id: %w", err)
	}

	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin create-workflow tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	queries := s.queries.WithTx(tx)
	if err := queries.InsertWorkflowConfig(ctx, models.InsertWorkflowConfigParams{
		ID:    configID.String(),
		Cwd:   cwd,
		Flags: flags,
	}); err != nil {
		return nil, fmt.Errorf("failed to insert config for workflow %q: %w", name, err)
	}

	row, err := queries.InsertWorkflow(ctx, models.InsertWorkflowParams{
		ID:               workflowID.String(),
		Name:             name,
		WorkflowConfigID: configID.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create workflow %q: %w", name, err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit create of workflow %q: %w", name, err)
	}

	workflow := workflowFromRow(row.ID, row.Name, cwd, flags)
	return &workflow, nil
}

// RenameWorkflow changes a workflow's display name, keyed by id. Nothing keys on
// the name, so this cannot collide.
func (s *Store) RenameWorkflow(ctx context.Context, id, name string) error {
	if err := s.queries.RenameWorkflow(ctx, models.RenameWorkflowParams{ID: id, Name: name}); err != nil {
		return fmt.Errorf("failed to rename workflow %q: %w", id, err)
	}

	return nil
}

// DeleteWorkflow soft-deletes a workflow and orphans its chats back to
// unassigned in one transaction, so no chat is ever stranded behind a hidden
// workflow. The two writes commit together or not at all.
func (s *Store) DeleteWorkflow(ctx context.Context, id string) error {
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin delete-workflow tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	queries := s.queries.WithTx(tx)
	if err := queries.MarkWorkflowDeleted(ctx, id); err != nil {
		return fmt.Errorf("failed to soft-delete workflow %q: %w", id, err)
	}

	if err := queries.ClearWorkflowSessions(ctx, &id); err != nil {
		return fmt.Errorf("failed to orphan chats of workflow %q: %w", id, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit delete of workflow %q: %w", id, err)
	}

	return nil
}

// DeleteWorkflowWithChats soft-deletes a workflow together with its chats in one
// transaction. The chats keep their workflow_id, so they still list under the
// workflow's id in search, tagged deleted alongside it.
func (s *Store) DeleteWorkflowWithChats(ctx context.Context, id string) error {
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin delete-workflow-with-chats tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	queries := s.queries.WithTx(tx)
	if err := queries.MarkWorkflowDeleted(ctx, id); err != nil {
		return fmt.Errorf("failed to soft-delete workflow %q: %w", id, err)
	}

	if err := queries.MarkWorkflowSessionsDeleted(ctx, &id); err != nil {
		return fmt.Errorf("failed to soft-delete chats of workflow %q: %w", id, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit delete of workflow %q with chats: %w", id, err)
	}

	return nil
}

// agentFromRow builds an Agent from the columns every session read returns.
// The joined reads produce distinct sqlc row types with identical fields, so
// the caller passes the fields explicitly rather than one row type.
func agentFromRow(id, name, sessionConfigID, pane, cwd, flags, prompt, state, claudeSessionID string, workflowID *string, isDeleted int64, createdAt time.Time) Agent {
	return Agent{
		ID:              id,
		Name:            name,
		SessionConfigID: sessionConfigID,
		Pane:            pane,
		Cwd:             cwd,
		Flags:           flags,
		Prompt:          prompt,
		State:           state,
		ClaudeSessionID: claudeSessionID,
		WorkflowID:      workflowID,
		Deleted:         isDeleted != 0,
		CreatedAt:       createdAt,
	}
}

func workflowFromRow(id, name, cwd, flags string) Workflow {
	return Workflow{
		ID:    id,
		Name:  name,
		Cwd:   cwd,
		Flags: flags,
	}
}
