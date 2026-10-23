package agent

import (
	"testing"

	"komrad/internal/store"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestIdempotencyKeyIsUnique pins that two agents cannot claim one key from the same requester while
// any number of them may claim none.
func TestIdempotencyKeyIsUnique(t *testing.T) {
	tests := []struct {
		name            string
		first           *string
		firstRequester  string
		second          *string
		secondRequester string
		wantFail        bool
	}{
		{
			name:     "the same key twice from a person is refused",
			first:    xptr.Ptr("launch-1"),
			second:   xptr.Ptr("launch-1"),
			wantFail: true,
		},
		{
			name:            "the same key twice from one requester is refused",
			first:           xptr.Ptr("launch-1"),
			firstRequester:  "requester",
			second:          xptr.Ptr("launch-1"),
			secondRequester: "requester",
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

			ids := map[string]*uuid.UUID{"": nil}
			for _, name := range []string{"requester", "other requester"} {
				require.NoError(t, st.Create(ctx, Agent{Name: name, State: agentv1.AgentState_AGENT_STATE_IDLE}))
				created, err := st.Get(ctx, name)
				require.NoError(t, err)
				require.NotNil(t, created)
				ids[name] = &created.ID
			}

			require.NoError(t, st.Create(ctx, Agent{
				Name:               "first",
				State:              agentv1.AgentState_AGENT_STATE_IDLE,
				IdempotencyKey:     tt.first,
				RequestedByAgentID: ids[tt.firstRequester],
			}))

			err := st.Create(ctx, Agent{
				Name:               "second",
				State:              agentv1.AgentState_AGENT_STATE_IDLE,
				IdempotencyKey:     tt.second,
				RequestedByAgentID: ids[tt.secondRequester],
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
