package agents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"weave/internal/diff"

	"weave/config"
	"weave/internal/frames"
	"weave/internal/logging"
	"weave/internal/store"
	"weave/lib/xslice"

	agentstore "weave/internal/store/agent"
	transcriptstore "weave/internal/store/transcript"
	workflowstore "weave/internal/store/workflow"
)

// lifecycleAgentReadWriter is the slice of the agent store archiving, restoring and deleting write
// to outside a transaction; the writes that have to be atomic go through the tx-scoped store.
type lifecycleAgentReadWriter interface {
	agentFailWriter
	childLister
	ListByID(ctx context.Context, ids []string) ([]agentstore.Agent, error)
	Archive(ctx context.Context, ids []string) ([]agentstore.AgentPane, error)
	ArchiveIfUnchanged(ctx context.Context, params agentstore.ArchiveIfUnchangedParams) (bool, error)
}

// emptyWorkflowDeleter permanently removes a workflow left with nothing archived inside it.
type emptyWorkflowDeleter interface {
	DeleteIfEmpty(ctx context.Context, workflowID string) error
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
	notifier       systemMailSender
	rounds         reviewRoundReaper
	logsDir        string
	framesDir      string
	maxAgents      int
	maxChildren    int
}

type LifecycleServiceParams struct {
	Tmux           paneKiller
	Agents         lifecycleAgentReadWriter
	Workflows      workflowReader
	EmptyWorkflows emptyWorkflowDeleter
	Tx             txRunner
	Scripts        agentScriptRunner
	Notifier       systemMailSender
	Rounds         reviewRoundReaper
	LogsDir        string
	FramesDir      string
	MaxAgents      int
	MaxChildren    int
}

// NewLifecycleService builds the service from its dependencies.
func NewLifecycleService(p LifecycleServiceParams) *LifecycleService {
	return &LifecycleService{
		tmux:           p.Tmux,
		agents:         p.Agents,
		workflows:      p.Workflows,
		emptyWorkflows: p.EmptyWorkflows,
		tx:             p.Tx,
		scripts:        p.Scripts,
		notifier:       p.Notifier,
		rounds:         p.Rounds,
		logsDir:        p.LogsDir,
		framesDir:      p.FramesDir,
		maxAgents:      p.MaxAgents,
		maxChildren:    p.MaxChildren,
	}
}

// DeleteAgents stops agents and removes those with nothing left to tear down, holding back any
// whose deprovisioner is still running until it finishes.
func (s *LifecycleService) DeleteAgents(ctx context.Context, ids []string) error {
	named, err := s.requireEveryAgent(ctx, ids)
	if err != nil {
		return err
	}

	agents, err := s.withChildren(ctx, named)
	if err != nil {
		return err
	}

	// A deprovisioner must not run against a directory a CLI is still working in, and since the
	// children share their parent's, this is what keeps the parent's deprovision from tearing down a
	// checkout one of them is still sitting in.
	killOwnedPanes(ctx, s.tmux, agents)

	bg := context.WithoutCancel(ctx)

	var immediate []agentstore.Agent
	for _, agent := range agents {
		name := deprovisionerFor(ctx, s.workflows, agent)
		if s.scripts == nil || name == "" {
			immediate = append(immediate, agent)

			continue
		}

		// The row is never dropped before the script says whether it worked.
		runDeprovision(ctx, s.scripts, agent, name, func(deprovisionErr error) {
			if deprovisionErr != nil {
				s.failCleanup(bg, agent, deprovisionErr)

				return
			}

			if err := s.removeAgents(bg, []string{agent.ID}); err != nil {
				logging.From(bg).LogAttrs(bg, slog.LevelError, "failed to remove an agent whose cleanup finished",
					append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)

				return
			}

			s.cleanupEmptyWorkflows(bg, []agentstore.Agent{agent})
			notifyParents(bg, s.notifier, []agentstore.Agent{agent}, agents, "deleted")
		})
	}

	if len(immediate) == 0 {
		return nil
	}

	if err := s.removeAgents(ctx, agentIDs(immediate)); err != nil {
		return err
	}

	s.cleanupEmptyWorkflows(ctx, immediate)
	notifyParents(ctx, s.notifier, immediate, agents, "deleted")

	return nil
}

func (s *LifecycleService) failCleanup(ctx context.Context, agent agentstore.Agent, cause error) {
	failAs(ctx, s.agents, agent, agentstore.StateFailedToCleanup, "cleanup", cause)
}

// cleanupEmptyWorkflows deletes each workflow among removed's that now holds nothing archived
// inside it, logging a failure rather than returning it since the agents are already gone.
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

	taken, err := s.agents.Archive(ctx, agentIDs(agents))
	if err != nil {
		return fmt.Errorf("failed to archive %d agents: %w", len(agents), err)
	}
	// A pane recorded in between only reaches here through what the write took off the rows.
	killPanes(ctx, s.tmux, taken)

	notifyParents(ctx, s.notifier, agents, agents, "archived")

	return nil
}

