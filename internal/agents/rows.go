package agents

import (
	"context"
	"fmt"

	"weave/lib/xslice"

	rowstore "weave/internal/store/row"
)

type rowLister interface {
	ListPage(ctx context.Context, page rowstore.Page) ([]rowstore.Row, error)
	ListByID(ctx context.Context, ids []string) ([]rowstore.Row, error)
	ListLiveByID(ctx context.Context, ids []string) ([]rowstore.Row, error)
}

// NewRowReaderParams is what a row reader is built over.
type NewRowReaderParams struct {
	Rows rowLister
}

// RowReader reads what each agent's row says.
type RowReader struct {
	rows rowLister
}

// NewRowReader builds the reader over the row store.
func NewRowReader(params NewRowReaderParams) *RowReader {
	return &RowReader{rows: params.Rows}
}

// ListRows returns one page of rows, newest first.
func (r *RowReader) ListRows(ctx context.Context, page rowstore.Page) ([]rowstore.Row, error) {
	rows, err := r.rows.ListPage(ctx, page)
	if err != nil {
		return nil, fmt.Errorf("failed to list agent rows: %w", err)
	}

	return rows, nil
}

// ListRowsByID returns the rows of the given agents in the order asked, leaving out any id
// nothing holds.
func (r *RowReader) ListRowsByID(ctx context.Context, ids []string) ([]rowstore.Row, error) {
	rows, err := r.rows.ListByID(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent rows: %w", err)
	}

	return orderByID(rows, ids), nil
}

// ListLiveRowsByID returns the rows of the given agents that the live listing holds, in the
// order asked.
func (r *RowReader) ListLiveRowsByID(ctx context.Context, ids []string) ([]rowstore.Row, error) {
	rows, err := r.rows.ListLiveByID(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to get live agent rows: %w", err)
	}

	return orderByID(rows, ids), nil
}

// orderByID is the stored rows in the order their ids were asked for.
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
func (r *RowReader) GetRow(ctx context.Context, id string) (*rowstore.Row, error) {
	rows, err := r.ListRowsByID(ctx, []string{id})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrAgentNotFound.Saying("no agent with id %q", id)
	}

	return &rows[0], nil
}
