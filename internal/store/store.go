// Package store persists Claude agents in SQLite via the sqlc-generated
// queries, so they survive tmux exits and restarts.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	ID              string
	Name            string
	Pane            string
	Cwd             string
	Flags           string
	Prompt          string
	State           string
	ClaudeSessionID string
	Deleted         bool
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

	// WAL plus a busy timeout so the frequent, independent opens (hooks, the
	// once-a-second status render, the panel) don't trip over each other.
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
		agents = append(agents, fromModel(row))
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
		agents = append(agents, fromModel(row))
	}

	return agents, nil
}

// Upsert inserts or updates an agent keyed by name, assigning a uuidv7 id on
// first insert.
func (s *Store) Upsert(ctx context.Context, agent Agent) error {
	if agent.ID == "" {
		id, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("failed to generate id: %w", err)
		}

		agent.ID = id.String()
	}

	if _, err := s.queries.UpsertSession(ctx, models.UpsertSessionParams{
		ID:              agent.ID,
		Name:            agent.Name,
		Cwd:             agent.Cwd,
		Flags:           agent.Flags,
		Prompt:          agent.Prompt,
		PaneID:          agent.Pane,
		State:           agent.State,
		ClaudeSessionID: agent.ClaudeSessionID,
	}); err != nil {
		return fmt.Errorf("failed to upsert session %q: %w", agent.Name, err)
	}

	return nil
}

// UpsertState records an agent's state and pane, creating a bare row if none
// exists yet. It never touches cwd/flags/prompt, so lifecycle hooks don't wipe
// the metadata captured when a tool-created agent was launched.
func (s *Store) UpsertState(ctx context.Context, name, pane, state, claudeSessionID string) error {
	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("failed to generate id: %w", err)
	}

	if err := s.queries.UpsertSessionState(ctx, models.UpsertSessionStateParams{
		ID:              id.String(),
		Name:            name,
		Pane:            pane,
		State:           state,
		ClaudeSessionID: claudeSessionID,
	}); err != nil {
		return fmt.Errorf("failed to upsert state for %q: %w", name, err)
	}

	return nil
}

// Get returns an agent by name, or nil when none exists.
func (s *Store) Get(ctx context.Context, name string) (*Agent, error) {
	row, err := s.queries.GetSession(ctx, name)
	if err != nil {
		return nil, nil
	}

	agent := fromModel(row)
	return &agent, nil
}

// SetState updates just an agent's state.
func (s *Store) SetState(ctx context.Context, name, state string) error {
	if err := s.queries.SetSessionState(ctx, models.SetSessionStateParams{Name: name, State: state}); err != nil {
		return fmt.Errorf("failed to set state for %q: %w", name, err)
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

// Undelete clears an agent's soft-deleted flag, so restoring a chat found in
// search returns it to the normal listing.
func (s *Store) Undelete(ctx context.Context, name string) error {
	if err := s.queries.RestoreSession(ctx, name); err != nil {
		return fmt.Errorf("failed to undelete session %q: %w", name, err)
	}

	return nil
}

func fromModel(row *models.Session) Agent {
	return Agent{
		ID:              row.ID,
		Name:            row.Name,
		Pane:            row.PaneID,
		Cwd:             row.Cwd,
		Flags:           row.Flags,
		Prompt:          row.Prompt,
		State:           row.State,
		ClaudeSessionID: row.ClaudeSessionID,
		Deleted:         row.IsDeleted != 0,
	}
}
