package agent

import (
	"context"

	"weave/db/models"
)

const (
	TableAgents    = "agents"
	TableWorkflows = "workflows"
)

type DB interface {
	models.DBTX
	Touch(table string, ids ...string)
}

type Store struct {
	queries *models.Queries
	db      DB
}

func (s *Store) SetState(ctx context.Context, id string) error {
	if err := s.queries.SetAgentStateByID(ctx, id); err != nil {
		return err
	}

	s.db.Touch(TableAgents, id)

	return nil
}

func (s *Store) SetStateQuietly(ctx context.Context, id string) error { // want `SetStateQuietly writes agents and never touches the agents listing afterwards: the rows it changed stay stale on the panel`
	return s.queries.SetAgentStateByID(ctx, id)
}

func (s *Store) TouchFirst(ctx context.Context, id string) error { // want `TouchFirst writes agents and never touches the agents listing afterwards: the rows it changed stay stale on the panel`
	s.db.Touch(TableAgents, id)

	return s.queries.SetAgentStateByID(ctx, id)
}

func (s *Store) Archive(ctx context.Context, ids []string) error {
	archived, err := s.queries.MarkAgentsArchived(ctx, ids)
	if err != nil {
		return err
	}

	s.db.Touch(TableAgents, archived...)

	return nil
}

func (s *Store) ClearMark(ctx context.Context, id string) error { // want `ClearMark writes marks and never touches the agents or workflows listing afterwards: the rows it changed stay stale on the panel`
	_, err := s.queries.DeleteMarkForTarget(ctx, id)

	return err
}

func (s *Store) Get(ctx context.Context, id string) (string, error) {
	return s.queries.GetAgent(ctx, id)
}

func (s *Store) Lock(ctx context.Context, id string) error {
	return s.queries.LockAgentMetadata(ctx, id)
}

func (s *Store) Measure(ctx context.Context, dir string) error {
	return s.queries.UpsertWorktreeSize(ctx, dir)
}

func (s *Store) Hook(ctx context.Context, id string) error {
	if err := models.New(nil).InsertWorkflowHook(ctx, id); err != nil {
		return err
	}

	s.db.Touch(TableWorkflows, id)

	return nil
}

func (s *Store) HookQuietly(ctx context.Context, id string) error { // want `HookQuietly writes workflow_hooks and never touches the workflows listing afterwards: the rows it changed stay stale on the panel`
	return models.New(nil).InsertWorkflowHook(ctx, id)
}

func (s *Store) ArchiveThroughHelper(ctx context.Context, ids []string) error {
	archived, err := s.queries.MarkAgentsArchived(ctx, ids)
	if err != nil {
		return err
	}

	s.touchArchived(archived)

	return nil
}

func (s *Store) touchArchived(ids []string) {
	s.db.Touch("agents", ids...)
}

func (s *Store) HookTouchingTheWrongListing(ctx context.Context, id string) error { // want `HookTouchingTheWrongListing writes workflow_hooks and never touches the workflows listing afterwards: the rows it changed stay stale on the panel`
	if err := s.queries.InsertWorkflowHook(ctx, id); err != nil {
		return err
	}

	s.db.Touch(TableAgents, id)

	return nil
}

func (s *Store) ClearMarkOfEither(ctx context.Context, id string) error {
	if _, err := s.queries.DeleteMarkForTarget(ctx, id); err != nil {
		return err
	}

	s.db.Touch(TableWorkflows, id)

	return nil
}
