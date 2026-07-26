package mcp

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListTables covers what the schema looks like to a model. Postgres keeps
// its own catalogs in other schemas, so the listing is the app's tables and
// nothing else.
func TestListTables(t *testing.T) {
	tests := []struct {
		name     string
		want     string
		wantRows int64
	}{
		{
			name:     "the chats themselves, counted",
			want:     "sessions",
			wantRows: 1,
		},
		{
			name: "the config each chat owns",
			want: "session_configs",
		},
		{
			name: "the buckets chats group into",
			want: "workflows",
		},
		{
			name:     "the transcripts search reads",
			want:     "session_transcripts",
			wantRows: 1,
		},
	}

	tables, err := openTools(t).listTables(t.Context())
	require.NoError(t, err)

	byName := make(map[string]int64, len(tables))
	for _, table := range tables {
		byName[table.Name] = table.Rows
		assert.False(t, strings.HasPrefix(table.Name, "pg_"), "a system catalog would bury the real schema")
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Contains(t, byName, tt.want)
			if tt.wantRows > 0 {
				assert.Equal(t, tt.wantRows, byName[tt.want])
			}
		})
	}
}

// TestDescribeTable covers the shape a model is given before it writes a query.
func TestDescribeTable(t *testing.T) {
	tests := []struct {
		name        string
		column      string
		wantNull    bool
		wantPrimary bool
		wantDefault string
	}{
		{
			name:        "the primary key is flagged and never null",
			column:      "id",
			wantPrimary: true,
		},
		{
			name:     "an unassigned chat has a null workflow",
			column:   "workflow_id",
			wantNull: true,
		},
		{
			name:        "a default is reported with the type it is coerced to",
			column:      "state",
			wantDefault: "'new'::text",
		},
	}

	desc, err := openTools(t).describeTable(t.Context(), "sessions")
	require.NoError(t, err)
	assert.NotEmpty(t, desc.Indexes, "the partial unique name index is reported")

	byName := make(map[string]columnInfo, len(desc.Columns))
	for _, col := range desc.Columns {
		byName[col.Name] = col
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			col, ok := byName[tt.column]
			require.True(t, ok)

			assert.Equal(t, tt.wantNull, col.Nullable)
			assert.Equal(t, tt.wantPrimary, col.PrimaryKey)
			if tt.wantDefault != "" {
				assert.Equal(t, tt.wantDefault, col.Default)
			}
		})
	}
}

func TestDescribeTableRejectsBadNames(t *testing.T) {
	tests := []struct {
		name    string
		table   string
		wantErr string
	}{
		{
			name:    "a name carrying a second statement",
			table:   "sessions; DROP TABLE sessions",
			wantErr: "invalid table name",
		},
		{
			name:    "a name that is not an identifier at all",
			table:   "no spaces allowed",
			wantErr: "invalid table name",
		},
		{
			name:    "a well-formed name for a table nobody has",
			table:   "nope",
			wantErr: "not found",
		},
	}

	tools := openTools(t)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tools.describeTable(t.Context(), tt.table)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}
