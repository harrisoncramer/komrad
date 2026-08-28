package agents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"weave/internal/logging"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
)

const prStateTimeout = 15 * time.Second

// prStateQuiet is how long an agent is left alone after a pass has asked about it and
// learned nothing worth acting on, whether the pull request was open or GitHub refused to
// answer. A merge nobody is waiting on is worth less than the quota, so the pause is long
// and flat rather than a queue with a per-window budget.
const prStateQuiet = 15 * time.Minute

// prArchivedMetadataKey records the pull request an agent was archived for, written when the
// loop archives one and again when somebody restores one. It is what stops an agent
// deliberately brought back from being archived again by the next pass, and it is the only
// thing on the row that says why the agent went away.
const prArchivedMetadataKey = "pr_archived"

const (
	prStateMerged = "MERGED"
	prStateClosed = "CLOSED"
)

// prStateResolver reads the pull request an agent recorded, and reports which repository a
// directory belongs to so the two can be checked against each other.
type prStateResolver interface {
	Resolve(ctx context.Context, dir, ref string) (*PullRequest, error)
	Repository(ctx context.Context, dir string) (string, error)
}

type prArchiveAgentStore interface {
	List(ctx context.Context) ([]agentstore.Agent, error)
	SetMetadata(ctx context.Context, params agentstore.SetMetadataParams) (bool, error)
}

type prArchiveWorkflowGetter interface {
	Get(ctx context.Context, id string) (*workflowstore.Workflow, error)
}

type prArchiveAgentArchiver interface {
	ArchiveUnchangedAgents(ctx context.Context, observed []agentstore.Agent) ([]agentstore.Agent, error)
}

type PRArchiverParams struct {
	Agents       prArchiveAgentStore
	Workflows    prArchiveWorkflowGetter
	Archiver     prArchiveAgentArchiver
	PullRequests prStateResolver
}

// PRArchiver archives an agent once the pull request it recorded has merged or closed, for
// the workflows that asked for it.
type PRArchiver struct {
	agents       prArchiveAgentStore
	workflows    prArchiveWorkflowGetter
	archiver     prArchiveAgentArchiver
	pullRequests prStateResolver

	// quiet is confined to the pass, which the loop runs one at a time, so it needs no lock
	// of its own. Neither does now. Both would were a pass ever to look agents up
	// concurrently.
	quiet map[string]time.Time

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
		quiet:        map[string]time.Time{},
		now:          time.Now,
	}
}

// prArchiveWorkflow is what a pass needs to know about a workflow its agents belong to, read
// once each rather than once per agent inside it.
type prArchiveWorkflow struct {
	archives bool
	repo     string
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

	a.forgetExpiredQuiet()

	seen := map[string]prArchiveWorkflow{}

	var failures []error
	var finished []agentstore.Agent
	for _, agent := range agents {
		url, ok := recordedPR(agent)
		if !ok || !readyToArchive(agent) || a.isQuiet(agent.ID) {
			continue
		}

		wf, err := a.workflow(ctx, seen, agent.WorkflowID)
		if err != nil {
			failures = append(failures, err)

			continue
		}
		if !wf.archives {
			continue
		}

		if a.closed(ctx, agent, url, wf.repo) {
			finished = append(finished, agent)
		}
	}

	archived, err := a.archive(ctx, finished)
	if err != nil {
		failures = append(failures, err)
	}

	a.stamp(ctx, archived)

	return errors.Join(failures...)
}

func (a *PRArchiver) archive(ctx context.Context, finished []agentstore.Agent) ([]agentstore.Agent, error) {
	if len(finished) == 0 {
		return nil, nil
	}

	archived, err := a.archiver.ArchiveUnchangedAgents(ctx, finished)
	if err != nil {
		return archived, fmt.Errorf("failed to archive %d agents whose pull requests closed: %w", len(finished), err)
	}

	return archived, nil
}

