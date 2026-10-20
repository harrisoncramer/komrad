package agents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"time"

	"komrad/config"
	"komrad/internal/agenterr"
	"komrad/internal/fail"
	"komrad/internal/frames"
	"komrad/internal/logging"
	"komrad/internal/scriptrun"
	"komrad/internal/scripts"
	"komrad/internal/store"
	"komrad/internal/tasks"
	"komrad/lib/xslice"
	"komrad/lib/xstr"

	agentstore "komrad/internal/store/agent"
	reviewstore "komrad/internal/store/review"
	taskstore "komrad/internal/store/task"
	transcriptstore "komrad/internal/store/transcript"
	workflowstore "komrad/internal/store/workflow"
)

// errArchiveNotYours is an agent other than an orchestrator archiving an agent that is neither itself
// nor, for a writer, a reviewer on its task.
var errArchiveNotYours = fail.Denied("an agent may archive only itself and, as a writer, the reviewers on its task")

type NewLifecycleServiceParams struct {
	Tmux           paneKiller
	Agents         *agentstore.AgentStore
	Workflows      *workflowstore.WorkflowStore
	EmptyWorkflows *EmptyWorkflowDeleter
	AgentDeleter   workflowAgentDeleter
	Tx             txRunner
	Scripts        agentScriptRunner
	Reviews        *reviewstore.ReviewStore
	Screens        screenKeeper
	LogsDir        string
	FramesDir      string
	Tasks          *tasks.TaskService
	Usage          config.Usage
}

// LifecycleService moves agents between the live listing, the archive and gone.
type LifecycleService struct {
	tmux           paneKiller
	agents         *agentstore.AgentStore
	workflows      *workflowstore.WorkflowStore
	emptyWorkflows *EmptyWorkflowDeleter
	agentDeleter   workflowAgentDeleter
	tx             txRunner
	scripts        agentScriptRunner
	reviews        *reviewstore.ReviewStore
	screens        screenKeeper
	logsDir        string
	framesDir      string
	usage          config.Usage
	tasks          *tasks.TaskService
}

// NewLifecycleService builds the service from its dependencies.
func NewLifecycleService(params NewLifecycleServiceParams) *LifecycleService {
	return &LifecycleService{
		tmux:           params.Tmux,
		agents:         params.Agents,
		workflows:      params.Workflows,
		emptyWorkflows: params.EmptyWorkflows,
		agentDeleter:   params.AgentDeleter,
		tx:             params.Tx,
		scripts:        params.Scripts,
		reviews:        params.Reviews,
		screens:        params.Screens,
		logsDir:        params.LogsDir,
		framesDir:      params.FramesDir,
		usage:          params.Usage,
		tasks:          params.Tasks,
	}
}

// DeleteAgent deletes an agent and, for a writer, the reviewers on its task the same claim took,
// and does nothing to an agent that claim no longer holds.
func (s *LifecycleService) DeleteAgent(ctx context.Context, id string, claim time.Time) error {
	named, err := s.claimedBy(ctx, id, claim)
	if err != nil || len(named) == 0 {
		return err
	}

	reviewers, err := s.listTaskReviewers(ctx, named[0])
	if err != nil {
		return err
	}

	agents := named
	for _, reviewer := range reviewers {
		if reviewer.DeletingAt != nil && !reviewer.DeletingAt.Equal(claim) {
			return agenterr.ErrReviewerDeleting.Saying("agent %s waits on the delete of reviewer %s", id, reviewer.ID)
		}

		agents = append(agents, reviewer)
	}

	return s.deleteAgents(ctx, agents)
}

// listTaskReviewers returns the reviewers on a writer's task, archived ones included, and none for
// any other agent.
func (s *LifecycleService) listTaskReviewers(ctx context.Context, agent agentstore.Agent) ([]agentstore.Agent, error) {
	if !isTaskWriter(agent) {
		return nil, nil
	}

	onTask, err := s.agents.ListOnTasks(ctx, []string{*agent.TaskID})
	if err != nil {
		return nil, err
	}

	return xslice.Filter(onTask, func(other agentstore.Agent) bool {
		return other.Role == agentstore.RoleReviewer
	}), nil
}