// ArchiveUnchangedAgents archives the agents whose rows still read as the caller observed them
// and returns the ones it archived, so an agent resumed since the snapshot is skipped.
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
	notifyParents(ctx, s.notifier, archived, archived, "archived")

	return archived, errors.Join(failures...)
}

// archiveChildrenOf takes the children of the agents a sweep archived, unconditionally where
// the parents were not, since what decides a child is gone is the parent going rather than the
// state its own row was last read in.
func (s *LifecycleService) archiveChildrenOf(ctx context.Context, parents []agentstore.Agent) ([]agentstore.Agent, error) {
	if len(parents) == 0 {
		return nil, nil
	}

	children, err := s.agents.ListChildren(ctx, agentIDs(parents))
	if err != nil {
		return nil, err
	}

	live := xslice.Filter(children, func(child agentstore.Agent) bool { return child.ArchivedAt == nil })
	if len(live) == 0 {
		return nil, nil
	}

	taken, err := s.agents.Archive(ctx, agentIDs(live))
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
		agentStore, workflowStore := agentstore.New(db), workflowstore.New(db)

		if err := claimRoomForAgents(ctx, agentStore, s.maxAgents, len(archived)); err != nil {
			return err
		}
		if err := claimRoomForRestoredChildren(ctx, agentStore, s.maxChildren, archived); err != nil {
			return err
		}
		if err := requireNamesFree(ctx, agentStore, archived); err != nil {
			return err
		}
		if err := agentStore.Restore(ctx, agentIDs(archived)); err != nil {
			return err
		}
		if err := stampRestoredPRs(ctx, agentStore, archived); err != nil {
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

	restored, err := s.agents.ListByID(ctx, agentIDs(agents))
	if err != nil {
		return nil, err
	}

	// The read-back is every agent the caller named, since one that was already live comes back
	// as it stands, but only the ones this call took out of the archive have been restored.
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
// archived, since the panel draws it indented under a row that would not be there.
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
// longer in the listing at all.
func describeParent(name string) string {
	if name == "" {
		return "the agent it is attached to"
	}

	return fmt.Sprintf("%q", name)
}

// withChildren adds the children of the agents a call named to the list it acts on, leaving a
// child named on its own to be acted on alone.
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

	return nil, fmt.Errorf("%w: %s", ErrAgentNotFound, describeMissing(missing))
}

func (s *LifecycleService) removeAgents(ctx context.Context, ids []string) error {
	// The refs holding this agent's snapshots go before the rows naming them do, since the rows
	// are the only record of which refs they were.
	s.releaseRounds(ctx, ids)

	// The delete is what says which pane each row really held, so a pane written after the
	// caller read the rows is reached here and nowhere else.
	var taken []agentstore.AgentPane
	if err := s.tx.InTx(ctx, func(db store.DBTX) error {
		deleted, err := agentstore.New(db).Delete(ctx, ids)
		if err != nil {
			return err
		}
		taken = deleted

		return transcriptstore.New(db).Delete(ctx, ids)
	}); err != nil {
		return err
	}

	killPanes(ctx, s.tmux, taken)
	s.removeRunLogs(ctx, ids)
	s.removeFrames(ctx, ids)

	return nil
}

// releaseRounds drops the refs holding an agent's snapshots, which would otherwise pin a commit
// covering its whole worktree in the person's own repository long after the agent is gone, and a
// failure here leaks a ref rather than losing anything.
func (s *LifecycleService) releaseRounds(ctx context.Context, ids []string) {
	if s.rounds == nil {
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

		rounds, err := s.rounds.ListRounds(ctx, agent.ID)
		if err != nil {
			logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to read the rounds of an agent being deleted",
				append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)

			continue
		}

		for _, round := range rounds {
			if err := diff.Release(ctx, agent.Cwd, RoundRef(round.SnapshotSHA)); err != nil {
				logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to release the snapshot of a round",
					append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)
			}
		}
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
			logging.From(ctx).Warn("failed to remove an agent's recorded frames", "path", path, "agent_id", id, "error", err)
		}
	}
}

// removeRunLogs drops the runs an agent leaves on disk once its row is gone, logging a
// failure rather than returning it since a leftover file costs nothing.
func (s *LifecycleService) removeRunLogs(ctx context.Context, ids []string) {
	if s.logsDir == "" {
		return
	}

	for _, id := range ids {
		path := config.RunLogDir(s.logsDir, id)
		if err := os.RemoveAll(path); err != nil {
			logging.From(ctx).Warn("failed to remove an agent's run logs", "path", path, "agent_id", id, "error", err)
		}
	}
}
