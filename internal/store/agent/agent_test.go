package agent

import (
	"context"
	"testing"

	"weave/internal/dbtest"
	"weave/internal/store"
	workflowstore "weave/internal/store/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSanitizeName pins which characters are rewritten to make a tmux window name.
func TestSanitizeName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "colon becomes a dash",
			in:   "a:b",
			want: "a-b",
		},
		{
			name: "dot becomes a dash",
			in:   "v1.0",
			want: "v1-0",
		},
		{
			name: "space becomes a dash",
			in:   "my agent",
			want: "my-agent",
		},
		{
			name: "a mix is fully sanitized",
			in:   "My Agent: v1.0",
			want: "My-Agent--v1-0",
		},
		{
			name: "an already-clean name is unchanged",
			in:   "clean-name",
			want: "clean-name",
		},
		{
			name: "a newline becomes a dash",
			in:   "ab\ncd",
			want: "ab-cd",
		},
		{
			name: "a delete becomes a dash",
			in:   "ab\x7fcd",
			want: "ab-cd",
		},
		{
			name: "characters above the ascii control range are kept",
			in:   "caf\u00e9\u00a0",
			want: "caf\u00e9\u00a0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, SanitizeName(tt.in))
		})
	}
}

type fixture struct {
	*AgentStore
	db              store.DBTX
	pool            *store.Pool
	workflows       *workflowstore.WorkflowStore
	defaultWorkflow string
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
	p := pool(t)

	workflows := workflowstore.NewWorkflowStore(p.GetDB())
	created, err := workflows.Create(t.Context(), workflowstore.Config{Name: "default", Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"}})
	require.NoError(t, err)

	return &fixture{
		AgentStore:      NewAgentStore(p.GetDB()),
		db:              p.GetDB(),
		pool:            p,
		workflows:       workflows,
		defaultWorkflow: created.ID,
	}
}

func pool(t *testing.T) *store.Pool {
	t.Helper()
	p, err := store.Open(t.Context(), dbtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	return p
}
