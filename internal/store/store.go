// Package store persists Claude agents in Postgres via the sqlc-generated
// queries, so they survive tmux exits and restarts.
package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"claude-agents/db"
	"claude-agents/db/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
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
	// SessionConfigID is this agent's own config row, holding its cwd and flags.
	// Every agent owns exactly one. Cwd and Flags are that row flattened on read.
	SessionConfigID string
	Pane            string
	Cwd             string
	Flags           string
	Prompt          string
	State           string
	ClaudeSessionID string
	// WorkflowID is the workflow this chat belongs to, or nil when unassigned. It
	// holds the workflow's stable id, not its display name.
	WorkflowID *string
	Deleted    bool
	// CreatedAt is when the chat was first recorded, for display.
	CreatedAt time.Time
	// UpdatedAt is when the row last changed. Listings sort by it, so it is also
	// the key a page of agents resumes from.
	UpdatedAt time.Time
}

// Workflow is a named bucket of related chats. It runs nothing. Its default cwd
// and flags are the blueprint prefilled into the new-chat form for chats created
// inside it.
type Workflow struct {
	ID   string
	Name string
	// Context is free-text durable state for the workflow, edited by the user.
	Context string
	Cwd     string
	Flags   string
	// UpdatedAt is what the listing sorts by, and half of the paging cursor.
	UpdatedAt time.Time
}

// WorkflowConfig is the editable blueprint of a workflow: its display label, its
// free-text context, and the cwd and flags seeded into chats created inside it.
type WorkflowConfig struct {
	Name    string
	Context string
	Cwd     string
	Flags   string
}

// SanitizeName maps a name to the characters tmux allows in a session name,
// which is also the store key so the two always agree.
func SanitizeName(name string) string {
	return strings.NewReplacer(":", "-", ".", "-", " ", "-").Replace(name)
}

// isID reports whether a string could be one of the ids this store hands out.
//
// Ids reach the store from outside: hook payloads, tool arguments, a name typed
// into the panel. The columns are uuid, so a value that is not one is refused by
// Postgres with a parse error rather than simply matching nothing, which turns a
// caller's typo into a failure instead of a miss. Checking first keeps the
// answer the caller expects: an id that cannot exist is one that is not there.
func isID(id string) bool {
	_, err := uuid.Parse(id)
	return err == nil
}

// Store is the Postgres-backed agent store.
type Store struct {
	pool    *pgxpool.Pool
	queries *models.Queries
}

// Open connects to the Postgres at url and brings its schema up to date. The
// ping is what turns an unreachable database into an error at startup rather
// than on the first query, which for a tool launched from a tmux keybinding is
// the difference between a message and a panel that renders empty.
func Open(ctx context.Context, url string) (*Store, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("failed to open db: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to reach db: %w", err)
	}

	if err := db.Migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to migrate db: %w", err)
	}

	return &Store{pool: pool, queries: models.New(pool)}, nil
}

// OpenReadOnly opens a pool for reading only, for surfaces that hand the
// database to something that must never write, such as the MCP endpoint. The
// guard rides on the connection rather than on each statement: every session
// the pool hands out defaults to read-only transactions, so a write is refused
// by Postgres itself even if it slips past the statement checks above it.
func OpenReadOnly(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database url: %w", err)
	}
	cfg.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to open db read-only: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to reach db read-only: %w", err)
	}

	return pool, nil
}

// Close releases the connection pool.
func (s *Store) Close() error {
	s.pool.Close()
	return nil
}

// List returns every persisted agent, most recently updated first.
func (s *Store) List(ctx context.Context) ([]Agent, error) {
	rows, err := s.queries.ListSessions(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}

	agents := make([]Agent, 0, len(rows))
	for _, row := range rows {
		agents = append(agents, agentFromRow(row.ID, row.Name, row.SessionConfigID, row.PaneID, row.Cwd, row.Flags, row.Prompt, row.State, row.ClaudeSessionID, row.WorkflowID, row.IsDeleted, row.CreatedAt, row.UpdatedAt))
	}

	return agents, nil
}

