// Package storetest opens the stores over a throwaway database and seeds the rows a test needs,
// so a test names the agent or workflow it is about rather than the columns.
package storetest

import (
	"context"
	"fmt"
	"net/url"
	"testing"

	"weave/config"
	"weave/db"
	"weave/internal/pgtest"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	mailstore "weave/internal/store/mail"
	schedulestore "weave/internal/store/schedule"
	transcriptstore "weave/internal/store/transcript"
	workflowstore "weave/internal/store/workflow"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

type Stores struct {
	URL         string
	ReadOnlyURL string
	Pool        *store.Pool
	Agents      *agentstore.Store
	Workflows   *workflowstore.Store
	Schedules   *schedulestore.Store
	Transcripts *transcriptstore.Store
	Mail        *mailstore.Store
}

// Open builds the stores over a database of this test's own, closed when the
// test ends.
func Open(t *testing.T) *Stores {
	t.Helper()

	dbURL := pgtest.URL(t)

	pool, err := store.Open(t.Context(), dbURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })

	return &Stores{
		URL:         dbURL,
		ReadOnlyURL: grantReadOnly(t, pool, dbURL),
		Pool:        pool,
		Agents:      agentstore.New(pool.GetDB()),
		Workflows:   workflowstore.New(pool.GetDB()),
		Schedules:   schedulestore.New(pool.GetDB()),
		Transcripts: transcriptstore.New(pool.GetDB()),
		Mail:        mailstore.New(pool.GetDB()),
	}
}

func grantReadOnly(t *testing.T, pool *store.Pool, dbURL string) string {
	t.Helper()

	cfg, err := config.Load(nil)
	require.NoError(t, err)

	readOnly, err := pgconn.ParseConfig(cfg.Connection.ReadOnlyDatabaseURL)
	require.NoError(t, err)

	require.NoError(t, db.GrantReadOnlyRole(t.Context(), pool.GetRawPool(), readOnly.User, readOnly.Password))

	parsed, err := url.Parse(dbURL)
	require.NoError(t, err)
	parsed.User = url.UserPassword(readOnly.User, readOnly.Password)

	return parsed.String()
}

// SeedAgent records an agent and returns it as stored, filing one with no workflow set under
// a workflow created for it.
func (s *Stores) SeedAgent(t *testing.T, agent agentstore.Agent) *agentstore.Agent {
	t.Helper()

	if agent.WorkflowID == "" {
		agent.WorkflowID = s.SeedWorkflow(t, workflowstore.Config{Name: "workflow-for-" + agent.Name}).ID
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

// SeedTranscript writes an agent's transcript, which is how a test makes one look like it has
// said something nobody has read since.
func (s *Stores) SeedTranscript(t *testing.T, agentID, content string) {
	t.Helper()

	require.NoError(t, s.Transcripts.Upsert(t.Context(), transcriptstore.UpsertParams{
		AgentID: agentID,
		Content: content,
		Offset:  int64(len(content)),
	}))
}

// SeedWorkflow creates a workflow and returns it, for the tests that only need
// something to file an agent under.
func (s *Stores) SeedWorkflow(t *testing.T, cfg workflowstore.Config) *workflowstore.Workflow {
	t.Helper()

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
