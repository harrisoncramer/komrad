package models

import "context"

type DBTX interface{}

type Queries struct{}

func New(db DBTX) *Queries { return &Queries{} }

func (q *Queries) SetAgentStateByID(ctx context.Context, id string) error { return nil }

func (q *Queries) MarkAgentsArchived(ctx context.Context, ids []string) ([]string, error) {
	return ids, nil
}

func (q *Queries) DeleteMarkForTarget(ctx context.Context, id string) (int64, error) { return 1, nil }

func (q *Queries) GetAgent(ctx context.Context, id string) (string, error) { return id, nil }

func (q *Queries) LockAgentMetadata(ctx context.Context, id string) error { return nil }

func (q *Queries) UpsertWorktreeSize(ctx context.Context, dir string) error { return nil }

func (q *Queries) InsertWorkflowHook(ctx context.Context, id string) error { return nil }