// deleteNow claims agents and runs the delete of each root the claim took, as the queued jobs do.
func (s *LifecycleService) deleteNow(ctx context.Context, ids []string) error {
	claims, err := s.agents.MarkDeleting(ctx, ids)
	if err != nil {
		return err
	}

	return errors.Join(xslice.Map(agentstore.ClaimRoots(claims), func(root agentstore.Claim) error {
		return s.DeleteAgent(ctx, root.ID, root.DeletingAt)
	})...)
}

// deleteAgents stops agents, tears each down, and removes those whose teardown worked,
// returning only once every teardown has finished.
func (s *LifecycleService) deleteAgents(ctx context.Context, agents []agentstore.Agent) error {
	// A pre-delete hook must not run against a directory a CLI is still working in.
	killOwnedPanes(ctx, s.tmux, agents)

	hooks := make(map[string][]workflowstore.Hook, len(agents))
	var torn []agentstore.Agent
	var failed []error
	for _, agent := range slices.Backward(agents) {
		hooks[agent.ID] = agentHooks(ctx, s.workflows, agent)
		if err := s.tearDown(ctx, agent, hooks[agent.ID]); err != nil {
			s.failCleanup(context.WithoutCancel(ctx), agent, err)
			failed = append(failed, fmt.Errorf("%w: agent %s: %w", agenterr.ErrTeardownFailed, agent.ID, err))

			continue
		}

		torn = append(torn, agent)
	}

	if len(torn) == 0 {
		return errors.Join(failed...)
	}

	if err := s.removeAgents(ctx, agentstore.IDs(torn)); err != nil {
		return err
	}

	s.cleanupEmptyTasks(ctx, torn)

	for _, agent := range torn {
		scriptrun.HookRun{
			Scripts: s.scripts,
			Agent:   agent,
			Hooks:   hooks[agent.ID],
			Event:   workflowstore.HookAgentPostDelete,
		}.Start(ctx)
	}

	s.cleanupEmptyWorkflows(ctx, torn)

	return errors.Join(failed...)
}

// FailDelete gives up on deleting an agent the claim still holds, leaving it in the archive as
// failed to clean up.
func (s *LifecycleService) FailDelete(ctx context.Context, id string, claim time.Time, cause error) error {
	named, err := s.claimedBy(ctx, id, claim)
	if err != nil {
		return err
	}

	for _, agent := range named {
		s.failCleanup(ctx, agent, cause)
	}

	return nil
}

func (s *LifecycleService) claimedBy(ctx context.Context, id string, claim time.Time) ([]agentstore.Agent, error) {
	named, err := s.agents.ListByID(ctx, []string{id})
	if err != nil {
		return nil, fmt.Errorf("failed to look up agent %s: %w", id, err)
	}

	return xslice.Filter(named, func(agent agentstore.Agent) bool {
		return agent.DeletingAt != nil && agent.DeletingAt.Equal(claim)
	}), nil
}

// tearDown waits out an agent's hooks still running, then runs its pre-delete hooks while its
// directory is still there.
func (s *LifecycleService) tearDown(ctx context.Context, agent agentstore.Agent, hooks []workflowstore.Hook) error {
	if s.scripts == nil {
		return nil
	}

	if err := s.scripts.AwaitHooks(ctx, agent.ID); err != nil {
		return err
	}

	// A hook has nothing to run against once the directory has gone.
	if scripts.RequireUsableDir(agent.Cwd) == nil {
		return scriptrun.HookRun{
			Scripts: s.scripts,
			Agent:   agent,
			Hooks:   hooks,
			Event:   workflowstore.HookAgentPreDelete,
		}.RunInTurn(ctx)
	}

	return nil
}

func (s *LifecycleService) failCleanup(ctx context.Context, agent agentstore.Agent, cause error) {
	failAs(ctx, failAsParams{
		agents: s.agents,
		agent:  agent,
		state:  agentstore.StateFailedToCleanup,
		verb:   "cleanup",
		cause:  cause,
	})

	if err := s.agents.ClearDeleting(ctx, []string{agent.ID}); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to release an agent whose cleanup failed",
			append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)
	}
}

