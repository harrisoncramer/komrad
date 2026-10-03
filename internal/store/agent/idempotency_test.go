package agent

import (
	"testing"

	"komrad/internal/store"
	"komrad/lib/xptr"

	"github.com/stretchr/testify/require"
)

// TestIdempotencyKeyIsUnique pins that two agents cannot claim one key from the same requester under
// the same parent while any number of them may claim none.
func TestIdempotencyKeyIsUnique(t *testing.T) {
	tests := []struct {
		name            string
		first           *string
		firstRequester  string
		firstParent     string
		second          *string
		secondRequester string
		secondParent    string
		wantFail        bool
	}{
		{
			name:     "the same key twice from a person is refused",
			first:    xptr.Ptr("launch-1"),
			second:   xptr.Ptr("launch-1"),
			wantFail: true,
		},
		{
			name:            "the same key twice from one requester under one parent is refused",
			first:           xptr.Ptr("launch-1"),
			firstRequester:  "requester",
			firstParent:     "parent",
			second:          xptr.Ptr("launch-1"),
			secondRequester: "requester",
			secondParent:    "parent",
			wantFail:        true,
		},
		{
			name:            "the same key from two requesters is fine",
			first:           xptr.Ptr("launch-1"),
			firstRequester:  "requester",
			second:          xptr.Ptr("launch-1"),
			secondRequester: "other requester",
		},
		{
			name:            "the same key from a person and a requester is fine",
			first:           xptr.Ptr("launch-1"),
			second:          xptr.Ptr("launch-1"),
			secondRequester: "requester",
		},
		{
			name:            "the same key from one requester under two parents is fine",
			first:           xptr.Ptr("launch-1"),
			firstRequester:  "requester",
			firstParent:     "parent",
			second:          xptr.Ptr("launch-1"),
			secondRequester: "requester",
			secondParent:    "other parent",
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

			ids := map[string]*string{"": nil}
			for _, name := range []string{"requester", "other requester", "parent", "other parent"} {
				require.NoError(t, st.Create(ctx, Agent{Name: name, State: StateIdle}))
				created, err := st.Get(ctx, name)
				require.NoError(t, err)
				require.NotNil(t, created)
				ids[name] = &created.ID
			}

			require.NoError(t, st.Create(ctx, Agent{
				Name:               "first",
				State:              StateIdle,
				IdempotencyKey:     tt.first,
				RequestedByAgentID: ids[tt.firstRequester],
				ParentAgentID:      ids[tt.firstParent],
			}))

			err := st.Create(ctx, Agent{
				Name:               "second",
				State:              StateIdle,
				IdempotencyKey:     tt.second,
				RequestedByAgentID: ids[tt.secondRequester],
				ParentAgentID:      ids[tt.secondParent],
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
			_, err := open(t).GetByIdempotencyKey(t.Context(), IdempotencyKey{Key: tt.key})

			require.ErrorIs(t, err, store.ErrNotFound)
		})
	}
}
