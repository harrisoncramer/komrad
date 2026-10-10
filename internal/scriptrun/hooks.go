package scriptrun

import (
	"context"
	"fmt"
	"sync"
)

// HookQueue orders the hooks queued under each key, an agent's id or a task's, one moment at a time.
type HookQueue struct {
	mu    sync.Mutex
	tails map[string]chan struct{}
}

// HookTurn is one moment's place in a key's hook queue.
type HookTurn struct {
	key   string
	ahead <-chan struct{}
	done  func()
}

// QueueHooks takes the next place in a key's hook queue, which is held until Done.
func (q *HookQueue) QueueHooks(key string) *HookTurn {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.tails == nil {
		q.tails = make(map[string]chan struct{})
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
func (q *HookQueue) AwaitHooks(ctx context.Context, key string) error {
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

func awaitHooks(ctx context.Context, key string, ahead <-chan struct{}) error {
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