// cleanupEmptyTasks deletes each task among removed's that is being deleted and now has no agent
// on it, logging a failure rather than returning it.
func (s *LifecycleService) cleanupEmptyTasks(ctx context.Context, removed []agentstore.Agent) {
	for _, taskID := range agentstore.TaskIDs(removed) {
		if err := s.tasks.DeleteIfEmpty(ctx, taskID); err != nil {
			logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to delete a task a delete left with no agent on it",
				append([]slog.Attr{slog.String(logging.TaskID, taskID)}, logging.Err(err)...)...)
		}
	}
}

// cleanupEmptyWorkflows deletes each workflow among removed's that now holds nothing archived
// inside it, logging a failure rather than returning it.
func (s *LifecycleService) cleanupEmptyWorkflows(ctx context.Context, removed []agentstore.Agent) {
	touched := xslice.UniqueBy(removed, func(agent agentstore.Agent) string { return agent.WorkflowID })
	for _, agent := range touched {
		if err := s.emptyWorkflows.DeleteIfEmpty(ctx, agent.WorkflowID); err != nil {
			logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to delete a workflow a delete left with nothing in it",
				append([]slog.Attr{slog.String(logging.WorkflowID, agent.WorkflowID)}, logging.Err(err)...)...)
		}
	}
}

// ArchiveAgentsParams is the agents to archive and the agent asking.
type ArchiveAgentsParams struct {
	IDs []string
	// ByAgentID is the agent asking, empty for the panel.
	ByAgentID string
}

// ArchiveAgents stops agents while preserving their records and workspaces, refusing a task's
// writer, which goes only with its task.
func (s *LifecycleService) ArchiveAgents(ctx context.Context, params ArchiveAgentsParams) error {
	listed, err := s.agents.ListByID(ctx, params.IDs)
	if err != nil {
		return fmt.Errorf("failed to look up %d agents: %w", len(params.IDs), err)
	}

	if err := s.refuseArchivingOthers(ctx, params, listed); err != nil {
		return err
	}

	named, err := requireListed(params.IDs, listed)
	if err != nil {
		return err
	}

	if err := refuseWriters(named, "archive"); err != nil {
		return err
	}

	return s.archiveAgents(ctx, named)
}

// archiveAgents stops agents while preserving their records and workspaces.
func (s *LifecycleService) archiveAgents(ctx context.Context, agents []agentstore.Agent) error {
	if err := archiveBatch(ctx, archiveBatchParams{
		tmux:      s.tmux,
		scripts:   s.scripts,
		workflows: s.workflows,
		tasks:     s.tasks,
		live:      xslice.Filter(agents, func(agent agentstore.Agent) bool { return agent.ArchivedAt == nil }),
		owned:     agents,
		screens:   s.screens,
		archive: func(ctx context.Context) (*archiveTaken, error) {
			taken := &archiveTaken{}
			err := s.tx.InTx(ctx, func(db store.DBTX) error {
				panes, err := agentstore.NewAgentStore(db).Archive(ctx, agentstore.IDs(agents))
				if err != nil {
					return err
				}
				taken.panes = panes

				taken.tasks, err = taskstore.NewTaskStore(db).ArchiveIdle(ctx, agentstore.TaskIDs(agents))

				return err
			})

			return taken, err
		},
	}); err != nil {
		return fmt.Errorf("failed to archive %d agents: %w", len(agents), err)
	}

	return nil
}

// ArchiveTask archives the task an id or a slug names along with the live agents working on it.
func (s *LifecycleService) ArchiveTask(ctx context.Context, ref string) error {
	found, err := s.tasks.GetTask(ctx, ref)
	if err != nil {
		return err
	}

	live, err := s.agents.ListLiveOnTask(ctx, found.ID)
	if err != nil {
		return err
	}
	if len(live) > 0 {
		if err := s.archiveAgents(ctx, live); err != nil {
			return err
		}
	}

	return s.tasks.Archive(context.WithoutCancel(ctx), found.ID)
}

// DeleteTasksParams is the tasks to delete, whether their records go too, and the agent asking.
type DeleteTasksParams struct {
	Refs []string
	// Records deletes the tasks for good, with every agent on them, rather than only their worktrees.
	Records bool
	// ByAgentID is the agent asking, empty for the panel.
	ByAgentID string
}

