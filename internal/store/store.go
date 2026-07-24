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
	ID   string
	Name string
	// Context is free-text durable state for the workflow, edited by the user.
	Context string
	Cwd     string
	Flags   string
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

	if err := ensureTranscriptIndex(conn); err != nil {
		return nil, fmt.Errorf("failed to create transcript index: %w", err)
	}

	return &Store{conn: conn, queries: models.New(conn)}, nil
}

// transcriptFTS is the FTS5 index over transcript text. It lives outside the
// Atlas migrations and sqlc: neither models a virtual table, and sqlc's SQLite
// parser cannot parse fts5 or the MATCH/bm25/snippet the search reads with. The
// store owns both writes instead, so the index is derived state kept in step
// with session_transcripts by UpsertTranscript, never edited on its own.
const transcriptFTS = "session_transcripts_fts"

// ensureTranscriptIndex creates the FTS5 index if absent. The trigram tokenizer
// indexes every three-character run, so a query matches on any substring rather
// than whole words only. session_id is carried UNINDEXED so a hit maps back to
// its chat without a join.
func ensureTranscriptIndex(conn *sql.DB) error {
	stmt := fmt.Sprintf(
		"CREATE VIRTUAL TABLE IF NOT EXISTS %s USING fts5(content, session_id UNINDEXED, tokenize='trigram')",
		transcriptFTS,
	)
	if _, err := conn.Exec(stmt); err != nil {
		return fmt.Errorf("failed to create %s: %w", transcriptFTS, err)
	}

	return nil
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
		workflows = append(workflows, workflowFromRow(row.ID, row.Name, row.Context, row.Cwd, row.Flags))
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

	workflow := workflowFromRow(row.ID, row.Name, row.Context, row.Cwd, row.Flags)
	return &workflow, nil
}

