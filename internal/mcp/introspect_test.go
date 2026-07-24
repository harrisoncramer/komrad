package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListTables confirms the app's tables are listed with their counts and that
// SQLite's own bookkeeping and the FTS index's shadow tables stay out of it.
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
	assert.Equal(t, int64(1), byName["sessions"])

	for name := range byName {
		assert.False(t, strings.HasPrefix(name, "sqlite_"), "internal table %q should not be listed", name)
		assert.False(t, strings.HasPrefix(name, "session_transcripts_fts_"), "fts shadow table %q should not be listed", name)
	}
	// The virtual table itself is real and queryable, so it stays.
	assert.Contains(t, byName, "session_transcripts_fts")
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
	assert.Equal(t, "'new'", byName["state"].Default)

	assert.NotEmpty(t, desc.Indexes, "the partial unique name index should be reported")
}

func TestDescribeTableRejectsUnknownAndMalformedNames(t *testing.T) {
	tools := openTools(t)

	_, err := tools.describeTable(context.Background(), "sessions; DROP TABLE sessions")
	require.ErrorContains(t, err, "invalid table name")

	_, err = tools.describeTable(context.Background(), "nope")
	require.ErrorContains(t, err, "not found")
}

func TestIsShadowTable(t *testing.T) {
	virtual := []string{"session_transcripts_fts"}

	assert.True(t, isShadowTable("session_transcripts_fts_data", virtual))
	assert.False(t, isShadowTable("session_transcripts_fts", virtual))
	assert.False(t, isShadowTable("session_transcripts", virtual))
}
