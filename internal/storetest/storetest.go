// Package storetest opens the stores over a throwaway database and seeds the rows a test needs,
// so a test names the agent or workflow it is about rather than the columns.
package storetest

import (
	"context"
	"fmt"
	"testing"

	"weave/internal/dbtest"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	mailstore "weave/internal/store/mail"
	markstore "weave/internal/store/mark"
	pullrequeststore "weave/internal/store/pullrequest"
	rowstore "weave/internal/store/row"
	runstore "weave/internal/store/run"
	schedulestore "weave/internal/store/schedule"
	transcriptstore "weave/internal/store/transcript"
	workflowstore "weave/internal/store/workflow"
	"weave/lib/xslice"

	"github.com/stretchr/testify/require"
)

type Stores struct {
	URL              string
	ReadOnlyURL      string
	Pool             *store.Pool
	Agents           *agentstore.AgentStore
	Rows             *rowstore.RowStore
	Workflows        *workflowstore.WorkflowStore
	Schedules        *schedulestore.ScheduleStore
	Transcripts      *transcriptstore.TranscriptStore
	Mail             *mailstore.MailStore
	Runs             *runstore.RunStore
	Marks            *markstore.MarkStore
	PullRequests     *pullrequeststore.PullRequestStore
	PullRequestSyncs *pullrequeststore.PullRequestSyncStore
}

// Open builds the stores over a database of this test's own, closed when the
// test ends.
func Open(t *testing.T) *Stores {
	t.Helper()

	engine := dbtest.NewEngine(t, dbtest.New(t))

	pool, err := store.Open(t.Context(), engine.URL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })

	return &Stores{
		URL:              engine.URL,
		ReadOnlyURL:      engine.ReaderURL,
		Pool:             pool,
		Agents:           agentstore.NewAgentStore(pool.GetDB()),
		Rows:             rowstore.NewRowStore(pool.GetDB()),
		Workflows:        workflowstore.NewWorkflowStore(pool.GetDB()),
		Schedules:        schedulestore.NewScheduleStore(pool.GetDB()),
		Transcripts:      transcriptstore.NewTranscriptStore(pool.GetDB()),
		Mail:             mailstore.NewMailStore(pool.GetDB()),
		Runs:             runstore.NewRunStore(pool.GetDB()),
		Marks:            markstore.NewMarkStore(pool.GetDB()),
		PullRequests:     pullrequeststore.NewPullRequestStore(pool.GetDB()),
		PullRequestSyncs: pullrequeststore.NewPullRequestSyncStore(pool.GetDB()),
	}
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

	require.NoError(t, s.Agents.Create(t.Context(), agent))

	stored, err := s.Agents.GetInWorkflow(t.Context(), agent.WorkflowID, agent.Name)
	require.NoError(t, err)
	require.NotNil(t, stored)

	return stored
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
