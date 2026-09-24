package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"weave/internal/harnesstest"
	"weave/internal/logging"
	agentstore "weave/internal/store/agent"
	"weave/internal/storetest"

	"github.com/stretchr/testify/require"
)

// seedIdler records a live idle agent whose state changed an hour ago.
func seedIdler(t *testing.T, st *storetest.Stores, name string) agentstore.Agent {
	t.Helper()

	agent := st.SeedAgent(t, agentstore.Agent{
		Name:    name,
		Pane:    "%1",
		State:   agentstore.StateIdle,
		Harness: harnesstest.FakeID,
	})
	_, err := st.Pool.GetDB().Exec(t.Context(),
		`UPDATE agents SET state_changed_at = NOW() - $2::interval WHERE id = $1`, agent.ID, time.Hour.String())
	require.NoError(t, err)

	return *agent
}

func backdateNudge(t *testing.T, st *storetest.Stores, id string, age time.Duration) {
	t.Helper()
	_, err := st.Pool.GetDB().Exec(t.Context(),
		`UPDATE agents SET nudged_at = NOW() - $2::interval WHERE id = $1`, id, age.String())
	require.NoError(t, err)
}

func agentByID(t *testing.T, st *storetest.Stores, id string) agentstore.Agent {
	t.Helper()

	all, err := st.Agents.ListAll(t.Context())
	require.NoError(t, err)

	for _, agent := range all {
		if agent.ID == id {
			return agent
		}
	}

	t.Fatalf("no agent with id %q", id)

	return agentstore.Agent{}
}

// loggedContext is a context whose log lines a test can read back as records.
func loggedContext(t *testing.T) (context.Context, func() []map[string]any) {
	t.Helper()

	var written bytes.Buffer

	ctx := logging.WithLogger(t.Context(), logging.New(&written, slog.LevelDebug))

	return ctx, func() []map[string]any {
		records := make([]map[string]any, 0, 2)
		for _, line := range strings.Split(strings.TrimSpace(written.String()), "\n") {
			if line == "" {
				continue
			}

			var record map[string]any
			require.NoError(t, json.Unmarshal([]byte(line), &record))
			records = append(records, record)
		}

		return records
	}
}