// SessionCursor locates an agent within a listing. Agents sort by update time,
// which several rows can share, so the id breaks the tie and makes the order
// total. Without it a page boundary landing mid-tie would skip or repeat rows.
type SessionCursor struct {
	UpdatedAt time.Time `json:"u"`
	ID        string    `json:"i"`
}

// ListPage returns one page of live agents, most recently updated first,
// resuming after cursor when one is given. Update time is a moving key: a chat
// the reconciler touches mid-scan sorts ahead of where the caller already is, so
// it can be seen twice or missed. That is inherent to keying a page on the
// column the listing sorts by, and the alternative, holding a snapshot open
// across requests, costs more than the duplicate is worth here.
func (s *Store) ListPage(ctx context.Context, limit int64, cursor *SessionCursor) ([]Agent, error) {
	params := models.ListSessionsPageParams{Lim: int32(limit)}
	if cursor != nil {
		params.CursorUpdatedAt = &cursor.UpdatedAt
		params.CursorID = &cursor.ID
	}

	rows, err := s.queries.ListSessionsPage(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list a page of sessions: %w", err)
	}

	agents := make([]Agent, 0, len(rows))
	for _, row := range rows {
		agents = append(agents, agentFromRow(row.ID, row.Name, row.SessionConfigID, row.PaneID, row.Cwd, row.Flags, row.Prompt, row.State, row.ClaudeSessionID, row.WorkflowID, row.IsDeleted, row.CreatedAt, row.UpdatedAt))
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
		agents = append(agents, agentFromRow(row.ID, row.Name, row.SessionConfigID, row.PaneID, row.Cwd, row.Flags, row.Prompt, row.State, row.ClaudeSessionID, row.WorkflowID, row.IsDeleted, row.CreatedAt, row.UpdatedAt))
	}

	return agents, nil
}

// Upsert inserts a new agent or updates an existing one. A blank id means a
// fresh agent, whose config row is inserted alongside the session in one
// transaction. A set id means an existing agent whose linked config and runtime
// fields are updated in place, never inserting a second config row.
func (s *Store) Upsert(ctx context.Context, agent Agent) error {
	if agent.ID == "" {
		return s.insertAgent(ctx, agent)
	}

	return s.updateAgent(ctx, agent)
}

// insertAgent creates a fresh agent and its own config row in one transaction.
// Both ids come back from the database, which generates them, so the config is
// inserted first and the session points at what it returned.
func (s *Store) insertAgent(ctx context.Context, agent Agent) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin insert-session tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)

	configID, err := queries.InsertSessionConfig(ctx, models.InsertSessionConfigParams{
		Cwd:   agent.Cwd,
		Flags: agent.Flags,
	})
	if err != nil {
		return fmt.Errorf("failed to insert config for %q: %w", agent.Name, err)
	}

	if _, err := queries.InsertSession(ctx, models.InsertSessionParams{
		Name:            agent.Name,
		SessionConfigID: configID,
		Prompt:          agent.Prompt,
		PaneID:          agent.Pane,
		State:           agent.State,
		ClaudeSessionID: agent.ClaudeSessionID,
		WorkflowID:      agent.WorkflowID,
	}); err != nil {
		return fmt.Errorf("failed to insert session %q: %w", agent.Name, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit insert of session %q: %w", agent.Name, err)
	}

	return nil
}

