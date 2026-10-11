package scriptrun

import (
	"context"
	"log/slog"
	"slices"

	"komrad/internal/logging"

	agentstore "komrad/internal/store/agent"
	workflowstore "komrad/internal/store/workflow"
)

const (
	// WorktreeProvisioner is the built-in script that makes a task's worktree.
	WorktreeProvisioner = "_worktree_setup"
	// WorktreeDeprovisioner is the built-in script that gives a task's worktree back.
	WorktreeDeprovisioner = "_worktree_cleanup"
)

// HookRunner runs a workflow's hooks, each in its turn among those queued under the same key.
type HookRunner interface {
	RunHook(ctx context.Context, agent agentstore.Agent, name, event string) error
	AwaitHooks(ctx context.Context, key string) error
	QueueHooks(key string) *HookTurn
}

// HookRun is one moment's worth of work: the hooks a workflow lists, the agent they run against,
// and the moment itself.
type HookRun struct {
	Scripts HookRunner
	Agent   agentstore.Agent
	Hooks   []workflowstore.Hook
	Event   string
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
					slog.String(logging.ScriptAgentID, r.Agent.ID),
				}, logging.Err(err)...)...)
		}
	}()
}

// queueKey is what a moment's hooks queue under, the task's id for a moment in a task's life and the agent's otherwise.
func (r HookRun) queueKey() string {
	if workflowstore.IsTaskEvent(r.Event) && r.Agent.TaskID != nil {
		return *r.Agent.TaskID
	}

	return r.Agent.ID
}

// HasHookOn reports whether a workflow hangs any script on one moment.
func HasHookOn(hooks []workflowstore.Hook, event string) bool {
	return slices.ContainsFunc(hooks, func(hook workflowstore.Hook) bool { return hook.Event == event })
}

// WorktreeScripts is the provisioner and deprovisioner a workflow gives an agent, the built-in pair or nothing at all.
func WorktreeScripts(wf *workflowstore.Workflow, provisionEnabled *bool) (provisioner, deprovisioner string) {
	if wf == nil || !wf.Worktree.Enabled || agentstore.IsProvisionerDisabled(provisionEnabled) {
		return "", ""
	}

	return WorktreeProvisioner, WorktreeDeprovisioner
}