// stamp records which pull request each agent was archived for. An agent it cannot write is
// one a later restore would archive again, so the failure is logged rather than swallowed.
func (a *PRArchiver) stamp(ctx context.Context, archived []agentstore.Agent) {
	for _, agent := range archived {
		url, ok := recordedPR(agent)
		if !ok {
			continue
		}

		written, err := a.agents.SetMetadata(ctx, agentstore.SetMetadataParams{ID: agent.ID, Key: prArchivedMetadataKey, Value: &url})
		if err != nil {
			logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to record why an agent was archived",
				append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)

			continue
		}
		if !written {
			logging.From(ctx).LogAttrs(ctx, slog.LevelError, "an agent was archived for a pull request without recording which one, so restoring it will archive it again",
				slog.String(logging.AgentID, agent.ID))
		}
	}
}

// closed reports whether the pull request an agent recorded has merged or closed, and that it
// is the pull request weave thinks it is. A lookup that fails leaves the agent alone: an agent
// still worth having is a better outcome than one archived because gh could not answer.
func (a *PRArchiver) closed(ctx context.Context, agent agentstore.Agent, url, repo string) bool {
	lookupCtx, cancel := context.WithTimeout(ctx, prStateTimeout)
	defer cancel()

	pr, err := a.pullRequests.Resolve(lookupCtx, prLookupDir(agent), url)
	if err != nil {
		a.beQuiet(agent.ID)

		logging.From(ctx).LogAttrs(ctx, slog.LevelDebug, "failed to read the state of an agent's pull request",
			append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)

		return false
	}

	if pr.State != prStateMerged && pr.State != prStateClosed {
		a.beQuiet(agent.ID)

		return false
	}

	if !answersAbout(agent, *pr, repo) {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "an agent's recorded pull request is not the one it is working on, so it was left alone",
			slog.String(logging.AgentID, agent.ID),
			slog.String(logging.PullRequestURL, pr.URL))

		a.beQuiet(agent.ID)

		return false
	}

	return true
}

// answersAbout reports whether a finished pull request is still the one this agent is on,
// since nothing refreshes the recorded one.
func answersAbout(agent agentstore.Agent, pr PullRequest, repo string) bool {
	// One casing comes from a pull request URL and the other from a git remote.
	if repo != "" && pr.Repository != "" && !strings.EqualFold(pr.Repository, repo) {
		return false
	}

	branch := agentBranch(agent)
	// A timeout, a detached HEAD, and a directory that is gone all read as a blank branch, so
	// a removed worktree is what an agent with nothing to compare is judged by.
	if branch == "" {
		return agent.WorktreeRemoved
	}

	return pr.Branch == branch
}

func agentBranch(agent agentstore.Agent) string {
	if agent.AdoptedBranch != "" {
		return agent.AdoptedBranch
	}
	if agent.Branch != nil {
		return *agent.Branch
	}

	return ""
}

// workflow reads whether a workflow asked for this and which repository it points at, once
// per workflow per pass.
func (a *PRArchiver) workflow(ctx context.Context, seen map[string]prArchiveWorkflow, workflowID string) (prArchiveWorkflow, error) {
	if known, ok := seen[workflowID]; ok {
		return known, nil
	}

	wf, err := a.workflows.Get(ctx, workflowID)
	if err != nil {
		return prArchiveWorkflow{}, fmt.Errorf("failed to read workflow %q for pull request archiving: %w", workflowID, err)
	}

	answer := prArchiveWorkflow{archives: wf != nil && wf.ArchiveOnPRClosed}
	if answer.archives {
		answer.repo = a.repositoryOf(ctx, wf.Cwd)
	}
	seen[workflowID] = answer

	return answer, nil
}

