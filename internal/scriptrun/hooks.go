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

type hookRuns struct {
	count int
	idle  chan struct{}
}

// BeginHooks counts an agent's hooks as running until the returned func is called.
func (s *ScriptService) BeginHooks(agentID string) func() {
	s.beginHook(agentID)

	return sync.OnceFunc(func() { s.endHook(agentID) })
}

// AwaitHooks waits until none of an agent's hooks are running.
func (s *ScriptService) AwaitHooks(ctx context.Context, agentID string) error {
	s.hooksMu.Lock()
	runs, ok := s.hooksRunning[agentID]
	s.hooksMu.Unlock()

	if !ok {
		return nil
	}

	select {
	case <-runs.idle:
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

func (s *ScriptService) beginHook(agentID string) {
	s.hooksMu.Lock()
	defer s.hooksMu.Unlock()

	runs, ok := s.hooksRunning[agentID]
	if !ok {
		runs = &hookRuns{idle: make(chan struct{})}
		s.hooksRunning[agentID] = runs
	}
	runs.count++
}

func (s *ScriptService) endHook(agentID string) {
	s.hooksMu.Lock()
	defer s.hooksMu.Unlock()

	runs, ok := s.hooksRunning[agentID]
	if !ok {
		return
	}

	runs.count--
	if runs.count == 0 {
		close(runs.idle)
		delete(s.hooksRunning, agentID)
	}
}
