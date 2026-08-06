// Package storetest hands a test the domain stores over a throwaway database.
//
// Every package above the store layer needs the same three lines to get going:
// open a pool against a fresh database, register its cleanup, and build a store
// per domain over it. This is that, once.
//
// The store packages' own tests cannot use it. Their tests live in the package
// being tested and this imports those packages, so taking it would be an import
// cycle. They keep their own opener, which builds one store rather than all of
// them.
package storetest

import (
	"context"
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

// Stores are the domain stores over one database, plus the pool itself, which is
// the transaction boundary the writes spanning two domains run through.
type Stores struct {
	// URL is the database these stores were opened against, for a test that needs
	// a second connection to it.
	URL string
	// ReadOnlyURL reaches the same database as the least-privilege role, which is what
	// the MCP tools connect as. A test that opens it exercises the grants rather than
	// the schema alone, since a missing one shows up as a refused read.
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

// grantReadOnly provisions the least-privilege role on this test's database and returns a
// url that reaches it as that role. It runs after the store has migrated, so the grant
// covers the tables the schema just created.
//
// The role itself is a cluster object shared by every test database, which is why the SQL
// tolerates one already existing. Only the grants are per-database, and those are what a
// test is exercising.
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

// SeedAgent records an agent and returns it as stored, since a test usually
// needs the generated id rather than the fields it just wrote. An agent with no
// workflow set is filed under one created for it, since every agent belongs to
// a workflow and most tests do not care which.
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

// LiveAgentByName finds the one live agent holding a name, nil when none does. Nothing in
// production reaches an agent this way, since a name is unique per workflow rather than
// panel-wide. A test that chose the names itself knows they are unambiguous.
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

	return nil, nil
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

// SeedSchedule records a schedule and returns it, for the tests that need one
// to aim at rather than one to inspect. A schedule with no workflow set is filed
// under one created for it, since a schedule launches into a workflow and most
// tests do not care which.
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
