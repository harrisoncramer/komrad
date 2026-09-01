package agent

import (
	"fmt"
	"sync"
	"testing"

	"weave/lib/xptr"

	"github.com/google/uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSetMetadata pins that a write lands on exactly one key and refuses an id belonging to
// no live agent, rather than reporting a success the agent would go on believing.
func TestSetMetadata(t *testing.T) {
	tests := []struct {
		name        string
		create      bool
		archive     bool
		id          func(created Agent) string
		before      []Metadata
		key         string
		value       *string
		wantChanged bool
		wantStored  []Metadata
	}{
		{
			name:  "an id no agent holds",
			id:    func(Agent) string { return uuid.NewString() },
			key:   "pr",
			value: xptr.Ptr("https://github.com/o/r/pull/1"),
		},
		{
			name:  "a value that is not an id at all",
			id:    func(Agent) string { return "not-an-id" },
			key:   "pr",
			value: xptr.Ptr("https://github.com/o/r/pull/1"),
		},
		{
			name:        "an archived agent, which is how an archive records what it was for",
			create:      true,
			archive:     true,
			id:          func(created Agent) string { return created.ID },
			key:         "pr",
			value:       xptr.Ptr("https://github.com/o/r/pull/1"),
			wantChanged: true,
			wantStored: []Metadata{
				{Key: "pr", Value: "https://github.com/o/r/pull/1"},
			},
		},
		{
			name:   "clearing a key the agent never held",
			create: true,
			id:     func(created Agent) string { return created.ID },
			key:    "pr",
		},
		{
			name:        "recording a fact",
			create:      true,
			id:          func(created Agent) string { return created.ID },
			key:         "pr",
			value:       xptr.Ptr("https://github.com/o/r/pull/1"),
			wantChanged: true,
			wantStored: []Metadata{
				{Key: "pr", Value: "https://github.com/o/r/pull/1"},
			},
		},
		{
			name:   "revising a fact leaves the agent's other keys standing",
			create: true,
			id:     func(created Agent) string { return created.ID },
			before: []Metadata{
				{Key: "issue", Value: "https://linear.app/x/issue/ENG-1"},
				{Key: "pr", Value: "https://github.com/o/r/pull/1"},
			},
			key:         "pr",
			value:       xptr.Ptr("https://github.com/o/r/pull/2"),
			wantChanged: true,
			wantStored: []Metadata{
				{Key: "issue", Value: "https://linear.app/x/issue/ENG-1"},
				{Key: "pr", Value: "https://github.com/o/r/pull/2"},
			},
		},
		{
			name:   "clearing one key leaves the rest",
			create: true,
			id:     func(created Agent) string { return created.ID },
			before: []Metadata{
				{Key: "issue", Value: "https://linear.app/x/issue/ENG-1"},
				{Key: "pr", Value: "https://github.com/o/r/pull/1"},
			},
			key:         "pr",
			wantChanged: true,
			wantStored: []Metadata{
				{Key: "issue", Value: "https://linear.app/x/issue/ENG-1"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			var created Agent
			if tt.create {
				require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateIdle}))
				got, err := st.Get(ctx, "agent")
				require.NoError(t, err)
				require.NotNil(t, got)
				created = *got
			}
			for _, entry := range tt.before {
				changed, err := st.SetMetadata(ctx, SetMetadataParams{
					ID:    created.ID,
					Key:   entry.Key,
					Value: xptr.Ptr(entry.Value),
				})
				require.NoError(t, err)
				require.True(t, changed)
			}
			if tt.archive {
				require.NoError(t, st.archiveByName(ctx, "agent"))
			}

			changed, err := st.SetMetadata(ctx, SetMetadataParams{
				ID:    tt.id(created),
				Key:   tt.key,
				Value: tt.value,
			})
			require.NoError(t, err)
			assert.Equal(t, tt.wantChanged, changed)

			if !tt.create || tt.archive {
				return
			}

			after, err := st.Get(ctx, "agent")
			require.NoError(t, err)
			require.NotNil(t, after)
			assert.Equal(t, tt.wantStored, after.Metadata)
		})
	}
}

