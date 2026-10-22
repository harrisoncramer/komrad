package scriptrun

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"komrad/internal/logging"

	agentstore "komrad/internal/store/agent"
	workflowstore "komrad/internal/store/workflow"

	"github.com/google/uuid"
)

// HookRunner runs a workflow's hooks, each in its turn among those queued under the same key.
type HookRunner interface {
	RunHook(ctx context.Context, agent agentstore.Agent, name, event string) error
	AwaitHooks(ctx context.Context, key uuid.UUID) error
	QueueHooks(key uuid.UUID) *HookTurn
}

// HookRun is one moment's worth of work: the hooks a workflow lists, the agent they run against,
// and the moment itself.
type HookRun struct {
	Scripts HookRunner
	Agent   agentstore.Agent
	Hooks   []workflowstore.Hook
	Event   string
}

// HookQueue orders the hooks queued under each key, an agent's id or a task's, one moment at a time.
type HookQueue struct {
	mu    sync.Mutex
	tails map[uuid.UUID]chan struct{}
}

// HookTurn is one moment's place in a key's hook queue.
type HookTurn struct {
	key   uuid.UUID
	ahead <-chan struct{}
	done  func()
}

// QueueHooks takes the next place in a key's hook queue, which is held until Done.
func (q *HookQueue) QueueHooks(key uuid.UUID) *HookTurn {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.tails == nil {
		q.tails = make(map[uuid.UUID]chan struct{})
	}

	ahead := q.tails[key]
	mine := make(chan struct{})
	q.tails[key] = mine

	release := func() {
		q.mu.Lock()
		defer q.mu.Unlock()

		close(mine)
		if q.tails[key] == mine {
			delete(q.tails, key)
		}
	}

	return &HookTurn{
		key:   key,
		ahead: ahead,
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

// AwaitHooks waits until every hook already queued under a key has finished.
func (q *HookQueue) AwaitHooks(ctx context.Context, key uuid.UUID) error {
	q.mu.Lock()
	tail := q.tails[key]
	q.mu.Unlock()

	return awaitHooks(ctx, key, tail)
}

// Wait waits until every moment queued ahead of this one has finished.
func (t *HookTurn) Wait(ctx context.Context) error {
	return awaitHooks(ctx, t.key, t.ahead)
}

// Done gives up the place, once the moments ahead of it have finished.
func (t *HookTurn) Done() {
	t.done()
}

// Run runs the hooks on one moment in order, waiting for each and stopping at the first that fails.
func (r HookRun) Run(ctx context.Context) error {
	if r.Scripts == nil {
		return nil
	}

	for _, hook := range r.Hooks {
		if hook.Event != r.Event {
			continue
		}

		if err := r.Scripts.RunHook(ctx, r.Agent, hook.Script, r.Event); err != nil {
			return err
		}
	}

	return nil
}

// RunInTurn runs the hooks on one moment once those queued ahead of it under the same key have finished.
func (r HookRun) RunInTurn(ctx context.Context) error {
	if r.Scripts == nil || !HasHookOn(r.Hooks, r.Event) {
		return nil
	}

	turn := r.Scripts.QueueHooks(r.queueKey())
	defer turn.Done()

	if err := turn.Wait(ctx); err != nil {
		return err
	}

	return r.Run(ctx)
}

// Start runs a moment's hooks in the background.
func (r HookRun) Start(ctx context.Context) {
	if r.Scripts == nil || !HasHookOn(r.Hooks, r.Event) {
		return
	}

	bg := context.WithoutCancel(ctx)
	turn := r.Scripts.QueueHooks(r.queueKey())

	go func() {
		defer turn.Done()

		if err := turn.Wait(bg); err != nil {
			return
		}

		if err := r.Run(bg); err != nil {
			logging.From(bg).LogAttrs(bg, slog.LevelError, "a hook failed",
				append([]slog.Attr{
					slog.String(logging.RunTrigger, r.Event),
					slog.String(logging.ScriptAgentID, r.Agent.ID.String()),
				}, logging.Err(err)...)...)
		}
	}()
}

// queueKey is what a moment's hooks queue under, the task's id for a moment in a task's life and the agent's otherwise.
func (r HookRun) queueKey() uuid.UUID {
	if workflowstore.IsTaskEvent(r.Event) && r.Agent.TaskID != nil {
		return *r.Agent.TaskID
	}

	return r.Agent.ID
}

// HasHookOn reports whether a workflow hangs any script on one moment.
func HasHookOn(hooks []workflowstore.Hook, event string) bool {
	return slices.ContainsFunc(hooks, func(hook workflowstore.Hook) bool { return hook.Event == event })
}

func awaitHooks(ctx context.Context, key uuid.UUID, ahead <-chan struct{}) error {
	if ahead == nil {
		return nil
	}

	select {
	case <-ahead:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("failed to wait for the hooks already running under %q: %w", key, context.Cause(ctx))
	}
}