// updateAgent updates an existing agent's linked config and runtime fields in
// one transaction, keyed by its stable id.
func (s *Store) updateAgent(ctx context.Context, agent Agent) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin update-session tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

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

	if err := tx.Commit(ctx); err != nil {
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
// failure. No-rows is the nil case. Every other error is surfaced so a caller
// does not report a genuine DB failure as not found.
func (s *Store) Get(ctx context.Context, name string) (*Agent, error) {
	row, err := s.queries.GetSession(ctx, name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get session %q: %w", name, err)
	}

	agent := agentFromRow(row.ID, row.Name, row.SessionConfigID, row.PaneID, row.Cwd, row.Flags, row.Prompt, row.State, row.ClaudeSessionID, row.WorkflowID, row.IsDeleted, row.CreatedAt, row.UpdatedAt)
	return &agent, nil
}

// SetState updates just an agent's state, keyed by name. It reports whether a
// row was updated, so naming a chat that does not exist is distinguishable from
// setting a state it already had. The query returns the ids it touched, which is
// how the count is known without a second read.
func (s *Store) SetState(ctx context.Context, name, state string) (bool, error) {
	updated, err := s.queries.SetSessionState(ctx, models.SetSessionStateParams{Name: name, State: state})
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
	if !isID(id) {
		return nil
	}

	if err := s.queries.SetSessionStateByID(ctx, models.SetSessionStateByIDParams{ID: id, State: state}); err != nil {
		return fmt.Errorf("failed to set state for id %q: %w", id, err)
	}

	return nil
}

// Delete removes an agent by name, together with everything keyed to it: its
// config row and its transcript. The schema declares no foreign keys, so nothing
// cascades on its own, and deleting the session alone left the text still
// readable through the transcript and the SQL tools. It reports whether a row
// was there to delete, so a caller can tell a real delete from a no-op.
//
// It all commits together or not at all: a session removed without its
// transcript is the half-state this is here to prevent.
func (s *Store) Delete(ctx context.Context, name string) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to begin delete tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)

	target, err := queries.GetSessionForDelete(ctx, name)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to find session %q to delete: %w", name, err)
	}

	if err := queries.DeleteSessionByID(ctx, target.ID); err != nil {
		return false, fmt.Errorf("failed to delete session %q: %w", name, err)
	}
	if err := queries.DeleteSessionConfig(ctx, target.SessionConfigID); err != nil {
		return false, fmt.Errorf("failed to delete config for %q: %w", name, err)
	}
	if err := queries.DeleteSessionTranscript(ctx, target.ID); err != nil {
		return false, fmt.Errorf("failed to delete transcript for %q: %w", name, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("failed to commit delete of %q: %w", name, err)
	}

	return true, nil
}

// ErrNameTaken reports that a rename collided with a live chat's name. The
// unique index is scoped to live rows, so this only ever means a chat currently
// listed already answers to that name.
var ErrNameTaken = errors.New("name already taken")

// uniqueViolation is the SQLSTATE Postgres raises for a broken unique
// constraint, which is how a taken name is told apart from a real failure
// without reading the driver's message text.
const uniqueViolation = "23505"

// Rename changes an agent's name (title), keyed by id. It reports whether a row
// was renamed, so an id that matches nothing is distinguishable from a rename
// to the name a chat already had.
func (s *Store) Rename(ctx context.Context, id, name string) (bool, error) {
	if !isID(id) {
		return false, nil
	}

	renamed, err := s.queries.RenameSession(ctx, models.RenameSessionParams{ID: id, Name: name})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return false, ErrNameTaken
		}

		return false, fmt.Errorf("failed to rename session %q: %w", id, err)
	}

	return len(renamed) > 0, nil
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
	if !isID(id) {
		return nil
	}

	if err := s.queries.RestoreSession(ctx, id); err != nil {
		return fmt.Errorf("failed to undelete session %q: %w", id, err)
	}

	return nil
}

// ActiveNameExists reports whether a live, non-deleted agent already holds the
// name. New sessions consult this so an auto-generated name never collides with
// an agent whose row still owns the name, including a dormant one whose tmux
// session is gone but whose record survives.
func (s *Store) ActiveNameExists(ctx context.Context, name string) (bool, error) {
	exists, err := s.queries.ActiveNameExists(ctx, name)
	if err != nil {
		return false, fmt.Errorf("failed to check active name %q: %w", name, err)
	}

	return exists, nil
}

// MoveToWorkflow reassigns a chat to a workflow, keyed by the chat's id. A nil
// workflowID unassigns the chat, returning it to the root listing.
func (s *Store) MoveToWorkflow(ctx context.Context, id string, workflowID *string) error {
	if !isID(id) {
		return nil
	}

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
		workflows = append(workflows, workflowFromRow(row.ID, row.Name, row.Context, row.Cwd, row.Flags, row.UpdatedAt))
	}

	return workflows, nil
}

