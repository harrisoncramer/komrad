package harness

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"komrade/internal/fail"
)

var ErrNotRegistered = fail.InvalidArgument("harness not registered")

type Registry struct {
	harnesses map[string]Harness
}

// NewRegistry indexes harnesses by their own id, refusing two that claim one id.
func NewRegistry(harnesses ...Harness) (*Registry, error) {
	indexed := make(map[string]Harness, len(harnesses))
	for _, candidate := range harnesses {
		id := candidate.ID()
		if id == "" {
			return nil, errors.New("a harness answers to no id")
		}
		if _, taken := indexed[id]; taken {
			return nil, fmt.Errorf("two harnesses answer to %q", id)
		}

		indexed[id] = candidate
	}

	return &Registry{harnesses: indexed}, nil
}

// GetByID returns the harness answering to id, or ErrNotRegistered naming it.
func (r *Registry) GetByID(id string) (Harness, error) {
	found, ok := r.harnesses[id]
	if !ok {
		return nil, fmt.Errorf("%w: %q, known are %s", ErrNotRegistered, id, strings.Join(r.IDs(), ", "))
	}

	return found, nil
}

// IDs are the registered harness ids, sorted so a message listing the choices reads the same
// every time.
func (r *Registry) IDs() []string {
	ids := make([]string, 0, len(r.harnesses))
	for id := range r.harnesses {
		ids = append(ids, id)
	}
	slices.Sort(ids)

	return ids
}
