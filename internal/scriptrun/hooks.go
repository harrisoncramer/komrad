package scriptrun

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"komrad/internal/scripts"

	agentstore "komrad/internal/store/agent"
)

// HookQueue orders each agent's hooks one moment at a time.
type HookQueue struct {
	mu    sync.Mutex
	tails map[string]chan struct{}
}

// HookTurn is one moment's place in an agent's hook queue.
type HookTurn struct {
	agentID string
	ahead   <-chan struct{}
	done    func()
}

// QueueHooks takes the next place in an agent's hook queue, which is held until Done.
func (q *HookQueue) QueueHooks(agentID string) *HookTurn {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.tails == nil {
		q.tails = make(map[string]chan struct{})
	}

	ahead := q.tails[agentID]
	mine := make(chan struct{})
	q.tails[agentID] = mine

	release := func() {
		q.mu.Lock()
		defer q.mu.Unlock()

		close(mine)
		if q.tails[agentID] == mine {
			delete(q.tails, agentID)
		}
	}

	return &HookTurn{
		agentID: agentID,
		ahead:   ahead,
		done: sync.OnceFunc(func() {
			if ahead == nil {
				release()

				return
			}

			select {
			case <-ahead:
				release()
			default:
				go func() {
					<-ahead
					release()
				}()
			}
		}),
	}
}

// AwaitHooks waits until every hook already queued for an agent has finished.
func (q *HookQueue) AwaitHooks(ctx context.Context, agentID string) error {
	q.mu.Lock()
	tail := q.tails[agentID]
	q.mu.Unlock()

	return awaitHooks(ctx, agentID, tail)
}

// Wait waits until every moment queued ahead of this one has finished.
func (t *HookTurn) Wait(ctx context.Context) error {
	return awaitHooks(ctx, t.agentID, t.ahead)
}

// Done gives up the place, once the moments ahead of it have finished.
func (t *HookTurn) Done() {
	t.done()
}

func awaitHooks(ctx context.Context, agentID string, ahead <-chan struct{}) error {
	if ahead == nil {
		return nil
	}

	select {
	case <-ahead:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("failed to wait for the hooks already running for agent %q: %w", agentID, context.Cause(ctx))
	}
}

// RunPostDeleteHook runs a hook for an agent whose row is gone, keeping no run record and
// logging its output to the service log.
func (s *ScriptService) RunPostDeleteHook(ctx context.Context, agent agentstore.Agent, name, event string) error {
	script, env, err := s.prepare(ctx, prepareParams{
		agent:   agent,
		name:    name,
		trigger: event,
		hook:    true,
	})
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrHookFailed, name, err)
	}

	dir := agent.Cwd
	if scripts.RequireUsableDir(dir) != nil {
		dir = script.GetDir()
	}

	logger := s.runLogger(ctx, name, event, agent)
	logger.LogAttrs(ctx, slog.LevelInfo, "script started")
	began := time.Now()

	result, err := s.runner.RunAndWait(ctx, scripts.RunParams{
		Script:  script,
		Dir:     dir,
		Env:     env,
		Timeout: script.Header.TimeoutOr(HookTimeout),
	})
	if err != nil {
		logger.LogAttrs(ctx, slog.LevelError, "script failed", failedScript(result, err)...)

		return fmt.Errorf("%w: %s: %w%s", ErrHookFailed, name, err, scriptDetail(result.Stdout, result.Stderr, ""))
	}

	logger.LogAttrs(ctx, slog.LevelInfo, "script finished", finishedScript(result, began)...)

	return nil
}
