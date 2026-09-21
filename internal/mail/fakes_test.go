package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"weave/internal/harness"
	"weave/internal/logging"
	agentstore "weave/internal/store/agent"
	"weave/internal/storetest"

	"github.com/stretchr/testify/require"
)

const (
	idleFrame         = "frame: idle"
	composingFrame    = "frame: composing"
	unrecognizedFrame = "frame: unrecognized"
)

const testHarnessID = "claude"

type fakeHarness struct{}

func (f *fakeHarness) ID() string { return testHarnessID }

func (f *fakeHarness) Binary() string { return f.ID() }

func (f *fakeHarness) BuildCommand(launch harness.Launch) (string, error) {
	return "agent-cli --session " + launch.SessionID, nil
}

func (f *fakeHarness) MintSessionID() string { return "" }

func (f *fakeHarness) InterruptKeys() []string { return []string{"Escape"} }

func (f *fakeHarness) ReadReport(harness.Event, []byte) (harness.Report, error) {
	return harness.Report{}, nil
}

func (f *fakeHarness) ReadInput(frame string) harness.Input {
	switch frame {
	case composingFrame:
		return harness.Input{Activity: harness.ActivityComposing, Held: []string{"half a question about"}}
	case "":
		return harness.Input{Activity: harness.ActivityUnknown}
	case unrecognizedFrame:
		return harness.Input{Activity: harness.ActivityUnrecognized}
	default:
		return harness.Input{Activity: harness.ActivityIdle, Held: []string{""}}
	}
}

func (f *fakeHarness) LogPath(string) string { return "" }

func (f *fakeHarness) FindSession(string) (string, error) { return "", nil }

func (f *fakeHarness) ReadLog(string, int64) (harness.LogRead, error) {
	return harness.LogRead{}, nil
}

type fakeHarnesses struct {
	harness *fakeHarness
	only    []string
}

func (f fakeHarnesses) GetByID(id string) (harness.Harness, error) {
	if len(f.only) > 0 && !slices.Contains(f.only, id) {
		return nil, fmt.Errorf("%w: %q", harness.ErrNotRegistered, id)
	}

	return f.harness, nil
}

// seedIdler records a live idle agent whose state changed an hour ago.
func seedIdler(t *testing.T, st *storetest.Stores, name string) agentstore.Agent {
	t.Helper()

	agent := st.SeedAgent(t, agentstore.Agent{
		Name:    name,
		Pane:    "%1",
		State:   agentstore.StateIdle,
		Harness: testHarnessID,
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
