package fakestore

import (
	"context"

	"weave/internal/store/boxstore"
)

type mailbox interface {
	Send(ctx context.Context, params boxstore.SendParams) error
}

type realBox struct{}

func (r *realBox) Send(_ context.Context, _ boxstore.SendParams) error {
	return nil
}

var _ mailbox = (*realBox)(nil)