// WorkflowCursor locates a workflow within a listing. Workflows sort by update
// time, which several rows can share, so the id breaks the tie and makes the
// order total.
type WorkflowCursor struct {
	UpdatedAt time.Time `json:"u"`
	ID        string    `json:"i"`
}

// ListWorkflowsPage returns one page of live workflows, most recently updated
// first, resuming after cursor when one is given.
func (s *Store) ListWorkflowsPage(ctx context.Context, limit int64, cursor *WorkflowCursor) ([]Workflow, error) {
	params := models.ListWorkflowsPageParams{Lim: int32(limit)}
	if cursor != nil {
		params.CursorUpdatedAt = &cursor.UpdatedAt
		params.CursorID = &cursor.ID
	}

	rows, err := s.queries.ListWorkflowsPage(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list a page of workflows: %w", err)
	}

	workflows := make([]Workflow, 0, len(rows))
	for _, row := range rows {
		workflows = append(workflows, workflowFromRow(row.ID, row.Name, row.Context, row.Cwd, row.Flags, row.UpdatedAt))
	}

	return workflows, nil
}

// GetWorkflow returns a workflow by id, nil when none exists, or an error for a
// real failure. No-rows is the nil case. Every other error is surfaced.
func (s *Store) GetWorkflow(ctx context.Context, id string) (*Workflow, error) {
	if !isID(id) {
		return nil, nil
	}

	row, err := s.queries.GetWorkflow(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get workflow %q: %w", id, err)
	}

	workflow := workflowFromRow(row.ID, row.Name, row.Context, row.Cwd, row.Flags, row.UpdatedAt)
	return &workflow, nil
}

