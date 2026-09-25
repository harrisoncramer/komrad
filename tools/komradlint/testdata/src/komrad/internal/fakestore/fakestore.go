package fakestore

import (
	"context"

	"komrad/internal/store/boxstore"
)

type mailbox interface {
	Send(ctx context.Context, params boxstore.SendParams) error
}

type realBox struct{}

func (r *realBox) Send(_ context.Context, _ boxstore.SendParams) error {
	return nil
}

var _ mailbox = (*realBox)(nil)
