package agents

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNewAgentCeilingUnderConcurrency drives more creates than fit at once, since every
// agent past the ceiling is a real CLI in a real pane nobody agreed to.
func TestNewAgentCeilingUnderConcurrency(t *testing.T) {
	t.Parallel()

	const (
		ceiling = 2
		callers = 8
	)

	tmux := newFakeTmux()
	st := newStore(t)
	manager := st.agentServiceCapped(tmux, &fakeLifecycle{}, ceiling)
	workflowID := seedWorkflow(t, st)
	dir := t.TempDir()

	var wg sync.WaitGroup
	errs := make([]error, callers)

	for i := range callers {
		wg.Add(1)

		go func() {
			defer wg.Done()

			_, err := manager.NewAgent(t.Context(), NewAgentParams{
				Title:      fmt.Sprintf("racer-%d", i),
				Dir:        dir,
				WorkflowID: workflowID,
			})
			errs[i] = err
		}()
	}

	wg.Wait()

	live, err := st.Agents.CountLive(t.Context())
	require.NoError(t, err)
	require.Equal(t, int64(ceiling), live)

	for i, err := range errs {
		if err != nil {
			require.ErrorIs(t, err, ErrTooManyAgents, "caller %d", i)
		}
	}

	panes, err := tmux.GetPanes()
	require.NoError(t, err)
	require.Len(t, panes, ceiling)
}
