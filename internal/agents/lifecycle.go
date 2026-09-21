package agents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"weave/config"
	"weave/internal/frames"
	"weave/internal/logging"
	"weave/internal/scripts"
	"weave/internal/store"
	"weave/lib/xslice"
	"weave/lib/xstr"

	agentstore "weave/internal/store/agent"
	reviewstore "weave/internal/store/review"
	transcriptstore "weave/internal/store/transcript"
	workflowstore "weave/internal/store/workflow"
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
	ArchiveIfUnchanged(ctx context.Context, params agentstore.ArchiveIfUnchangedParams) (bool, error)
	RestampArchived(ctx context.Context, ids []string) error
	MarkDeleting(ctx context.Context, ids []string) error
	ClearDeleting(ctx context.Context, ids []string) error
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
	maxAgents      int
	maxChildren    int
	tearingDownMu  sync.Mutex
	tearingDown    map[string]struct{}
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
	MaxAgents      int
	MaxChildren    int
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
		maxAgents:      params.MaxAgents,
		maxChildren:    params.MaxChildren,
		tearingDown:    make(map[string]struct{}),
	}
}

// DeleteAgents stops agents and removes those with nothing left to tear down, holding back any
// whose deprovisioner is still running until it finishes.
func (s *LifecycleService) DeleteAgents(ctx context.Context, ids []string) error {
	named, err := s.requireEveryAgent(ctx, ids)
	if err != nil {
		return errors.Join(err, s.agents.ClearDeleting(ctx, ids))
	}

	agents, err := s.withChildren(ctx, named)
	if err != nil {
		return errors.Join(err, s.agents.ClearDeleting(ctx, ids))
	}

	if err := s.agents.MarkDeleting(ctx, agentstore.IDs(agents)); err != nil {
		return errors.Join(err, s.agents.ClearDeleting(ctx, ids))
	}

	// A deprovisioner must not run against a directory a CLI is still working in, and since the
	// children share their parent's, this is what keeps the parent's deprovision from tearing down a
	// checkout one of them is still sitting in.
	killOwnedPanes(ctx, s.tmux, agents)

	bg := context.WithoutCancel(ctx)

	var immediate []agentstore.Agent
	for _, agent := range agents {
		name := deprovisionerFor(ctx, s.workflows, agent)
		hooks := agentHooks(ctx, s.workflows, agent)
		teardown := s.scripts != nil && hasHookOn(hooks, workflowstore.HookPreTeardown)
		if s.scripts == nil || (name == "" && !teardown) {
			immediate = append(immediate, agent)

			continue
		}

		// The row is never dropped before the script says whether it worked.
		done := s.finishDelete(bg, agent)
		if !teardown {
			runDeprovision(ctx, s.scripts, agent, name, done)

			continue
		}

		// The deprovisioner claims the agent so it cannot run twice at once, and a hook does not.
		if !s.claimTeardown(agent.ID) {
			continue
		}

		go s.tearDown(bg, agent, hooks, name, done)
	}

	if len(immediate) == 0 {
		return nil
	}

	if err := s.removeAgents(ctx, agentstore.IDs(immediate)); err != nil {
		return errors.Join(err, s.agents.ClearDeleting(ctx, agentstore.IDs(immediate)))
	}

	s.cleanupEmptyWorkflows(ctx, immediate)

	return nil
}

// tearDown runs a workflow's teardown hooks while the agent's directory is still there, then
// lets the deprovisioner take it away.
func (s *LifecycleService) tearDown(ctx context.Context, agent agentstore.Agent, hooks []workflowstore.Hook, deprovisioner string, done func(error)) { //nolint:revive // a params struct is the follow-up
	// A hook has nothing to run against once the directory has gone.
	if scripts.RequireUsableDir(agent.Cwd) == nil {
		// The deprovisioner waits until the teardown has worked.
		if err := runHooks(ctx, hookRunParams{
			scripts: s.scripts,
			agent:   agent,
			hooks:   hooks,
			event:   workflowstore.HookPreTeardown,
		}); err != nil {
			s.releaseTeardown(agent.ID)
			s.failCleanup(ctx, agent, err)

			return
		}
	}

	runDeprovision(ctx, s.scripts, agent, deprovisioner, func(deprovisionErr error) {
		s.releaseTeardown(agent.ID)
		done(deprovisionErr)
	})
}

// claimTeardown reports whether this caller is the one tearing an agent down, so a second
// delete does nothing instead of running the hooks again.
func (s *LifecycleService) claimTeardown(id string) bool {
	s.tearingDownMu.Lock()
	defer s.tearingDownMu.Unlock()

	if _, running := s.tearingDown[id]; running {
		return false
	}

	s.tearingDown[id] = struct{}{}

	return true
}

func (s *LifecycleService) releaseTeardown(id string) {
	s.tearingDownMu.Lock()
	defer s.tearingDownMu.Unlock()

	delete(s.tearingDown, id)
}

// finishDelete drops the row of an agent whose teardown worked, and writes down the reason on
// one whose teardown did not.
func (s *LifecycleService) finishDelete(ctx context.Context, agent agentstore.Agent) func(error) {
	return func(deprovisionErr error) {
		if deprovisionErr != nil {
			s.failCleanup(ctx, agent, deprovisionErr)

			return
		}

		if err := s.removeAgents(ctx, []string{agent.ID}); err != nil {
			logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to remove an agent whose cleanup finished",
				append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)
			s.failCleanup(ctx, agent, err)

			return
		}

		s.cleanupEmptyWorkflows(ctx, []agentstore.Agent{agent})
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

	// Killing what was read first keeps a failed write from hiding a live CLI, leaving the row
	// for the reconciler to tidy.
	killOwnedPanes(ctx, s.tmux, agents)

	taken, err := s.agents.Archive(ctx, agentstore.IDs(agents))
	if err != nil {
		return fmt.Errorf("failed to archive %d agents: %w", len(agents), err)
	}
	// A pane recorded in between only reaches here through what the write took off the rows.
	killPanes(ctx, s.tmux, taken)

	return nil
}

