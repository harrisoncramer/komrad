package agents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"weave/internal/logging"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
)

const prStateTimeout = 15 * time.Second

// prStateBackoff is how long a rate-limited agent is left alone before its pull request is
// asked about again. A merge nobody is waiting on is worth less than the quota, so the pause
// is long and flat rather than a queue with a per-window budget.
const prStateBackoff = 15 * time.Minute

const (
	prStateMerged = "MERGED"
	prStateClosed = "CLOSED"
)

// prStateResolver reads the pull request an agent already recorded, which is where its
// current state comes from.
type prStateResolver interface {
	Resolve(ctx context.Context, dir, ref string) (*PullRequest, error)
}

type prArchiveAgentLister interface {
	List(ctx context.Context) ([]agentstore.Agent, error)
}

type prArchiveWorkflowGetter interface {
	Get(ctx context.Context, id string) (*workflowstore.Workflow, error)
}

type prArchiveAgentArchiver interface {
	ArchiveAgents(ctx context.Context, ids []string) error
}

type PRArchiverParams struct {
	Agents       prArchiveAgentLister
	Workflows    prArchiveWorkflowGetter
	Archiver     prArchiveAgentArchiver
	PullRequests prStateResolver
}

// PRArchiver archives an agent once the pull request it recorded has merged or closed, for
// the worktree workflows that asked for it.
type PRArchiver struct {
	agents       prArchiveAgentLister
	workflows    prArchiveWorkflowGetter
	archiver     prArchiveAgentArchiver
	pullRequests prStateResolver

	// backoff is confined to the pass, which the loop runs one at a time, so it needs no
	// lock of its own.
	backoff map[string]time.Time

	now func() time.Time
}

// NewPRArchiver builds an archiver over the agent store, the workflows that hold the
// setting, and the way an agent is archived.
func NewPRArchiver(p PRArchiverParams) *PRArchiver {
	return &PRArchiver{
		agents:       p.Agents,
		workflows:    p.Workflows,
		archiver:     p.Archiver,
		pullRequests: p.PullRequests,
		backoff:      map[string]time.Time{},
		now:          time.Now,
	}
}

// Sync archives every agent whose recorded pull request has since merged or closed. An agent
// still working is left for a later pass, since archiving kills its pane and a finished pull
// request is no reason to take a turn away from it. Nothing is deleted: the record, the
// transcript, and the worktree all survive, and restoring the agent brings it back.
func (a *PRArchiver) Sync(ctx context.Context) error {
	agents, err := a.agents.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list agents for pull request archiving: %w", err)
	}

	a.forgetExpiredBackoff()

	wanted := map[string]bool{}

	var failures []error
	var finished []string
	for _, agent := range agents {
		url, ok := recordedPR(agent)
		if !ok || !readyToArchive(agent) || a.isBackedOff(agent.ID) {
			continue
		}

		archives, err := a.archivesOnPRClosed(ctx, wanted, agent.WorkflowID)
		if err != nil {
			failures = append(failures, err)

			continue
		}
		if !archives {
			continue
		}

		if a.closed(ctx, agent, url) {
			finished = append(finished, agent.ID)

			logging.From(ctx).LogAttrs(ctx, slog.LevelInfo, "archiving an agent whose pull request closed",
				slog.String(logging.AgentID, agent.ID))
		}
	}

	if len(finished) > 0 {
		if err := a.archiver.ArchiveAgents(ctx, finished); err != nil {
			failures = append(failures, fmt.Errorf("failed to archive %d agents whose pull requests closed: %w", len(finished), err))
		}
	}

	return errors.Join(failures...)
}

// closed reports whether the pull request an agent recorded has merged or closed. A lookup
// that fails leaves the agent alone: an agent still worth having is a better outcome than
// one archived because gh could not answer.
func (a *PRArchiver) closed(ctx context.Context, agent agentstore.Agent, url string) bool {
	lookupCtx, cancel := context.WithTimeout(ctx, prStateTimeout)
	defer cancel()

	pr, err := a.pullRequests.Resolve(lookupCtx, prLookupDir(agent), url)
	if err != nil {
		if isGHRateLimitError(err) {
			a.backOff(agent.ID)
		}

		logging.From(ctx).LogAttrs(ctx, slog.LevelDebug, "failed to read the state of an agent's pull request",
			append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)

		return false
	}

	return pr.State == prStateMerged || pr.State == prStateClosed
}

// archivesOnPRClosed reports whether a workflow asked for this, reading each workflow once
// per pass rather than once per agent inside it.
func (a *PRArchiver) archivesOnPRClosed(ctx context.Context, wanted map[string]bool, workflowID string) (bool, error) {
	if archives, ok := wanted[workflowID]; ok {
		return archives, nil
	}

	wf, err := a.workflows.Get(ctx, workflowID)
	if err != nil {
		return false, fmt.Errorf("failed to read workflow %q for pull request archiving: %w", workflowID, err)
	}

	archives := wf != nil && wf.Worktree.Enabled && wf.Worktree.ArchiveOnPRClosed
	wanted[workflowID] = archives

	return archives, nil
}

func (a *PRArchiver) isBackedOff(agentID string) bool {
	until, ok := a.backoff[agentID]

	return ok && a.now().Before(until)
}

func (a *PRArchiver) backOff(agentID string) {
	a.backoff[agentID] = a.now().Add(prStateBackoff)
}

func (a *PRArchiver) forgetExpiredBackoff() {
	for id, until := range a.backoff {
		if !a.now().Before(until) {
			delete(a.backoff, id)
		}
	}
}

// readyToArchive reports whether an agent can lose its pane without losing work: one sitting
// idle, or one already dormant with no pane to lose.
func readyToArchive(agent agentstore.Agent) bool {
	return agent.State == agentstore.StateIdle || agent.Pane == ""
}

func recordedPR(agent agentstore.Agent) (string, bool) {
	i := slices.IndexFunc(agent.Metadata, func(m agentstore.Metadata) bool { return m.Key == prMetadataKey })
	if i < 0 || agent.Metadata[i].Value == "" {
		return "", false
	}

	return agent.Metadata[i].Value, true
}

// prLookupDir is where gh runs, which is the agent's own worktree while it still exists.
// A worktree already removed leaves the lookup to run wherever weave itself is, since the
// pull request URL names its own repository.
func prLookupDir(agent agentstore.Agent) string {
	if agent.WorktreeRemoved {
		return ""
	}

	return agent.Cwd
}