// repositoryOf names the repository a workflow's own checkout points at, blank when that
// cannot be read. Blank leaves the branch to do the checking on its own rather than refusing
// to archive anything.
func (a *PRArchiver) repositoryOf(ctx context.Context, cwd string) string {
	if cwd == "" {
		return ""
	}

	lookupCtx, cancel := context.WithTimeout(ctx, prStateTimeout)
	defer cancel()

	repo, err := a.pullRequests.Repository(lookupCtx, cwd)
	if err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelDebug, "failed to read the repository a workflow points at",
			logging.Err(err)...)

		return ""
	}

	return repo
}

func (a *PRArchiver) isQuiet(agentID string) bool {
	until, ok := a.quiet[agentID]

	return ok && a.now().Before(until)
}

func (a *PRArchiver) beQuiet(agentID string) {
	a.quiet[agentID] = a.now().Add(prStateQuiet)
}

func (a *PRArchiver) forgetExpiredQuiet() {
	for id, until := range a.quiet {
		if !a.now().Before(until) {
			delete(a.quiet, id)
		}
	}
}

// readyToArchive reports whether an agent can lose its pane without losing work: one sitting
// idle, or one already dormant with no pane to lose. An agent still being set up, or one that
// failed, is not the pull request's to archive.
func readyToArchive(agent agentstore.Agent) bool {
	if agent.State == agentstore.StateSettingUp || agentstore.IsFailed(agent.State) {
		return false
	}

	return agent.State == agentstore.StateIdle || agent.Pane == ""
}

// recordedPR reads the pull request an agent recorded, which agents write themselves and
// weave writes for them. Only a pull request URL counts: a bare number is a reference gh
// resolves against whichever repository it runs in, which is not something to archive an
// agent on. An agent already archived for that same pull request is not offered again, which
// is what leaves one somebody restored alone.
func recordedPR(agent agentstore.Agent) (string, bool) {
	url := metadataFor(agent, prMetadataKey)
	if url == "" || !strings.Contains(url, "/pull/") {
		return "", false
	}
	if metadataFor(agent, prArchivedMetadataKey) == url {
		return "", false
	}

	return url, true
}

func metadataFor(agent agentstore.Agent, key string) string {
	i := slices.IndexFunc(agent.Metadata, func(m agentstore.Metadata) bool { return m.Key == key })
	if i < 0 {
		return ""
	}

	return agent.Metadata[i].Value
}

// prLookupDir is where gh runs, which is the agent's own directory while it still exists.
// A worktree already removed leaves the lookup to run wherever weave itself is, since the
// pull request URL names its own repository.
func prLookupDir(agent agentstore.Agent) string {
	if agent.WorktreeRemoved {
		return ""
	}

	return agent.Cwd
}

type prStampStore interface {
	SetMetadata(ctx context.Context, params agentstore.SetMetadataParams) (bool, error)
}

// stampRestoredPRs records the pull request each restored agent carries as one already dealt
// with, so a later pass leaves the agent alone. The loop stamps what it archives itself, but
// it cannot tell that archive from one a person made by hand, and a hand-archived agent whose
// pull request has since finished is archived again two minutes after somebody brought it
// back. Restoring is that person deciding they want the agent, so the decision is written down
// as they make it.
//
// It is written whatever state the pull request is in, since reading that means asking GitHub
// and a restore is somebody waiting on a keypress. An agent restored while its pull request is
// still open is therefore not archived when it later merges, which is the safe way round: the
// person who pulled it back gets to keep it, and a pull request they open afterwards is a
// different URL that archives normally.
func stampRestoredPRs(ctx context.Context, store prStampStore, restored []agentstore.Agent) error {
	var failures []error
	for _, agent := range restored {
		url, ok := recordedPR(agent)
		if !ok {
			continue
		}

		if _, err := store.SetMetadata(ctx, agentstore.SetMetadataParams{ID: agent.ID, Key: prArchivedMetadataKey, Value: &url}); err != nil {
			failures = append(failures, fmt.Errorf("failed to record the pull request agent %q was restored with: %w", agent.ID, err))
		}
	}

	return errors.Join(failures...)
}
