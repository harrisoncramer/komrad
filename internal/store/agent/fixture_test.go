package agent

import (
	"context"
	"testing"

	"weave/internal/dbtest"
	"weave/internal/store"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/watchtest"

	"github.com/stretchr/testify/require"
)

type fixture struct {
	*AgentStore
	db              store.DBTX
	pool            *store.Pool
	workflows       *workflowstore.WorkflowStore
	defaultWorkflow string
	touched         *watchtest.Recorder
}

// Get reads an agent back by name out of the default workflow.
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

	return f.AgentStore.Create(ctx, agent)
}

func open(t *testing.T) *fixture {
	t.Helper()
	touched := watchtest.NewRecorder()
	p := pool(t, touched)

	workflows := workflowstore.NewWorkflowStore(p.GetDB())
	created, err := workflows.Create(t.Context(), workflowstore.Config{Name: "default", Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"}})
	require.NoError(t, err)

	return &fixture{
		AgentStore:      NewAgentStore(p.GetDB()),
		db:              p.GetDB(),
		pool:            p,
		workflows:       workflows,
		defaultWorkflow: created.ID,
		touched:         touched,
	}
}

func pool(t *testing.T, touched store.Publisher) *store.Pool {
	t.Helper()
	p, err := store.Open(t.Context(), store.OpenParams{URL: dbtest.URL(t), Publisher: touched})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	return p
}
