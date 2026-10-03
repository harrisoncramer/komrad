package agent

import (
	"testing"

	"komrad/internal/store"
	"komrad/lib/xptr"

	"github.com/stretchr/testify/require"
)

// TestIdempotencyKeyIsUnique pins that two agents cannot claim one key from the same spawner while
// any number of them may claim none.
func TestIdempotencyKeyIsUnique(t *testing.T) {
	tests := []struct {
		name          string
		first         *string
		firstSpawner  string
		second        *string
		secondSpawner string
		wantFail      bool
	}{
		{
			name:     "the same key twice from a person is refused",
			first:    xptr.Ptr("launch-1"),
			second:   xptr.Ptr("launch-1"),
			wantFail: true,
		},
		{
			name:          "the same key twice from one spawner is refused",
			first:         xptr.Ptr("launch-1"),
			firstSpawner:  "spawner",
			second:        xptr.Ptr("launch-1"),
			secondSpawner: "spawner",
			wantFail:      true,
		},
		{
			name:          "the same key from two spawners is fine",
			first:         xptr.Ptr("launch-1"),
			firstSpawner:  "spawner",
			second:        xptr.Ptr("launch-1"),
			secondSpawner: "other spawner",
		},
		{
			name:          "nor does a person's key collide with a spawner's",
			first:         xptr.Ptr("launch-1"),
			second:        xptr.Ptr("launch-1"),
			secondSpawner: "spawner",
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

			spawners := map[string]*string{"": nil}
			for _, name := range []string{"spawner", "other spawner"} {
				require.NoError(t, st.Create(ctx, Agent{Name: name, State: StateIdle}))
				spawner, err := st.Get(ctx, name)
				require.NoError(t, err)
				require.NotNil(t, spawner)
				spawners[name] = &spawner.ID
			}

			require.NoError(t, st.Create(ctx, Agent{
				Name:             "first",
				State:            StateIdle,
				IdempotencyKey:   tt.first,
				SpawnedByAgentID: spawners[tt.firstSpawner],
			}))

			err := st.Create(ctx, Agent{
				Name:             "second",
				State:            StateIdle,
				IdempotencyKey:   tt.second,
				SpawnedByAgentID: spawners[tt.secondSpawner],
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
			_, err := open(t).GetByIdempotencyKey(t.Context(), tt.key, nil)

			require.ErrorIs(t, err, store.ErrNotFound)
		})
	}
}