// CreateWorkflow inserts a workflow together with its own config row holding the
// given cwd and flags, and returns it. The config is the blueprint prefilled
// into new chats created inside the workflow.
func (s *Store) CreateWorkflow(ctx context.Context, cfg WorkflowConfig) (*Workflow, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin create-workflow tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)

	configID, err := queries.InsertWorkflowConfig(ctx, models.InsertWorkflowConfigParams{
		Cwd:   cfg.Cwd,
		Flags: cfg.Flags,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to insert config for workflow %q: %w", cfg.Name, err)
	}

	row, err := queries.InsertWorkflow(ctx, models.InsertWorkflowParams{
		Name:             cfg.Name,
		Context:          cfg.Context,
		WorkflowConfigID: configID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create workflow %q: %w", cfg.Name, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit create of workflow %q: %w", cfg.Name, err)
	}

	workflow := workflowFromRow(row.ID, row.Name, row.Context, cfg.Cwd, cfg.Flags, row.UpdatedAt)
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

// UpdateWorkflow patches a workflow's display name, its free-text context, and
// its linked config of cwd and flags in one transaction, keyed by id. Nothing
// keys on the name, so the rename cannot collide. The writes commit together or
// not at all.
func (s *Store) UpdateWorkflow(ctx context.Context, id string, cfg WorkflowConfig) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin update-workflow tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	if err := queries.RenameWorkflow(ctx, models.RenameWorkflowParams{ID: id, Name: cfg.Name}); err != nil {
		return fmt.Errorf("failed to rename workflow %q: %w", id, err)
	}

	if err := queries.SetWorkflowContext(ctx, models.SetWorkflowContextParams{ID: id, Context: cfg.Context}); err != nil {
		return fmt.Errorf("failed to update context for workflow %q: %w", id, err)
	}

	if err := queries.UpdateWorkflowConfig(ctx, models.UpdateWorkflowConfigParams{WorkflowID: id, Cwd: cfg.Cwd, Flags: cfg.Flags}); err != nil {
		return fmt.Errorf("failed to update config for workflow %q: %w", id, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit update of workflow %q: %w", id, err)
	}

	return nil
}

// DeleteWorkflow soft-deletes a workflow and orphans its chats back to
// unassigned in one transaction, so no chat is ever stranded behind a hidden
// workflow. The two writes commit together or not at all.
func (s *Store) DeleteWorkflow(ctx context.Context, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin delete-workflow tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	if err := queries.MarkWorkflowDeleted(ctx, id); err != nil {
		return fmt.Errorf("failed to soft-delete workflow %q: %w", id, err)
	}

	if err := queries.ClearWorkflowSessions(ctx, &id); err != nil {
		return fmt.Errorf("failed to orphan chats of workflow %q: %w", id, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit delete of workflow %q: %w", id, err)
	}

	return nil
}

// DeleteWorkflowWithChats soft-deletes a workflow together with its chats in one
// transaction. The chats keep their workflow_id, so they still list under the
// workflow's id in search, tagged deleted alongside it.
func (s *Store) DeleteWorkflowWithChats(ctx context.Context, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin delete-workflow-with-chats tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := s.queries.WithTx(tx)
	if err := queries.MarkWorkflowDeleted(ctx, id); err != nil {
		return fmt.Errorf("failed to soft-delete workflow %q: %w", id, err)
	}

	if err := queries.MarkWorkflowSessionsDeleted(ctx, &id); err != nil {
		return fmt.Errorf("failed to soft-delete chats of workflow %q: %w", id, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit delete of workflow %q with chats: %w", id, err)
	}

	return nil
}

// Transcript is a session's stored transcript: the flattened text and how far
// into the source JSONL the sync has consumed.
type Transcript struct {
	Content      string
	SyncedOffset int64
}

// SearchHit is one transcript matched by a search: the session it belongs to,
// its display name, whether that session is soft-deleted, a snippet of the
// matching text with the hit marked, and how many times the query occurs in the
// transcript, which is what the results are ranked by.
type SearchHit struct {
	SessionID string
	Name      string
	Deleted   bool
	Snippet   string
	Rank      int32
}

// SnippetOpen and SnippetClose wrap the matched terms inside a hit's snippet, so
// the caller can find and style the match without re-running the query. They are
// plain text markers chosen not to occur in ordinary transcript prose.
const (
	SnippetOpen  = "\x02"
	SnippetClose = "\x03"
)

// The shape of a search hit's snippet: how much text to keep before the match so
// it reads in context, and how much of the transcript to return in total.
const (
	snippetLead  = 60
	snippetWidth = 200
)

// defaultSearchLimit bounds a search that asks for no limit of its own, so a
// direct store caller never pulls the whole corpus by omission.
const defaultSearchLimit = 50

// GetTranscript returns a session's stored transcript, or a zero Transcript when
// none has been synced yet. No-rows is the zero case, so a first sync sees an
// offset of 0 and indexes the whole file. Every other error is surfaced.
func (s *Store) GetTranscript(ctx context.Context, sessionID string) (Transcript, error) {
	if !isID(sessionID) {
		return Transcript{}, nil
	}

	row, err := s.queries.GetTranscript(ctx, sessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Transcript{}, nil
	}
	if err != nil {
		return Transcript{}, fmt.Errorf("failed to get transcript for %q: %w", sessionID, err)
	}

	return Transcript{Content: row.Content, SyncedOffset: row.SyncedOffset}, nil
}

// SessionExists reports whether any chat holds the given id, including
// soft-deleted ones, so a caller can tell an unknown id from a chat that simply
// has nothing stored yet.
func (s *Store) SessionExists(ctx context.Context, id string) (bool, error) {
	if !isID(id) {
		return false, nil
	}

	exists, err := s.queries.SessionExists(ctx, id)
	if err != nil {
		return false, fmt.Errorf("failed to check for session %q: %w", id, err)
	}

	return exists, nil
}

// UpsertTranscript writes a session's transcript. The search index is a trigram
// index on the column itself, so Postgres keeps it in step with the write and
// there is nothing separate to reindex.
func (s *Store) UpsertTranscript(ctx context.Context, sessionID, content string, offset int64) error {
	if err := s.queries.UpsertTranscript(ctx, models.UpsertTranscriptParams{
		SessionID:    sessionID,
		Content:      content,
		SyncedOffset: offset,
	}); err != nil {
		return fmt.Errorf("failed to upsert transcript for %q: %w", sessionID, err)
	}

	return nil
}

// TranscriptCursor locates a hit within one query's results. Hits sort by how
// many times the query occurs, which two transcripts can tie on, so the session
// id breaks the tie. The rank only means anything for the query that produced
// it, so a cursor is only ever replayed against that same query.
type TranscriptCursor struct {
	Rank      int32  `json:"r"`
	SessionID string `json:"i"`
}

// SearchTranscriptsParams is one page of a transcript search: the text to match,
// how many hits to return, and where in the previous page to resume.
type SearchTranscriptsParams struct {
	Query  string
	Limit  int64
	Cursor *TranscriptCursor
}

// SearchTranscripts returns one page of the transcripts containing the query as
// a substring, the transcripts that mention it most first, resuming after the
// cursor when one is given. A blank query returns no hits rather than every
// transcript. The database returns a plain window of the text around each match
// and the markers are added here, so what wraps a hit is decided in one place
// rather than in SQL.
func (s *Store) SearchTranscripts(ctx context.Context, params SearchTranscriptsParams) ([]SearchHit, error) {
	// Postgres refuses a NUL in a text value outright, so a query carrying one
	// would fail the request rather than simply match nothing. Dropping it makes
	// the search find nothing, which is what a search for an unmatchable byte
	// should do: no transcript can contain one either.
	query := strings.ReplaceAll(params.Query, "\x00", "")
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}

	limit := params.Limit
	if limit <= 0 {
		limit = defaultSearchLimit
	}

	args := models.SearchTranscriptsParams{
		Query:   query,
		Pattern: likePattern(query),
		Lead:    snippetLead,
		Width:   snippetWidth,
		Lim:     int32(limit),
	}
	if params.Cursor != nil {
		args.CursorRank = &params.Cursor.Rank
		args.CursorSessionID = &params.Cursor.SessionID
	}

	rows, err := s.queries.SearchTranscripts(ctx, args)
	if err != nil {
		return nil, fmt.Errorf("failed to search transcripts for %q: %w", query, err)
	}

	hits := make([]SearchHit, 0, len(rows))
	for _, row := range rows {
		hits = append(hits, SearchHit{
			SessionID: row.SessionID,
			Name:      row.Name,
			Deleted:   row.IsDeleted,
			Snippet:   markMatches(row.Snippet, query),
			Rank:      row.Rank,
		})
	}

	return hits, nil
}

// likePattern turns a user query into an ILIKE pattern matching it as a literal
// substring. The wildcards and the escape character are the three things LIKE
// reads specially, so a query containing one is escaped rather than allowed to
// widen its own search.
func likePattern(query string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)
	return "%" + escaped + "%"
}

// markMatches wraps every case-insensitive occurrence of query inside a snippet
// with the markers, which is what lets the caller style the match without
// searching the text again. The window the database returns is centred on a
// match, so there is normally at least one; a window that got cut mid-match is
// returned unchanged rather than marked wrongly.
func markMatches(snippet, query string) string {
	if query == "" {
		return snippet
	}

	var (
		out       strings.Builder
		lowerHay  = strings.ToLower(snippet)
		lowerNeed = strings.ToLower(query)
		at        int
	)
	for {
		i := strings.Index(lowerHay[at:], lowerNeed)
		if i < 0 {
			out.WriteString(snippet[at:])
			return out.String()
		}

		start := at + i
		end := start + len(query)
		out.WriteString(snippet[at:start])
		out.WriteString(SnippetOpen)
		out.WriteString(snippet[start:end])
		out.WriteString(SnippetClose)
		at = end
	}
}

// agentFromRow builds an Agent from the columns every session read returns.
// The joined reads produce distinct sqlc row types with identical fields, so
// the caller passes the fields explicitly rather than one row type.
func agentFromRow(id, name, sessionConfigID, pane, cwd, flags, prompt, state, claudeSessionID string, workflowID *string, isDeleted bool, createdAt, updatedAt time.Time) Agent {
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
		Deleted:         isDeleted,
		CreatedAt:       createdAt,
		UpdatedAt:       updatedAt,
	}
}

func workflowFromRow(id, name, workflowContext, cwd, flags string, updatedAt time.Time) Workflow {
	return Workflow{
		ID:        id,
		Name:      name,
		Context:   workflowContext,
		Cwd:       cwd,
		Flags:     flags,
		UpdatedAt: updatedAt,
	}
}