// DeleteTasks archives the tasks params names and removes their worktrees, or with records set
// deletes each task for good along with every agent on it.
func (s *LifecycleService) DeleteTasks(ctx context.Context, params DeleteTasksParams) error {
	if params.ByAgentID != "" {
		return agenterr.ErrDeleteByAgent
	}

	ids := make([]string, 0, len(params.Refs))
	for _, ref := range params.Refs {
		found, err := s.tasks.GetTask(ctx, ref)
		if err != nil {
			return err
		}
		ids = append(ids, found.ID)
	}
	ids = xslice.UniqueBy(ids, func(id string) string { return id })

	if err := s.tasks.RefuseParentDelete(ctx, ids); err != nil {
		return err
	}

	for _, id := range ids {
		if err := s.ArchiveTask(ctx, id); err != nil {
			return err
		}
	}

	if !params.Records {
		return s.tasks.RemoveWorktrees(ctx, ids)
	}

	if err := s.tasks.MarkDeleting(ctx, ids); err != nil {
		return err
	}

	if err := s.deleteAgentsOnTasks(ctx, ids); err != nil {
		return errors.Join(err, s.tasks.ClearDeleting(context.WithoutCancel(ctx), ids))
	}

	return errors.Join(xslice.Map(ids, func(id string) error {
		return s.tasks.DeleteIfEmpty(ctx, id)
	})...)
}

func (s *LifecycleService) deleteAgentsOnTasks(ctx context.Context, taskIDs []string) error {
	onTasks, err := s.agents.ListOnTasks(ctx, taskIDs)
	if err != nil {
		return err
	}

	if len(onTasks) == 0 {
		return nil
	}

	return s.agentDeleter.DeleteAgents(ctx, agentstore.IDs(onTasks))
}

// RestoreAgents returns archived agents and their workflows to live listings, a writer bringing
// its task back with it and a reviewer waiting until its task is back.
func (s *LifecycleService) RestoreAgents(ctx context.Context, ids []string) ([]agentstore.Agent, error) {
	agents, err := s.requireEveryAgent(ctx, ids)
	if err != nil {
		return nil, err
	}

	if err := s.refuseDeletedWorktrees(ctx, agents); err != nil {
		return nil, err
	}
	if err := s.requireTasksLive(ctx, agents); err != nil {
		return nil, err
	}

	archived := xslice.Filter(agents, func(agent agentstore.Agent) bool { return agent.ArchivedAt != nil })
	if len(archived) == 0 {
		return agents, nil
	}

	if err := restoreBatch(ctx, restoreBatchParams{
		tx:        s.tx,
		usage:     s.usage,
		scripts:   s.scripts,
		workflows: s.workflows,
		agents:    s.agents,
		tasks:     s.tasks,
		archived:  archived,
		restore: func(ctx context.Context, db store.DBTX) error {
			if err := agentstore.NewAgentStore(db).Restore(ctx, agentstore.IDs(archived)); err != nil {
				return err
			}
			workflowStore := workflowstore.NewWorkflowStore(db)
			for _, id := range workflowIDs(archived) {
				if err := workflowStore.Restore(ctx, id); err != nil {
					return err
				}
			}

			return nil
		},
	}); err != nil {
		return nil, fmt.Errorf("failed to restore %d agents: %w", len(archived), err)
	}

	return s.agents.ListByID(ctx, agentstore.IDs(agents))
}

func (s *LifecycleService) archiveWorkflow(ctx context.Context, workflowID string, live []agentstore.Agent) error {
	return archiveBatch(ctx, archiveBatchParams{
		tmux:      s.tmux,
		scripts:   s.scripts,
		workflows: s.workflows,
		tasks:     s.tasks,
		live:      live,
		owned:     live,
		screens:   s.screens,
		archive: func(ctx context.Context) (*archiveTaken, error) {
			taken := &archiveTaken{}
			err := s.tx.InTx(ctx, func(db store.DBTX) error {
				if err := workflowstore.NewWorkflowStore(db).Archive(ctx, workflowID); err != nil {
					return err
				}

				panes, err := agentstore.NewAgentStore(db).ArchiveWorkflowAgents(ctx, workflowID)
				if err != nil {
					return err
				}
				taken.panes = panes

				taken.tasks, err = taskstore.NewTaskStore(db).ArchiveIdle(ctx, agentstore.TaskIDs(live))

				return err
			})

			return taken, err
		},
	})
}

