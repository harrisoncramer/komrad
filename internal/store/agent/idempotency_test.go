package agent

import (
	"testing"

	"weave/internal/store"
	"weave/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetByIdempotencyKey pins that the lookup spans archived agents, since a retry has to
// find what it already created however that agent ended up.
func TestGetByIdempotencyKey(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		archive bool
		lookFor string
		wantHit bool
	}{
		{
			name:    "the key an agent holds finds it",
			key:     "schedule:s1:2026-07-28T09:00:00Z",
			lookFor: "schedule:s1:2026-07-28T09:00:00Z",
			wantHit: true,
		},
		{
			name:    "and still finds it after the agent is archived",
			key:     "schedule:s1:2026-07-28T09:00:00Z",
			archive: true,
			lookFor: "schedule:s1:2026-07-28T09:00:00Z",
			wantHit: true,
		},
		{
			name:    "a key nobody holds finds nothing",
			key:     "schedule:s1:2026-07-28T09:00:00Z",
			lookFor: "schedule:s1:2026-07-29T09:00:00Z",
		},
		{
			name:    "an empty key is not a claim, so it matches nothing",
			key:     "schedule:s1:2026-07-28T09:00:00Z",
			lookFor: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			require.NoError(t, st.Create(ctx, Agent{
				Name:           "agent",
				State:          StateIdle,
				IdempotencyKey: &tt.key,
			}))
			if tt.archive {
				require.NoError(t, st.archiveByName(ctx, "agent"))
			}

			got, err := st.GetByIdempotencyKey(ctx, tt.lookFor)

			if !tt.wantHit {
				assert.ErrorIs(t, err, store.ErrNotFound)

				return
			}

			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, "agent", got.Name)
			require.NotNil(t, got.IdempotencyKey)
			assert.Equal(t, tt.key, *got.IdempotencyKey)
		})
	}
}

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
