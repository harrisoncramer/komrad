// Package storetest opens the stores over a throwaway database and seeds the rows a test needs,
// so a test names the agent or workflow it is about rather than the columns.
package storetest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"komrad/internal/dbtest"
	"komrad/internal/forge"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	documentstore "komrad/internal/store/document"
	documenttypestore "komrad/internal/store/documenttype"
	mailstore "komrad/internal/store/mail"
	pullrequeststore "komrad/internal/store/pullrequest"
	rowstore "komrad/internal/store/row"
	runstore "komrad/internal/store/run"
	schedulestore "komrad/internal/store/schedule"
	screenstore "komrad/internal/store/screen"
	taskstore "komrad/internal/store/task"
	"komrad/internal/store/task/tasktest"
	transcriptstore "komrad/internal/store/transcript"
	workflowstore "komrad/internal/store/workflow"
	"komrad/lib/xslice"

	"github.com/stretchr/testify/require"
)

type Stores struct {
	OwnerURL         string
	Engine           string
	URL              string
	ReadOnlyURL      string
	Pool             *store.Pool
	Agents           *agentstore.AgentStore
	Rows             *rowstore.RowStore
	Workflows        *workflowstore.WorkflowStore
	Schedules        *schedulestore.ScheduleStore
	Transcripts      *transcriptstore.TranscriptStore
	Mail             *mailstore.MailStore
	Documents        *documentstore.DocumentStore
	DocumentTypes    *documenttypestore.DocumentTypeStore
	Runs             *runstore.RunStore
	Screens          *screenstore.ScreenStore
	PullRequests     *pullrequeststore.PullRequestStore
	PullRequestSyncs *pullrequeststore.PullRequestSyncStore
	ForgeThreads     *pullrequeststore.ForgeThreadStore
	Tasks            *taskstore.TaskStore
	Access           *store.AccessStore
}

// Open builds the stores over a database of this test's own, closed when the
// test ends.
func Open(t *testing.T) *Stores {
	t.Helper()

	database := dbtest.New(t)
	engine := dbtest.NewEngine(t, database)

	pool, err := store.Open(t.Context(), engine.URL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })

	return &Stores{
		OwnerURL:         database.OwnerURL,
		Engine:           engine.Name,
		URL:              engine.URL,
		ReadOnlyURL:      engine.ReaderURL,
		Pool:             pool,
		Agents:           agentstore.NewAgentStore(pool.GetDB()),
		Rows:             rowstore.NewRowStore(pool.GetDB()),
		Workflows:        workflowstore.NewWorkflowStore(pool.GetDB()),
		Schedules:        schedulestore.NewScheduleStore(pool.GetDB()),
		Transcripts:      transcriptstore.NewTranscriptStore(pool.GetDB()),
		Mail:             mailstore.NewMailStore(pool.GetDB()),
		Documents:        documentstore.NewDocumentStore(pool.GetDB()),
		DocumentTypes:    documenttypestore.NewDocumentTypeStore(pool.GetDB()),
		Runs:             runstore.NewRunStore(pool.GetDB()),
		Screens:          screenstore.NewScreenStore(pool.GetDB()),
		PullRequests:     pullrequeststore.NewPullRequestStore(pool.GetDB()),
		ForgeThreads:     pullrequeststore.NewForgeThreadStore(pool.GetDB()),
		PullRequestSyncs: pullrequeststore.NewPullRequestSyncStore(pool.GetDB()),
		Tasks:            taskstore.NewTaskStore(pool.GetDB()),
		Access:           store.NewAccessStore(pool.GetDB()),
	}
}

// SeedAccess grants email access to the engine the stores are over, as the owner does.
func (s *Stores) SeedAccess(t *testing.T, email string) {
	t.Helper()

	require.NoError(t, store.GrantAccess(t.Context(), store.GrantAccessParams{
		OwnerURL: s.OwnerURL,
		Engine:   s.Engine,
		Email:    email,
	}))
}