func (s *LifecycleService) restoreWorkflow(ctx context.Context, workflowID string, archived []agentstore.Agent) error {
	return restoreBatch(ctx, restoreBatchParams{
		tx:        s.tx,
		usage:     s.usage,
		scripts:   s.scripts,
		workflows: s.workflows,
		agents:    s.agents,
		tasks:     s.tasks,
		archived:  archived,
		relist: func(ctx context.Context, agents *agentstore.AgentStore) ([]agentstore.Agent, error) {
			return agents.ListRestorableInWorkflow(ctx, workflowID)
		},
		restore: func(ctx context.Context, db store.DBTX) error {
			if err := workflowstore.NewWorkflowStore(db).Restore(ctx, workflowID); err != nil {
				return err
			}

			return agentstore.NewAgentStore(db).RestoreWorkflowAgents(ctx, workflowID)
		},
	})
}

func (s *LifecycleService) removeWorkflowWorktrees(ctx context.Context, workflowID string) error {
	return s.tasks.RemoveWorkflowWorktrees(ctx, workflowID)
}

// requireTasksLive refuses a reviewer coming back while its task stays archived, letting one
// through whose writer comes back alongside it.
func (s *LifecycleService) requireTasksLive(ctx context.Context, agents []agentstore.Agent) error {
	returning := make(map[string]bool, len(agents))
	for _, agent := range agents {
		if isTaskWriter(agent) {
			returning[*agent.TaskID] = true
		}
	}

	for _, agent := range agents {
		if agent.TaskID == nil || isTaskWriter(agent) || agent.ArchivedAt == nil || returning[*agent.TaskID] {
			continue
		}

		task, err := s.tasks.GetTask(ctx, *agent.TaskID)
		if err != nil {
			return err
		}
		if task.ArchivedAt != nil {
			return agenterr.ErrTaskArchived.Saying("restore task %q to bring %q back", task.Slug, agent.Name)
		}
	}

	return nil
}

// refuseWriters refuses a call that names a task's writer on its own, saying what to do to its
// task instead.
func refuseWriters(agents []agentstore.Agent, verb string) error {
	for _, agent := range agents {
		if isTaskWriter(agent) {
			return agenterr.ErrWriterGoesWithTask.Saying("%s task %s, which %q writes for, instead", verb, *agent.TaskID, agent.Name)
		}
	}

	return nil
}

// refuseDeletedWorktrees refuses a restore that names an archived agent on a task whose worktree
// was deleted, since nothing is left for it to run in.
func (s *LifecycleService) refuseDeletedWorktrees(ctx context.Context, agents []agentstore.Agent) error {
	for _, agent := range agents {
		if agent.TaskID == nil || agent.ArchivedAt == nil {
			continue
		}

		task, err := s.tasks.GetTask(ctx, *agent.TaskID)
		if err != nil {
			return err
		}
		if task.Checkout.WorktreeRemovalReason == taskstore.WorktreeDeleted {
			return tasks.ErrWorktreeRemoved.Saying("task %q's worktree was deleted, so %q cannot be restored", task.Slug, agent.Name)
		}
	}

	return nil
}

func (s *LifecycleService) requireEveryAgent(ctx context.Context, ids []string) ([]agentstore.Agent, error) {
	agents, err := s.agents.ListByID(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to look up %d agents: %w", len(ids), err)
	}

	return requireListed(ids, agents)
}

func requireListed(ids []string, agents []agentstore.Agent) ([]agentstore.Agent, error) {
	if len(agents) == len(ids) {
		return agents, nil
	}

	found := xslice.KeyBy(agents, func(agent agentstore.Agent) string { return agent.ID })

	missing := xslice.Filter(ids, func(id string) bool {
		_, ok := found[id]

		return !ok
	})

	return nil, agenterr.ErrAgentNotFound.Saying("no agent with id %s", xstr.JoinSome(missing, namedMissing))
}

