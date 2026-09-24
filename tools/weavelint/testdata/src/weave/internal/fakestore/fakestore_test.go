package fakestore

import (
	"context"

	"weave/internal/store/boxstore"
)

type fakeBox struct {
	sent []string
}

func (f *fakeBox) Send(_ context.Context, params boxstore.SendParams) error { // want `fakeBox.Send stands in for boxstore.Store.Send; test against the real store through storetest`
	f.sent = append(f.sent, params.Body)

	return nil
}

func (f *fakeBox) List(_ context.Context) ([]*boxstore.Box, error) {
	return nil, nil
}

func (f *fakeBox) Seed(boxes []*boxstore.Box) { // want `fakeBox.Seed stands in for boxstore.Store.Seed; test against the real store through storetest`
}

func (f *fakeBox) Get(_ context.Context, _ string) (*boxstore.Box, error) { // want `fakeBox.Get stands in for boxstore.Store.Get; test against the real store through storetest`
	return nil, nil
}

func (f *fakeBox) Validate() error {
	return nil
}

func (f *fakeBox) Count() int {
	return len(f.sent)
}

func (f *fakeBox) stage(_ boxstore.SendParams) {}

type fakeService struct{}

func (s *fakeService) Deliver(_ context.Context, _ boxstore.SendParams) error {
	return nil
}

func (s *fakeService) Send(_ boxstore.SendParams) error {
	return nil
}

func seed(_ boxstore.SendParams) {}