// ArchiveUnchangedAgents archives the agents whose rows still read as the caller observed them
// and returns the ones it archived.
func (s *LifecycleService) ArchiveUnchangedAgents(ctx context.Context, observed []agentstore.Agent) ([]agentstore.Agent, error) {
	var failures []error
	var archived []agentstore.Agent

	for _, agent := range observed {
		applied, err := s.agents.ArchiveIfUnchanged(ctx, agentstore.ArchiveIfUnchangedParams{ID: agent.ID, UpdatedAt: agent.UpdatedAt})
		if err != nil {
			failures = append(failures, err)

			continue
		}
		if applied {
			archived = append(archived, agent)
		}
	}

	children, err := s.archiveChildrenOf(ctx, archived)
	if err != nil {
		failures = append(failures, err)
	}
	archived = append(archived, children...)

	killOwnedPanes(ctx, s.tmux, archived)

	// The stamp stands for a CLI that is already dead.
	if err := s.agents.RestampArchived(ctx, agentstore.IDs(archived)); err != nil {
		failures = append(failures, err)
	}

	return archived, errors.Join(failures...)
}

// archiveChildrenOf takes the children of the agents a sweep archived, unconditionally where
// the parents were not.
func (s *LifecycleService) archiveChildrenOf(ctx context.Context, parents []agentstore.Agent) ([]agentstore.Agent, error) {
	if len(parents) == 0 {
		return nil, nil
	}

	children, err := s.agents.ListChildren(ctx, agentstore.IDs(parents))
	if err != nil {
		return nil, err
	}

	live := xslice.Filter(children, func(child agentstore.Agent) bool { return child.ArchivedAt == nil })
	if len(live) == 0 {
		return nil, nil
	}

	taken, err := s.agents.Archive(ctx, agentstore.IDs(live))
	if err != nil {
		return nil, fmt.Errorf("failed to archive the %d children of %d archived agents: %w", len(live), len(parents), err)
	}
	// A pane a child was resumed into between the read and the write only reaches here through
	// what the write took off the rows.
	killPanes(ctx, s.tmux, taken)

	return live, nil
}

// RestoreAgents returns archived agents and their workflows to live listings.
func (s *LifecycleService) RestoreAgents(ctx context.Context, ids []string) ([]agentstore.Agent, error) {
	named, err := s.requireEveryAgent(ctx, ids)
	if err != nil {
		return nil, err
	}

	agents, err := s.withChildren(ctx, named)
	if err != nil {
		return nil, err
	}

	if err := s.requireParentsPresent(ctx, agents); err != nil {
		return nil, err
	}

	archived := xslice.Filter(agents, func(agent agentstore.Agent) bool { return agent.ArchivedAt != nil })
	if len(archived) == 0 {
		return agents, nil
	}

	err = s.tx.InTx(ctx, func(db store.DBTX) error {
		agentStore, workflowStore := agentstore.NewAgentStore(db), workflowstore.NewWorkflowStore(db)

		if err := claimRoomForAgents(ctx, agentStore, s.maxAgents, len(archived)); err != nil {
			return err
		}
		if err := claimRoomForRestoredChildren(ctx, agentStore, s.maxChildren, archived); err != nil {
			return err
		}
		if err := requireNamesFree(ctx, agentStore, archived); err != nil {
			return err
		}
		if err := agentStore.Restore(ctx, agentstore.IDs(archived)); err != nil {
			return err
		}
		for _, id := range workflowIDs(archived) {
			if err := workflowStore.Restore(ctx, id); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to restore %d agents: %w", len(archived), err)
	}

	restored, err := s.agents.ListByID(ctx, agentstore.IDs(agents))
	if err != nil {
		return nil, err
	}

	// The read-back is every agent the caller named, and only the ones this call took out of the
	// archive have been restored.
	wasArchived := make(map[string]bool, len(archived))
	for _, agent := range archived {
		wasArchived[agent.ID] = true
	}
	startRestoreHooks(ctx, s.scripts, s.workflows, xslice.Filter(restored, func(agent agentstore.Agent) bool {
		return wasArchived[agent.ID]
	}))

	return restored, nil
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

		return transcriptstore.NewTranscriptStore(db).Delete(ctx, ids)
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
		if agent.Cwd == "" || agent.WorktreeRemoved {
			continue
		}

		rounds, err := s.reviews.ListRounds(ctx, agent.ID)
		if err != nil {
			logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to read the rounds of an agent being deleted",
				append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)

			continue
		}

		for _, round := range rounds {
			if err := s.snapshots.ReleaseRef(ctx, agent.Cwd, reviewstore.RoundRef(agent.ID, round.SnapshotSHA)); err != nil {
				logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to release the snapshot of a round",
					append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)
			}
		}
	}
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
	if _, err := agents.SetMetadata(ctx, agentstore.SetMetadataParams{
		ID:    agent.ID,
		Key:   "error",
		Value: &reason,
	}); err != nil {
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
