package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListTables confirms the app's tables are listed with their counts.
func TestListTables(t *testing.T) {
	tables, err := openTools(t).listTables(context.Background())
	require.NoError(t, err)

	byName := make(map[string]int64, len(tables))
	for _, table := range tables {
		byName[table.Name] = table.Rows
	}

	assert.Contains(t, byName, "sessions")
	assert.Contains(t, byName, "session_configs")
	assert.Contains(t, byName, "workflows")
	assert.Contains(t, byName, "session_transcripts")
	assert.Equal(t, int64(1), byName["sessions"])
}

func TestDescribeTable(t *testing.T) {
	desc, err := openTools(t).describeTable(context.Background(), "sessions")
	require.NoError(t, err)

	byName := make(map[string]columnInfo, len(desc.Columns))
	for _, col := range desc.Columns {
		byName[col.Name] = col
	}

	require.Contains(t, byName, "id")
	assert.True(t, byName["id"].PrimaryKey)
	assert.False(t, byName["id"].Nullable)

	require.Contains(t, byName, "workflow_id")
	assert.True(t, byName["workflow_id"].Nullable)

	require.Contains(t, byName, "state")
	assert.Equal(t, "'new'::text", byName["state"].Default,
		"Postgres reports a default with the type it is coerced to")

	assert.NotEmpty(t, desc.Indexes, "the partial unique name index should be reported")
}

func TestDescribeTableRejectsUnknownAndMalformedNames(t *testing.T) {
	tools := openTools(t)

	_, err := tools.describeTable(context.Background(), "sessions; DROP TABLE sessions")
	require.ErrorContains(t, err, "invalid table name")

	_, err = tools.describeTable(context.Background(), "nope")
	require.ErrorContains(t, err, "not found")
}

// TestListTablesExcludesSystemCatalogs confirms the listing shows the schema a
// caller cares about and not Postgres's own bookkeeping, which lives in other
// schemas and would bury it.
func TestListTablesExcludesSystemCatalogs(t *testing.T) {
	tools := openTools(t)

	tables, err := tools.listTables(context.Background())
	require.NoError(t, err)

	names := make([]string, 0, len(tables))
	for _, table := range tables {
		names = append(names, table.Name)
	}

	assert.Contains(t, names, "sessions")
	assert.Contains(t, names, "session_transcripts")
	for _, name := range names {
		assert.False(t, strings.HasPrefix(name, "pg_"), "system catalog %q should not be listed", name)
	}
}