// SeedAgent records an agent and returns it as stored, filing one with no workflow set under
// a workflow created for it.
func (s *Stores) SeedAgent(t *testing.T, agent agentstore.Agent) *agentstore.Agent {
	t.Helper()

	if agent.WorkflowID == "" {
		agent.WorkflowID = s.SeedWorkflow(t, workflowstore.Config{Name: "workflow-for-" + agent.Name}).ID
	}

	// The state column is an enum with no member for the empty string, so a test that does not
	// care which state its agent is in gets the one a fresh agent launches in.
	if agent.State == "" {
		agent.State = agentstore.StateIdle
	}

	if agent.TaskID == nil && agent.Role != agentstore.RoleOrchestrator {
		taskID, err := s.TaskFor(t.Context(), agent)
		require.NoError(t, err)
		agent.TaskID = taskID
	}

	require.NoError(t, s.Agents.Create(t.Context(), agent))

	stored, err := s.Agents.GetInWorkflow(t.Context(), agent.WorkflowID, agent.Name)
	require.NoError(t, err)
	require.NotNil(t, stored)

	return stored
}

// TaskFor is a task made for an agent written by a test.
func (s *Stores) TaskFor(ctx context.Context, agent agentstore.Agent) (*string, error) {
	return tasktest.Seed(ctx, s.Pool.GetDB(), agent.WorkflowID)
}

// LiveAgentByName finds the one live agent holding a name, which only a test can ask for
// since a name is unique per workflow rather than panel-wide.
func (s *Stores) LiveAgentByName(ctx context.Context, name string) (*agentstore.Agent, error) {
	listed, err := s.Agents.List(ctx)
	if err != nil {
		return nil, err
	}

	for _, agent := range listed {
		if agent.Name == name {
			return &agent, nil
		}
	}

	return nil, fmt.Errorf("%w: no live agent named %q", store.ErrNotFound, name)
}

// SeedTranscript stores prompts for an agent under a session of its own.
func (s *Stores) SeedTranscript(t *testing.T, agentID string, turns ...string) {
	t.Helper()

	s.SeedTurns(t, agentID, xslice.Map(turns, func(text string) transcriptstore.Turn {
		return transcriptstore.Turn{Role: "user", Content: text}
	})...)
}

// SeedDocumentTypes creates a document type for each name, with a context naming it.
func (s *Stores) SeedDocumentTypes(t *testing.T, names ...string) {
	t.Helper()

	for _, name := range names {
		_, err := s.DocumentTypes.Create(t.Context(), name, "How to write a "+name+".")
		require.NoError(t, err)
	}
}

// SeedTurns stores turns for an agent under a session of its own.
func (s *Stores) SeedTurns(t *testing.T, agentID string, turns ...transcriptstore.Turn) {
	t.Helper()

	session, err := s.Transcripts.OpenSession(t.Context(), transcriptstore.OpenSessionParams{
		AgentID:   agentID,
		SessionID: "session-" + agentID,
	})
	require.NoError(t, err)

	require.NoError(t, s.Transcripts.AppendTurns(t.Context(), transcriptstore.AppendTurnsParams{
		AgentID:   agentID,
		SessionID: session.ID,
		Turns:     turns,
	}))
}

// SeedWorkflow creates a workflow and returns it, for the tests that only need
// something to file an agent under, naming main as its base when the test names none.
func (s *Stores) SeedWorkflow(t *testing.T, cfg workflowstore.Config) *workflowstore.Workflow {
	t.Helper()

	if cfg.Worktree.BaseBranch == "" {
		cfg.Worktree.BaseBranch = "main"
	}

	created, err := s.Workflows.Create(t.Context(), cfg)
	require.NoError(t, err)
	require.NotNil(t, created)

	return created
}

// SeedTask creates a task and returns it, filing one with no workflow set under a workflow created
// for it.
func (s *Stores) SeedTask(t *testing.T, params taskstore.CreateParams) *taskstore.Task {
	t.Helper()

	if params.WorkflowID == "" {
		params.WorkflowID = s.SeedWorkflow(t, workflowstore.Config{Name: "workflow-for-" + params.Slug}).ID
	}
	if params.Title == "" {
		params.Title = params.Slug
	}

	created, err := s.Tasks.Create(t.Context(), params)
	require.NoError(t, err)

	return created
}

// SeedSchedule records a schedule and returns it, filing one with no workflow set under a
// workflow created for it.
func (s *Stores) SeedSchedule(t *testing.T, cfg schedulestore.Config) *schedulestore.Schedule {
	t.Helper()

	if cfg.WorkflowID == "" {
		cfg.WorkflowID = s.SeedWorkflow(t, workflowstore.Config{Name: "workflow-for-" + cfg.Name}).ID
	}

	created, err := s.Schedules.Create(t.Context(), cfg)
	require.NoError(t, err)
	require.NotNil(t, created)

	return created
}

