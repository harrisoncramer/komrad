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
	"komrad/internal/frames"
	"komrad/internal/logging"
	"komrad/internal/scripts"
	"komrad/internal/store"
	"komrad/lib/xslice"
	"komrad/lib/xstr"

	agentstore "komrad/internal/store/agent"
	reviewstore "komrad/internal/store/review"
	transcriptstore "komrad/internal/store/transcript"
	workflowstore "komrad/internal/store/workflow"
	worktreestore "komrad/internal/store/worktree"
)

// childLister reads the agents attached to a set of parents.
type childLister interface {
	ListChildren(ctx context.Context, parentIDs []string) ([]agentstore.Agent, error)
}

// lifecycleAgentReadWriter is the slice of the agent store archiving, restoring and deleting
// write to outside a transaction.
type lifecycleAgentReadWriter interface {
	agentFailWriter
	childLister
	ListByID(ctx context.Context, ids []string) ([]agentstore.Agent, error)
	Archive(ctx context.Context, ids []string) ([]agentstore.AgentPane, error)
	ClearDeleting(ctx context.Context, ids []string) error
	MarkDeleting(ctx context.Context, ids []string) ([]agentstore.Claim, error)
}

// emptyWorkflowDeleter permanently removes a workflow left with nothing archived inside it.
type emptyWorkflowDeleter interface {
	DeleteIfEmpty(ctx context.Context, workflowID string) error
}

// reviewReaper is the half of the review store the lifecycle needs, being the refs an agent's
// snapshots are held by and the drafts it was writing about others.
type reviewReaper interface {
	ListRounds(ctx context.Context, agentID string) ([]reviewstore.Round, error)
	DeleteOpenReviewsBy(ctx context.Context, authorAgentIDs []string) error
}

// snapshotReleaser drops the ref holding a round's snapshot.
type snapshotReleaser interface {
	ReleaseRef(ctx context.Context, dir, ref string) error
}

type NewLifecycleServiceParams struct {
	Tmux           paneKiller
	Agents         lifecycleAgentReadWriter
	Workflows      workflowReader
	EmptyWorkflows emptyWorkflowDeleter
	Tx             txRunner
	Scripts        agentScriptRunner
	Reviews        reviewReaper
	Snapshots      snapshotReleaser
	LogsDir        string
	FramesDir      string
	Usage          config.Usage
}

// LifecycleService moves agents between the live listing, the archive and gone, running the
// deprovisioner on the way out.
type LifecycleService struct {
	tmux           paneKiller
	agents         lifecycleAgentReadWriter
	workflows      workflowReader
	emptyWorkflows emptyWorkflowDeleter
	tx             txRunner
	scripts        agentScriptRunner
	reviews        reviewReaper
	snapshots      snapshotReleaser
	logsDir        string
	framesDir      string
	usage          config.Usage
}

// NewLifecycleService builds the service from its dependencies.
func NewLifecycleService(params NewLifecycleServiceParams) *LifecycleService {
	return &LifecycleService{
		tmux:           params.Tmux,
		agents:         params.Agents,
		workflows:      params.Workflows,
		emptyWorkflows: params.EmptyWorkflows,
		tx:             params.Tx,
		scripts:        params.Scripts,
		reviews:        params.Reviews,
		snapshots:      params.Snapshots,
		logsDir:        params.LogsDir,
		framesDir:      params.FramesDir,
		usage:          params.Usage,
	}
}

