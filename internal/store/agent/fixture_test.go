package agent

import (
	"context"
	"testing"

	"weave/internal/pgtest"
	"weave/internal/store"
	schedulestore "weave/internal/store/schedule"
	workflowstore "weave/internal/store/workflow"

	"github.com/google/uuid"

	"github.com/stretchr/testify/require"
)

type fixture struct {
	*Store
	db        store.DBTX
	pool      *store.Pool
	workflows *workflowstore.Store

	defaultWorkflow string
}

func agentIDs(agents []Agent) []string {
	ids := make([]string, 0, len(agents))
	for _, agent := range agents {
		ids = append(ids, agent.ID)
	}

	return ids
}

// Get reads an agent back by name out of the default workflow, which is the scope a
// name is unique in.
func (f *fixture) Get(ctx context.Context, name string) (*Agent, error) {
	return f.GetInWorkflow(ctx, f.defaultWorkflow, name)
}

func (f *fixture) archiveByName(ctx context.Context, name string) error {
	agent, err := f.GetInWorkflow(ctx, f.defaultWorkflow, name)
	if err != nil {
		return err
	}

	_, err = f.Archive(ctx, []string{agent.ID})

	return err
}

// Create fills in the default workflow when the test did not name one.
func (f *fixture) Create(ctx context.Context, agent Agent) error {
	if agent.WorkflowID == "" {
		agent.WorkflowID = f.defaultWorkflow
	}

	return f.Store.Create(ctx, agent)
}

func open(t *testing.T) *fixture {
	t.Helper()
	p := pool(t)

	workflows := workflowstore.New(p.GetDB())
	created, err := workflows.Create(t.Context(), workflowstore.Config{Name: "default"})
	require.NoError(t, err)

	return &fixture{
		Store:           New(p.GetDB()),
		db:              p.GetDB(),
		pool:            p,
		workflows:       workflows,
		defaultWorkflow: created.ID,
	}
}

func pool(t *testing.T) *store.Pool {
	t.Helper()
	p, err := store.Open(t.Context(), pgtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	return p
}

func seedSchedule(t *testing.T, st *fixture) string {
	t.Helper()

	created, err := schedulestore.New(st.db).Create(t.Context(), schedulestore.Config{
		Name:       "schedule-" + uuid.NewString(),
		Cron:       "0 9 * * *",
		Prompt:     "go",
		WorkflowID: st.defaultWorkflow,
	})
	require.NoError(t, err)

	return created.ID
}