// TestSetMetadataCeiling pins the cap on how many facts one agent may record, and that
// revising a fact it already recorded keeps working however full it is.
func TestSetMetadataCeiling(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateIdle}))
	created, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NotNil(t, created)

	for i := range maxMetadataKeys {
		changed, err := st.SetMetadata(ctx, SetMetadataParams{
			ID:    created.ID,
			Key:   fmt.Sprintf("key-%d", i),
			Value: xptr.Ptr("v"),
		})
		require.NoError(t, err)
		require.True(t, changed)
	}

	_, err = st.SetMetadata(ctx, SetMetadataParams{
		ID:    created.ID,
		Key:   "one-too-many",
		Value: xptr.Ptr("v"),
	})
	require.ErrorIs(t, err, ErrTooManyMetadataKeys)

	changed, err := st.SetMetadata(ctx, SetMetadataParams{
		ID:    created.ID,
		Key:   "key-0",
		Value: xptr.Ptr("revised"),
	})
	require.NoError(t, err, "revising a key the agent already holds is not a new key")
	assert.True(t, changed)
}

// TestSetMetadataCeilingUnderConcurrency drives more writers at one agent's key limit than fit, testing locking.
func TestSetMetadataCeilingUnderConcurrency(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, Agent{Name: "agent", State: StateIdle}))
	created, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NotNil(t, created)

	const (
		room    = 4
		writers = 16
	)

	for i := range maxMetadataKeys - room {
		changed, err := st.SetMetadata(ctx, SetMetadataParams{
			ID:    created.ID,
			Key:   fmt.Sprintf("key-%d", i),
			Value: xptr.Ptr("v"),
		})
		require.NoError(t, err)
		require.True(t, changed)
	}

	var wg sync.WaitGroup
	errs := make([]error, writers)

	for i := range writers {
		wg.Go(func() {
			_, err := st.SetMetadata(ctx, SetMetadataParams{
				ID:    created.ID,
				Key:   fmt.Sprintf("racer-%d", i),
				Value: xptr.Ptr("v"),
			})
			errs[i] = err
		})
	}

	wg.Wait()

	for i, err := range errs {
		if err != nil {
			require.ErrorIs(t, err, ErrTooManyMetadataKeys, "writer %d", i)
		}
	}

	after, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NotNil(t, after)
	assert.Len(t, after.Metadata, maxMetadataKeys, "only room many racers should have gotten past the limit")
}

// TestListPageHydratesMetadata pins that a listing carries each agent's own facts and
// nobody else's, since the panel renders straight off the listing.
func TestListPageHydratesMetadata(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	ids := make(map[string]string, 2)
	for _, name := range []string{"first", "second"} {
		require.NoError(t, st.Create(ctx, Agent{Name: name, State: StateIdle}))
		created, err := st.Get(ctx, name)
		require.NoError(t, err)
		require.NotNil(t, created)
		ids[name] = created.ID
	}

	for key, value := range map[string]string{"pr": "https://github.com/o/r/pull/1", "branch": "harry/x"} {
		changed, err := st.SetMetadata(ctx, SetMetadataParams{
			ID:    ids["first"],
			Key:   key,
			Value: xptr.Ptr(value),
		})
		require.NoError(t, err)
		require.True(t, changed)
	}

	listed, err := st.ListPage(ctx, 10, nil, false)
	require.NoError(t, err)
	require.Len(t, listed, 2)

	byName := make(map[string][]Metadata, len(listed))
	for _, agent := range listed {
		byName[agent.Name] = agent.Metadata
	}

	assert.Equal(t, []Metadata{
		{Key: "branch", Value: "harry/x"},
		{Key: "pr", Value: "https://github.com/o/r/pull/1"},
	}, byName["first"], "ordered by key, whatever order they were written in")
	assert.Empty(t, byName["second"], "an agent that recorded nothing carries nothing")
}
