package agent

import (
	"testing"

	"weave/internal/store"
	"weave/lib/xptr"

	"github.com/stretchr/testify/require"
)

// TestIdempotencyKeyIsUnique pins that two agents cannot claim one key while any number of
// them may claim none.
func TestIdempotencyKeyIsUnique(t *testing.T) {
	tests := []struct {
		name     string
		first    *string
		second   *string
		wantFail bool
	}{
		{
			name:     "the same key twice is refused",
			first:    xptr.Ptr("launch-1"),
			second:   xptr.Ptr("launch-1"),
			wantFail: true,
		},
		{
			name:   "two different keys are fine",
			first:  xptr.Ptr("launch-1"),
			second: xptr.Ptr("launch-2"),
		},
		{
			name: "so are two agents claiming no key at all",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			require.NoError(t, st.Create(ctx, Agent{
				Name:           "first",
				State:          StateIdle,
				IdempotencyKey: tt.first,
			}))

			err := st.Create(ctx, Agent{
				Name:           "second",
				State:          StateIdle,
				IdempotencyKey: tt.second,
			})
			if tt.wantFail {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
		})
	}
}

// TestGetByIdempotencyKeyAnswersNotFoundForAKeyNoCreateRecorded pins store.ErrNotFound for a miss.
func TestGetByIdempotencyKeyAnswersNotFoundForAKeyNoCreateRecorded(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{name: "a key nothing was created under", key: ""},
		{name: "a key nobody used", key: "a key nobody used"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := open(t).GetByIdempotencyKey(t.Context(), tt.key)

			require.ErrorIs(t, err, store.ErrNotFound)
		})
	}
}
