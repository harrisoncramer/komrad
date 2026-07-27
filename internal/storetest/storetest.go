// Package storetest hands a test the domain stores over a throwaway database.
//
// Every package above the store layer needs the same three lines to get going:
// open a pool against a fresh database, register its cleanup, and build a store
// per domain over it. This is that, once.
//
// The store packages' own tests cannot use it. Their tests live in the package
// being tested, and this imports those packages, so taking it would be an import
// cycle. They keep their own opener, which is small because it builds one store
// rather than all of them.
package storetest

import (
	"testing"

	"claude-agents/internal/pgtest"
	"claude-agents/internal/store"
	agentstore "claude-agents/internal/store/agent"
	transcriptstore "claude-agents/internal/store/transcript"
	workflowstore "claude-agents/internal/store/workflow"

	"github.com/stretchr/testify/require"
)

// Stores are the domain stores over one database, plus the pool itself, which is
// the transaction boundary the writes spanning two domains run through.
type Stores struct {
	// URL is the database these stores were opened against, for a test that needs
	// a second connection to it, such as the read-only pool the MCP tools use.
	URL         string
	Pool        *store.Pool
	Agents      *agentstore.Store
	Workflows   *workflowstore.Store
	Transcripts *transcriptstore.Store
}

// Open builds the stores over a database of this test's own, closed when the
// test ends.
func Open(t *testing.T) *Stores {
	t.Helper()

	url := pgtest.URL(t)

	pool, err := store.Open(t.Context(), url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })

	return &Stores{
		URL:         url,
		Pool:        pool,
		Agents:      agentstore.New(pool.DB()),
		Workflows:   workflowstore.New(pool.DB()),
		Transcripts: transcriptstore.New(pool.DB()),
	}
}

// SeedAgent records an agent and returns it as stored, since a test usually
// needs the generated id rather than the fields it just wrote.
func (s *Stores) SeedAgent(t *testing.T, agent agentstore.Agent) *agentstore.Agent {
	t.Helper()

	require.NoError(t, s.Agents.Upsert(t.Context(), agent))

	stored, err := s.Agents.Get(t.Context(), agent.Name)
	require.NoError(t, err)
	require.NotNil(t, stored)

	return stored
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
