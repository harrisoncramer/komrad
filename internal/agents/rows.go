package agents

import (
	"context"
	"fmt"

	"weave/internal/git"
	"weave/lib/xslice"

	agentstore "weave/internal/store/agent"
	rowstore "weave/internal/store/row"
)

// Row is everything an agent's row in the panel says.
type Row struct {
	rowstore.Row
	Alive bool
	// LocalTree is where the worktree stands against the round the local verdict was given on.
	LocalTree git.Standing
}

type rowLister interface {
	ListPage(ctx context.Context, page rowstore.Page) ([]rowstore.Row, error)
	ListByID(ctx context.Context, ids []string) ([]rowstore.Row, error)
	ListLiveByID(ctx context.Context, ids []string) ([]rowstore.Row, error)
}

type runningReader interface {
	GetRunningAgents(ctx context.Context, agents []agentstore.Agent) map[string]bool
}

type treeStandingReader interface {
	GetStanding(agentID, snapshotSHA string) git.Standing
}

// NewRowReaderParams is what a row reader is built over.
type NewRowReaderParams struct {
	Rows     rowLister
	Liveness runningReader
	Trees    treeStandingReader
}

// RowReader reads what each agent's row says, in one query for the stored facts and one look at
// tmux and the cwd sync for the rest.
type RowReader struct {
	rows     rowLister
	liveness runningReader
	trees    treeStandingReader
}

// NewRowReader builds the reader over the store, the pane liveness and the cwd sync.
func NewRowReader(params NewRowReaderParams) *RowReader {
	return &RowReader{
		rows:     params.Rows,
		liveness: params.Liveness,
		trees:    params.Trees,
	}
}

// ListRows returns one page of rows, newest first.
func (r *RowReader) ListRows(ctx context.Context, page rowstore.Page) ([]Row, error) {
	stored, err := r.rows.ListPage(ctx, page)
	if err != nil {
		return nil, fmt.Errorf("failed to list agent rows: %w", err)
	}

	return r.fill(ctx, stored), nil
}

// ListRowsByID returns the rows of the given agents in the order asked, leaving out any id
// nothing holds.
func (r *RowReader) ListRowsByID(ctx context.Context, ids []string) ([]Row, error) {
	stored, err := r.rows.ListByID(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent rows: %w", err)
	}

	return r.fill(ctx, orderByID(stored, ids)), nil
}

// ListLiveRowsByID returns the rows of the given agents that the live listing holds, in the
// order asked.
func (r *RowReader) ListLiveRowsByID(ctx context.Context, ids []string) ([]Row, error) {
	stored, err := r.rows.ListLiveByID(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to get live agent rows: %w", err)
	}

	return r.fill(ctx, orderByID(stored, ids)), nil
}

func orderByID(stored []rowstore.Row, ids []string) []rowstore.Row {
	byID := xslice.KeyBy(stored, func(row rowstore.Row) string { return row.ID })
	ordered := make([]rowstore.Row, 0, len(stored))
	for _, id := range ids {
		if row, ok := byID[id]; ok {
			ordered = append(ordered, row)
		}
	}

	return ordered
}

// GetRow returns one agent's row, archived or not.
func (r *RowReader) GetRow(ctx context.Context, id string) (*Row, error) {
	rows, err := r.ListRowsByID(ctx, []string{id})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrAgentNotFound.Saying("no agent with id %q", id)
	}

	return &rows[0], nil
}

// fill adds what only tmux and the cwd sync know.
func (r *RowReader) fill(ctx context.Context, stored []rowstore.Row) []Row {
	running := r.liveness.GetRunningAgents(ctx, xslice.Map(stored, func(row rowstore.Row) agentstore.Agent { return row.Agent }))

	return xslice.Map(stored, func(row rowstore.Row) Row {
		return Row{
			Row:       row,
			Alive:     running[row.ID],
			LocalTree: r.trees.GetStanding(row.ID, row.LocalSnapshotSHA),
		}
	})
}
