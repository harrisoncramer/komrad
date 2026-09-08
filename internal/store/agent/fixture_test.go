package agent

import (
	"context"
	"testing"

	"weave/internal/pgtest"
	"weave/internal/store"
	workflowstore "weave/internal/store/workflow"

	"github.com/stretchr/testify/require"
)

type fixture struct {
	*Store
	db              store.DBTX
	pool            *store.Pool
	workflows       *workflowstore.Store
	defaultWorkflow string
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