// refuseArchivingOthers holds an agent other than an orchestrator to itself and, as a writer, the
// reviewers on its task.
func (s *LifecycleService) refuseArchivingOthers(ctx context.Context, params ArchiveAgentsParams, listed []agentstore.Agent) error {
	if params.ByAgentID == "" {
		return nil
	}

	asker, err := s.agents.GetByID(ctx, params.ByAgentID)
	if err != nil {
		return fmt.Errorf("failed to get the agent archiving: %w", err)
	}
	if asker.Role == agentstore.RoleOrchestrator {
		return nil
	}

	found := xslice.KeyBy(listed, func(agent agentstore.Agent) string { return agent.ID })
	for _, id := range params.IDs {
		target, ok := found[id]
		if id == asker.ID || !ok {
			continue
		}
		if !isTaskWriter(*asker) || !isReviewerOf(target, *asker) {
			return errArchiveNotYours.Saying("%s is not a reviewer on the task %s writes for", target.Name, asker.Name)
		}
	}

	return nil
}

func (s *LifecycleService) removeAgents(ctx context.Context, ids []string) error {
	// The drafts these agents were writing about other agents go before the rows naming them do,
	// and outside the delete's own transaction.
	s.dropOpenReviews(ctx, ids)

	// The delete says which pane each row really held, so only it reaches a pane written after
	// the caller read the rows.
	var taken []agentstore.AgentPane
	if err := s.tx.InTx(ctx, func(db store.DBTX) error {
		deleted, err := agentstore.NewAgentStore(db).Delete(ctx, ids)
		if err != nil {
			return err
		}
		taken = deleted

		return transcriptstore.NewTranscriptStore(db).Delete(ctx, ids)
	}); err != nil {
		return err
	}

	killPanes(ctx, s.tmux, taken)
	s.removeRunLogs(ctx, ids)
	s.removeFrames(ctx, ids)

	return nil
}

// dropOpenReviews removes the unreleased reviews these agents were writing on other agents'
// work.
func (s *LifecycleService) dropOpenReviews(ctx context.Context, ids []string) {
	if s.reviews == nil {
		return
	}

	if err := s.reviews.DeleteOpenReviewsBy(ctx, ids); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to drop the open reviews of agents being deleted",
			logging.Err(err)...)
	}
}

// removeFrames drops the pane captures a recording kept for an agent once its row is gone.
func (s *LifecycleService) removeFrames(ctx context.Context, ids []string) {
	if s.framesDir == "" {
		return
	}

	for _, id := range ids {
		path := frames.AgentDir(s.framesDir, id)
		if err := os.RemoveAll(path); err != nil {
			logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to remove an agent's recorded frames",
				append([]slog.Attr{
					slog.String(logging.FilePath, path),
					slog.String(logging.AgentID, id),
				}, logging.Err(err)...)...)
		}
	}
}

// removeRunLogs drops the runs an agent leaves on disk once its row is gone, logging a failure
// rather than returning it.
func (s *LifecycleService) removeRunLogs(ctx context.Context, ids []string) {
	if s.logsDir == "" {
		return
	}

	for _, id := range ids {
		path := config.RunLogDir(s.logsDir, id)
		if err := os.RemoveAll(path); err != nil {
			logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to remove an agent's run logs",
				append([]slog.Attr{
					slog.String(logging.FilePath, path),
					slog.String(logging.AgentID, id),
				}, logging.Err(err)...)...)
		}
	}
}

type failAsParams struct {
	agents *agentstore.AgentStore
	agent  agentstore.Agent
	state  agentstore.State
	verb   string
	cause  error
}

// failAs marks an agent as having failed create or cleanup and leaves the row where it was.
func failAs(ctx context.Context, params failAsParams) {
	agent := params.agent
	agent.State = params.state
	if err := params.agents.Update(ctx, agent); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to record why an agent's lifecycle action failed",
			append([]slog.Attr{
				slog.String(logging.AgentID, agent.ID),
				slog.String(logging.LifecycleAction, params.verb),
			}, logging.Err(err)...)...)

		return
	}

	reason := store.FailureReason(params.cause)
	if _, err := params.agents.SetFailureReason(ctx, agent.ID, reason); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to record why an agent's lifecycle action failed",
			append([]slog.Attr{
				slog.String(logging.AgentID, agent.ID),
				slog.String(logging.LifecycleAction, params.verb),
			}, logging.Err(err)...)...)
	}
}

const namedMissing = 5

func workflowIDs(agents []agentstore.Agent) []string {
	unique := xslice.UniqueBy(agents, func(agent agentstore.Agent) string { return agent.WorkflowID })

	return xslice.Map(unique, func(agent agentstore.Agent) string { return agent.WorkflowID })
}
