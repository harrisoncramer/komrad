package billing

import (
	"context"
	"time"

	"komrad/internal/fail"
)

type charger interface {
	Charge(ctx context.Context, id string) error
}

type refunder interface {
	Refund(ctx context.Context, id string) error
}

type rowReader interface {
	GetRow(ctx context.Context, id string) (string, error)
}

type Handler struct {
	charges charger
	refunds refunder
	rows    rowReader
	now     func() time.Time
	name    string
}

func (h *Handler) Charge(ctx context.Context, id string) (*string, *fail.Error) {
	if err := h.charges.Charge(ctx, id); err != nil {
		return nil, &fail.Error{}
	}

	return h.render(ctx, id)
}

func (h *Handler) Stamp(ctx context.Context, id string) (*string, *fail.Error) {
	if err := h.charges.Charge(ctx, id); err != nil {
		return nil, &fail.Error{}
	}

	stamped := h.now().String() + h.name

	return &stamped, nil
}

func (h *Handler) Settle(ctx context.Context, id string) (*string, *fail.Error) { // want `Settle reaches charges, refunds; give one service a method that does the combining`
	if err := h.charges.Charge(ctx, id); err != nil {
		return nil, &fail.Error{}
	}

	if err := h.refunds.Refund(ctx, id); err != nil {
		return nil, &fail.Error{}
	}

	return nil, nil
}

func (h *Handler) Reverse(ctx context.Context, id string) (*string, *fail.Error) { // want `Reverse reaches charges, refunds; give one service a method that does the combining`
	if err := h.refunds.Refund(ctx, id); err != nil {
		return nil, &fail.Error{}
	}

	h.recharge(ctx, id)

	return nil, nil
}

func (h *Handler) Retry(ctx context.Context, id string) error {
	if err := h.charges.Charge(ctx, id); err != nil {
		return err
	}

	return h.refunds.Refund(ctx, id)
}

func (h *Handler) recharge(ctx context.Context, id string) {
	_ = h.charges.Charge(ctx, id)
}

func (h *Handler) render(ctx context.Context, id string) (*string, *fail.Error) {
	row, err := h.rows.GetRow(ctx, id)
	if err != nil {
		return nil, &fail.Error{}
	}

	return &row, nil
}
