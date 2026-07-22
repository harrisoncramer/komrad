// Package store persists Claude agents in SQLite via the sqlc-generated
// queries, so they survive tmux exits and restarts.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"claude-agents/db"
	"claude-agents/db/models"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// Agent is a persisted background agent.
type Agent struct {
	ID     string
	Name   string
	Pane   string
	Cwd    string
	Flags  string
	Prompt string
	State  string
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
		ID:     agent.ID,
		Name:   agent.Name,
		Cwd:    agent.Cwd,
		Flags:  agent.Flags,
		Prompt: agent.Prompt,
		PaneID: agent.Pane,
		State:  agent.State,
	}); err != nil {
		return fmt.Errorf("failed to upsert session %q: %w", agent.Name, err)
	}

	return nil
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

func fromModel(row *models.Session) Agent {
	return Agent{
		ID:     row.ID,
		Name:   row.Name,
		Pane:   row.PaneID,
		Cwd:    row.Cwd,
		Flags:  row.Flags,
		Prompt: row.Prompt,
		State:  row.State,
	}
}