// DeleteAgent deletes an agent and the children the same claim took, and does nothing to an
// agent that claim no longer holds.
func (s *LifecycleService) DeleteAgent(ctx context.Context, id string, claim time.Time) error {
	named, err := s.claimedBy(ctx, id, claim)
	if err != nil || len(named) == 0 {
		return err
	}

	children, err := s.agents.ListChildren(ctx, []string{id})
	if err != nil {
		return err
	}

	agents := named
	for _, child := range children {
		if child.DeletingAt != nil && !child.DeletingAt.Equal(claim) {
			return ErrChildDeleting.Saying("agent %s waits on the delete of its child %s", id, child.ID)
		}

		agents = append(agents, child)
	}

	return s.deleteAgents(ctx, agents)
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
	// A deprovisioner must not run against a directory a CLI is still working in.
	killOwnedPanes(ctx, s.tmux, agents)

	hooks := make(map[string][]workflowstore.Hook, len(agents))
	var torn []agentstore.Agent
	var failed []error
	for _, agent := range slices.Backward(agents) {
		hooks[agent.ID] = agentHooks(ctx, s.workflows, agent)
		if err := s.tearDown(ctx, agent, hooks[agent.ID]); err != nil {
			s.failCleanup(context.WithoutCancel(ctx), agent, err)
			failed = append(failed, fmt.Errorf("%w: agent %s: %w", ErrTeardownFailed, agent.ID, err))

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

	for _, agent := range torn {
		startHooks(ctx, hookRunParams{
			scripts: s.scripts,
			agent:   agent,
			hooks:   hooks[agent.ID],
			event:   workflowstore.HookPostDelete,
		})
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

// tearDown waits out an agent's hooks still running, runs its pre-delete hooks while its
// directory is still there, then its deprovisioner, and waits for both.
func (s *LifecycleService) tearDown(ctx context.Context, agent agentstore.Agent, hooks []workflowstore.Hook) error {
	if s.scripts == nil {
		return nil
	}

	if err := s.scripts.AwaitHooks(ctx, agent.ID); err != nil {
		return err
	}

	// A hook has nothing to run against once the directory has gone.
	if scripts.RequireUsableDir(agent.Cwd) == nil {
		if err := runPreHooks(ctx, hookRunParams{
			scripts: s.scripts,
			agent:   agent,
			hooks:   hooks,
			event:   workflowstore.HookPreDelete,
		}); err != nil {
			return err
		}
	}

	name := deprovisionerFor(ctx, s.workflows, agent)
	if name == "" {
		return nil
	}

	done := make(chan error, 1)
	runDeprovision(ctx, s.scripts, agent, name, func(err error) { done <- err })

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *LifecycleService) failCleanup(ctx context.Context, agent agentstore.Agent, cause error) {
	failAs(ctx, s.agents, agent, agentstore.StateFailedToCleanup, "cleanup", cause)

	if err := s.agents.ClearDeleting(ctx, []string{agent.ID}); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to release an agent whose cleanup failed",
			append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)
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

// ArchiveAgents stops agents while preserving their records and workspaces.
func (s *LifecycleService) ArchiveAgents(ctx context.Context, ids []string) error {
	named, err := s.requireEveryAgent(ctx, ids)
	if err != nil {
		return err
	}

	agents, err := s.withChildren(ctx, named)
	if err != nil {
		return err
	}

	if err := archiveBatch(ctx, archiveBatchParams{
		tmux:      s.tmux,
		scripts:   s.scripts,
		workflows: s.workflows,
		live:      xslice.Filter(agents, func(agent agentstore.Agent) bool { return agent.ArchivedAt == nil }),
		owned:     agents,
		archive: func(ctx context.Context) ([]agentstore.AgentPane, error) {
			return s.agents.Archive(ctx, agentstore.IDs(agents))
		},
	}); err != nil {
		return fmt.Errorf("failed to archive %d agents: %w", len(agents), err)
	}

	return nil
}

// RestoreAgents returns archived agents and their workflows to live listings.
func (s *LifecycleService) RestoreAgents(ctx context.Context, ids []string) ([]agentstore.Agent, error) {
	named, err := s.requireEveryAgent(ctx, ids)
	if err != nil {
		return nil, err
	}

	whole, err := s.withChildren(ctx, named)
	if err != nil {
		return nil, err
	}
	agents := archivedTogether(named, whole)

	if err := s.requireParentsPresent(ctx, agents); err != nil {
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

// archivedTogether keeps the agents a call named and the children that went into the archive
// with their parent.
func archivedTogether(named, whole []agentstore.Agent) []agentstore.Agent {
	batches := make(map[string]*string, len(named))
	for _, agent := range named {
		batches[agent.ID] = agent.ArchiveBatchID
	}

	return xslice.Filter(whole, func(agent agentstore.Agent) bool {
		if _, ok := batches[agent.ID]; ok {
			return true
		}
		if agent.ArchivedAt == nil || agent.ParentAgentID == nil {
			return true
		}
		batch := batches[*agent.ParentAgentID]

		return batch != nil && agent.ArchiveBatchID != nil && *batch == *agent.ArchiveBatchID
	})
}

// requireParentsPresent refuses a child coming back while the agent it hangs under is still
// archived.
func (s *LifecycleService) requireParentsPresent(ctx context.Context, agents []agentstore.Agent) error {
	held := make(map[string]bool, len(agents))
	for _, agent := range agents {
		held[agent.ID] = true
	}

	var wanted []string
	for _, agent := range agents {
		if agent.ParentAgentID != nil && !held[*agent.ParentAgentID] {
			wanted = append(wanted, *agent.ParentAgentID)
		}
	}
	if len(wanted) == 0 {
		return nil
	}

	parents, err := s.agents.ListByID(ctx, wanted)
	if err != nil {
		return fmt.Errorf("failed to look up the parents of %d children: %w", len(wanted), err)
	}

	live := make(map[string]bool, len(parents))
	names := make(map[string]string, len(parents))
	for _, parent := range parents {
		live[parent.ID] = parent.ArchivedAt == nil
		names[parent.ID] = parent.Name
	}

	for _, agent := range agents {
		if agent.ParentAgentID == nil || held[*agent.ParentAgentID] {
			continue
		}
		if !live[*agent.ParentAgentID] {
			return fmt.Errorf("%w: restore %s to bring %q back with it",
				ErrParentStillArchived, describeParent(names[*agent.ParentAgentID]), agent.Name)
		}
	}

	return nil
}

// describeParent names the agent a child hangs under, falling back to a phrase for a parent no
// longer in the listing.
func describeParent(name string) string {
	if name == "" {
		return "the agent it is attached to"
	}

	return fmt.Sprintf("%q", name)
}

// withChildren adds the children of the agents a call named to the list it acts on, leaving a
// child named on its own alone.
func (s *LifecycleService) withChildren(ctx context.Context, named []agentstore.Agent) ([]agentstore.Agent, error) {
	held := make(map[string]bool, len(named))
	for _, agent := range named {
		held[agent.ID] = true
	}

	var parents []string
	for _, agent := range named {
		if agent.ParentAgentID == nil {
			parents = append(parents, agent.ID)
		}
	}

	children, err := s.agents.ListChildren(ctx, parents)
	if err != nil {
		return nil, err
	}

	whole := make([]agentstore.Agent, 0, len(named)+len(children))
	whole = append(whole, named...)
	for _, child := range children {
		if held[child.ID] {
			continue
		}

		whole = append(whole, child)
	}

	return whole, nil
}

func (s *LifecycleService) requireEveryAgent(ctx context.Context, ids []string) ([]agentstore.Agent, error) {
	agents, err := s.agents.ListByID(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to look up %d agents: %w", len(ids), err)
	}
	if len(agents) == len(ids) {
		return agents, nil
	}

	found := xslice.KeyBy(agents, func(agent agentstore.Agent) string { return agent.ID })

	missing := xslice.Filter(ids, func(id string) bool {
		_, ok := found[id]

		return !ok
	})

	return nil, ErrAgentNotFound.Saying("no agent with id %s", xstr.JoinSome(missing, namedMissing))
}

func (s *LifecycleService) removeAgents(ctx context.Context, ids []string) error {
	// The refs holding these agents' snapshots and the drafts they were writing about other
	// agents both go before the rows naming them do, and both outside the delete's own
	// transaction.
	s.releaseRounds(ctx, ids)
	s.dropOpenReviews(ctx, ids)

	// The delete is what says which pane each row really held, so a pane written after the
	// caller read the rows is reached here and nowhere else.
	var taken []agentstore.AgentPane
	if err := s.tx.InTx(ctx, func(db store.DBTX) error {
		deleted, err := agentstore.NewAgentStore(db).Delete(ctx, ids)
		if err != nil {
			return err
		}
		taken = deleted

		if err := transcriptstore.NewTranscriptStore(db).Delete(ctx, ids); err != nil {
			return err
		}

		return worktreestore.NewWorktreeStore(db).DeleteByName(ctx, ids)
	}); err != nil {
		return err
	}

	killPanes(ctx, s.tmux, taken)
	s.removeRunLogs(ctx, ids)
	s.removeFrames(ctx, ids)

	return nil
}

// releaseRounds drops the refs holding an agent's snapshots, which would otherwise pin a commit
// covering its whole worktree long after the agent is gone.
func (s *LifecycleService) releaseRounds(ctx context.Context, ids []string) {
	if s.reviews == nil {
		return
	}

	going, err := s.agents.ListByID(ctx, ids)
	if err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to read the agents being deleted to release their rounds",
			logging.Err(err)...)

		return
	}

	for _, agent := range going {
		dir := s.findRoundRefDir(ctx, agent)
		if dir == "" {
			continue
		}

		rounds, err := s.reviews.ListRounds(ctx, agent.ID)
		if err != nil {
			logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to read the rounds of an agent being deleted",
				append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)

			continue
		}

		for _, round := range rounds {
			if err := s.snapshots.ReleaseRef(ctx, dir, reviewstore.RoundRef(agent.ID, round.SnapshotSHA)); err != nil {
				logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to release the snapshot of a round",
					append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)
			}
		}
	}
}

// findRoundRefDir is a checkout of the repository holding an agent's round refs: its own while it
// stands, else its workflow's directory, which shares the refs of every worktree cut from it.
func (s *LifecycleService) findRoundRefDir(ctx context.Context, agent agentstore.Agent) string {
	if agent.Cwd != "" && !agent.WorktreeRemoved {
		return agent.Cwd
	}

	flow, err := s.workflows.GetIncludingArchived(ctx, agent.WorkflowID)
	if err != nil {
		return ""
	}

	return flow.Cwd
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

// failAs marks an agent as having failed create or cleanup and leaves the row where it was.
func failAs(ctx context.Context, agents agentFailWriter, agent agentstore.Agent, state agentstore.State, verb string, cause error) { //nolint:revive // a params struct is the follow-up
	agent.State = state
	if err := agents.Update(ctx, agent); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to record why an agent's lifecycle action failed",
			append([]slog.Attr{
				slog.String(logging.AgentID, agent.ID),
				slog.String(logging.LifecycleAction, verb),
			}, logging.Err(err)...)...)

		return
	}

	reason := store.FailureReason(cause)
	if _, err := agents.SetFailureReason(ctx, agent.ID, reason); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to record why an agent's lifecycle action failed",
			append([]slog.Attr{
				slog.String(logging.AgentID, agent.ID),
				slog.String(logging.LifecycleAction, verb),
			}, logging.Err(err)...)...)
	}
}

const namedMissing = 5

func workflowIDs(agents []agentstore.Agent) []string {
	unique := xslice.UniqueBy(agents, func(agent agentstore.Agent) string { return agent.WorkflowID })

	return xslice.Map(unique, func(agent agentstore.Agent) string { return agent.WorkflowID })
}
