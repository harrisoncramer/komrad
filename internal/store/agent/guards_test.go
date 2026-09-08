package agent

import (
	"testing"

	"weave/internal/store"
	"weave/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadsAndWritesRefuseAnIDNoRowCouldHold pins the guard each one opens with, since a string
// that is not an id at all reaches Postgres as a type error rather than as a row nobody holds.
func TestReadsAndWritesRefuseAnIDNoRowCouldHold(t *testing.T) {
	tests := []struct {
		name string
		call func(*fixture) error
	}{
		{
			name: "hiding one",
			call: func(f *fixture) error {
				changed, err := f.SetHidden(t.Context(), "not-a-uuid", true)
				assert.False(t, changed, "nothing was hidden")

				return err
			},
		},
		{
			name: "recording a fact about one",
			call: func(f *fixture) error {
				changed, err := f.SetMetadata(t.Context(), SetMetadataParams{
					ID: "not-a-uuid", Key: "pr", Value: xptr.Ptr("https://github.com/o/r/pull/1"),
				})
				assert.False(t, changed, "nothing was recorded")

				return err
			},
		},
		{
			name: "listing them",
			call: func(f *fixture) error {
				got, err := f.ListByID(t.Context(), []string{"not-a-uuid", ""})
				assert.Empty(t, got, "no row answers to either of those")

				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, tt.call(open(t)),
				"an id no row could hold is nothing to do rather than something to report")
		})
	}
}

// TestLookupsAnswerNotFoundRatherThanNothing pins the readers that speak for one agent, since a
// caller handed a nil agent and a nil error has to invent the refusal for itself.
func TestLookupsAnswerNotFoundRatherThanNothing(t *testing.T) {
	tests := []struct {
		name string
		read func(*fixture) error
	}{
		{
			name: "by a key nothing was created under",
			read: func(f *fixture) error {
				_, err := f.GetByIdempotencyKey(t.Context(), "")

				return err
			},
		},
		{
			name: "by a key no create recorded",
			read: func(f *fixture) error {
				_, err := f.GetByIdempotencyKey(t.Context(), "a key nobody used")

				return err
			},
		},
		{
			name: "by a name nobody in the workflow holds",
			read: func(f *fixture) error {
				_, err := f.GetInWorkflow(t.Context(), f.defaultWorkflow, "nobody")

				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.ErrorIs(t, tt.read(open(t)), store.ErrNotFound)
		})
	}
}