// SeedTaskHead records what the cwd syncer would read of the checkout of an agent's task.
func (s *Stores) SeedTaskHead(t *testing.T, agentID string, params taskstore.SetHeadParams) {
	t.Helper()

	agent, err := s.Agents.GetByID(t.Context(), agentID)
	require.NoError(t, err)
	require.NotNil(t, agent.TaskID)
	require.NoError(t, s.Tasks.SetHead(t.Context(), *agent.TaskID, params))
}

// SeedForgeThreads opens a pull request on the agent's branch and stores the forge's threads on it, returning them as stored.
func (s *Stores) SeedForgeThreads(t *testing.T, agentID string, threads ...forge.ReviewThread) []pullrequeststore.ForgeThread {
	t.Helper()
	ctx := t.Context()

	agent, err := s.Agents.GetByID(ctx, agentID)
	require.NoError(t, err)
	require.NotNil(t, agent.TaskID)

	repository, branch := "https://github.com/harrisoncramer/komrad", "task/"+*agent.TaskID
	s.SeedTaskHead(t, agentID, taskstore.SetHeadParams{RepositoryURL: &repository, Branch: &branch})

	opened := time.Date(2026, time.September, 18, 12, 0, 0, 0, time.UTC)
	providerID := "PR_" + agentID
	_, err = s.PullRequests.Upsert(ctx, pullrequeststore.UpsertParams{
		Host:              "github.com",
		Forge:             "github",
		ProviderID:        providerID,
		URL:               "https://github.com/harrisoncramer/komrad/pull/1",
		RepositoryURL:     repository,
		HeadRepositoryURL: repository,
		Number:            1,
		Title:             "one",
		HeadBranch:        branch,
		HeadSHA:           "abc",
		BaseBranch:        "main",
		BaseSHA:           "def",
		State:             "OPEN",
		OpenedAt:          opened,
		ChangedAt:         opened,
	})
	require.NoError(t, err)
	_, err = s.PullRequests.Observe(ctx)
	require.NoError(t, err)

	pullRequest, err := s.PullRequests.GetByIdentity(ctx, "github.com", providerID)
	require.NoError(t, err)

	require.NoError(t, s.ForgeThreads.SetThreads(ctx, pullrequeststore.SetThreadsParams{
		PullRequestID: pullRequest.ID,
		Threads:       threads,
		Prune:         true,
	}))

	stored, err := s.ForgeThreads.ListOfPullRequest(ctx, pullRequest.ID)
	require.NoError(t, err)

	return stored
}

// SeedPullRequestParams is a pull request on github.com from one branch of a repository onto another.
type SeedPullRequestParams struct {
	RepositoryURL string
	Number        int
	HeadBranch    string
	HeadSHA       string
	BaseBranch    string
	// State is OPEN, MERGED or CLOSED, OPEN when empty.
	State string
	// OpenedAt is when the pull request opened, now when zero.
	OpenedAt time.Time
}

// SeedPullRequest stores a pull request and returns it as stored.
func (s *Stores) SeedPullRequest(t *testing.T, params SeedPullRequestParams) *pullrequeststore.PullRequest {
	t.Helper()

	opened := params.OpenedAt
	if opened.IsZero() {
		opened = time.Now()
	}

	upsert := pullrequeststore.UpsertParams{
		Host:              "github.com",
		Forge:             "github",
		ProviderID:        fmt.Sprintf("%s/pull/%d", params.RepositoryURL, params.Number),
		URL:               fmt.Sprintf("%s/pull/%d", params.RepositoryURL, params.Number),
		RepositoryURL:     params.RepositoryURL,
		HeadRepositoryURL: params.RepositoryURL,
		Number:            params.Number,
		Title:             params.HeadBranch,
		HeadBranch:        params.HeadBranch,
		HeadSHA:           params.HeadSHA,
		BaseBranch:        params.BaseBranch,
		State:             "OPEN",
		OpenedAt:          opened,
		ChangedAt:         opened,
	}
	switch params.State {
	case "MERGED":
		upsert.State, upsert.MergedAt, upsert.ClosedAt = params.State, &opened, &opened
	case "CLOSED":
		upsert.State, upsert.ClosedAt = params.State, &opened
	}

	_, err := s.PullRequests.Upsert(t.Context(), upsert)
	require.NoError(t, err)

	stored, err := s.PullRequests.GetByIdentity(t.Context(), upsert.Host, upsert.ProviderID)
	require.NoError(t, err)

	return stored
}