// CreateWorkflow inserts a workflow with a fresh uuidv7 id, together with its
// own config row holding the given cwd and flags, and returns it. The config is
// the blueprint prefilled into new chats created inside the workflow.
func (s *Store) CreateWorkflow(ctx context.Context, cfg WorkflowConfig) (*Workflow, error) {
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
		Cwd:   cfg.Cwd,
		Flags: cfg.Flags,
	}); err != nil {
		return nil, fmt.Errorf("failed to insert config for workflow %q: %w", cfg.Name, err)
	}

	row, err := queries.InsertWorkflow(ctx, models.InsertWorkflowParams{
		ID:               workflowID.String(),
		Name:             cfg.Name,
		Context:          cfg.Context,
		WorkflowConfigID: configID.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create workflow %q: %w", cfg.Name, err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit create of workflow %q: %w", cfg.Name, err)
	}

	workflow := workflowFromRow(row.ID, row.Name, row.Context, cfg.Cwd, cfg.Flags)
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
// its linked config (cwd, flags) in one transaction, keyed by id. Nothing keys
// on the name, so the rename cannot collide. The writes commit together or not
// at all.
func (s *Store) UpdateWorkflow(ctx context.Context, id string, cfg WorkflowConfig) error {
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin update-workflow tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

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

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit update of workflow %q: %w", id, err)
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

// Transcript is a session's stored transcript: the flattened text and how far
// into the source JSONL the sync has consumed.
type Transcript struct {
	Content      string
	SyncedOffset int64
}

// SearchHit is one transcript matched by a search: the session it belongs to,
// its display name, whether that session is soft-deleted, a snippet of the
// matching text with the hit marked, and the FTS relevance rank (a bm25 score,
// lower is a better match).
type SearchHit struct {
	SessionID string
	Name      string
	Deleted   bool
	Snippet   string
	Rank      float64
}

// SnippetOpen and SnippetClose wrap the matched terms inside a hit's snippet, so
// the caller can find and style the match without re-running the query. They are
// plain text markers passed to FTS5's snippet(), chosen not to occur in ordinary
// transcript prose.
const (
	SnippetOpen  = "\x02"
	SnippetClose = "\x03"
)

// GetTranscript returns a session's stored transcript, or a zero Transcript when
// none has been synced yet. No-rows is the zero case, so a first sync sees an
// offset of 0 and indexes the whole file; every other error is surfaced.
func (s *Store) GetTranscript(ctx context.Context, sessionID string) (Transcript, error) {
	row, err := s.queries.GetTranscript(ctx, sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return Transcript{}, nil
	}
	if err != nil {
		return Transcript{}, fmt.Errorf("failed to get transcript for %q: %w", sessionID, err)
	}

	return Transcript{Content: row.Content, SyncedOffset: row.SyncedOffset}, nil
}

// UpsertTranscript writes a session's transcript and re-indexes it in one
// transaction, so the FTS index never drifts from the stored text. The index row
// is deleted and reinserted rather than updated, since the content column is the
// indexed one and there is at most one row per session.
func (s *Store) UpsertTranscript(ctx context.Context, sessionID, content string, offset int64) error {
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin upsert-transcript tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	queries := s.queries.WithTx(tx)
	if err := queries.UpsertTranscript(ctx, models.UpsertTranscriptParams{
		SessionID:    sessionID,
		Content:      content,
		SyncedOffset: offset,
	}); err != nil {
		return fmt.Errorf("failed to upsert transcript for %q: %w", sessionID, err)
	}

	if _, err := tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE session_id = ?", transcriptFTS), sessionID); err != nil {
		return fmt.Errorf("failed to clear transcript index for %q: %w", sessionID, err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (content, session_id) VALUES (?, ?)", transcriptFTS), content, sessionID); err != nil {
		return fmt.Errorf("failed to index transcript for %q: %w", sessionID, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transcript for %q: %w", sessionID, err)
	}

	return nil
}

// SearchTranscripts returns the transcripts matching query, best match first.
// The query is passed to FTS5 as a single quoted string so its contents are
// treated as a literal phrase rather than FTS operator syntax; with the trigram
// tokenizer that yields a substring match. A blank query returns no hits rather
// than every transcript.
func (s *Store) SearchTranscripts(ctx context.Context, query string, limit int) ([]SearchHit, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}

	// The join carries the chat's name and deleted flag onto each hit so a result
	// row is self-describing. It is an inner join, so a transcript with no session
	// row (an orphan that should not occur) is dropped rather than shown nameless.
	// The bm25 and snippet auxiliary functions take the FTS table by name, so it
	// is referenced unaliased while sessions is aliased.
	stmt := fmt.Sprintf(
		"SELECT %[1]s.session_id, s.name, s.is_deleted, snippet(%[1]s, 0, ?, ?, '…', 20), bm25(%[1]s) AS rank "+
			"FROM %[1]s JOIN sessions s ON s.id = %[1]s.session_id "+
			"WHERE %[1]s MATCH ? ORDER BY rank LIMIT ?",
		transcriptFTS,
	)
	rows, err := s.conn.QueryContext(ctx, stmt, SnippetOpen, SnippetClose, ftsPhrase(query), limit)
	if err != nil {
		return nil, fmt.Errorf("failed to search transcripts for %q: %w", query, err)
	}
	defer func() { _ = rows.Close() }()

	var hits []SearchHit
	for rows.Next() {
		var (
			h       SearchHit
			deleted int64
		)
		if err := rows.Scan(&h.SessionID, &h.Name, &deleted, &h.Snippet, &h.Rank); err != nil {
			return nil, fmt.Errorf("failed to scan search hit: %w", err)
		}
		h.Deleted = deleted != 0
		hits = append(hits, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read search hits: %w", err)
	}

	return hits, nil
}

// ftsPhrase wraps a user query as a double-quoted FTS5 string literal, escaping
// embedded double quotes by doubling them. This makes FTS5 read the whole query
// as one phrase, so punctuation and spaces in it are matched literally instead
// of parsed as query syntax.
func ftsPhrase(query string) string {
	return `"` + strings.ReplaceAll(query, `"`, `""`) + `"`
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

func workflowFromRow(id, name, workflowContext, cwd, flags string) Workflow {
	return Workflow{
		ID:      id,
		Name:    name,
		Context: workflowContext,
		Cwd:     cwd,
		Flags:   flags,
	}
}
