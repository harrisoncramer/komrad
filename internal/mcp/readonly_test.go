package mcp

import (
	"strings"
	"testing"

	"weave/internal/store"
	"weave/internal/storetest"

	agentstore "weave/internal/store/agent"
	transcriptstore "weave/internal/store/transcript"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openTools(t *testing.T) *dbTools {
	t.Helper()

	st := storetest.Open(t)

	agent := st.SeedAgent(t, agentstore.Agent{
		Name:  "auth",
		Pane:  "%9",
		Cwd:   "/repo",
		State: agentstore.StateWaiting,
	})
	require.NoError(t, st.Transcripts.Upsert(t.Context(), transcriptstore.UpsertParams{
		AgentID: agent.ID,
		Content: "we should update the parser",
		Offset:  0,
	}))

	readOnly, err := store.OpenReadOnly(t.Context(), st.ReadOnlyURL)
	require.NoError(t, err)
	t.Cleanup(readOnly.Close)

	return &dbTools{db: readOnly}
}

// TestValidateReadOnly pins which statements the read-only endpoint accepts, so a write or a
// second statement is refused before it reaches the database.
func TestValidateReadOnly(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		wantErr bool
		want    string
	}{
		{
			name:    "empty",
			sql:     "   ",
			wantErr: true,
		},
		{
			name:    "insert",
			sql:     "INSERT INTO agents (name) VALUES ('a')",
			wantErr: true,
		},
		{
			name:    "update",
			sql:     "UPDATE agents SET state = 'idle'",
			wantErr: true,
		},
		{
			name:    "delete",
			sql:     "DELETE FROM agents",
			wantErr: true,
		},
		{
			name:    "drop",
			sql:     "DROP TABLE agents",
			wantErr: true,
		},
		{
			name:    "pragma",
			sql:     "PRAGMA table_info(sessions)",
			wantErr: true,
		},
		{
			name:    "attach",
			sql:     "SELECT 1 FROM agents; ATTACH DATABASE 'x' AS y",
			wantErr: true,
		},
		{
			name:    "multi statement",
			sql:     "SELECT 1; SELECT 2",
			wantErr: true,
		},
		{
			name:    "writing cte",
			sql:     "WITH x AS (UPDATE agents SET state = 'z' RETURNING id) SELECT * FROM x",
			wantErr: true,
		},
		{
			name:    "not a select",
			sql:     "EXPLAIN SELECT 1",
			wantErr: true,
		},
		{
			name:    "statement hidden behind a line comment",
			sql:     "SELECT 1 -- ok\n; DROP TABLE agents",
			wantErr: true,
		},
		{
			name:    "write after a literal that looks like it swallows it",
			sql:     "SELECT 'a' UNION SELECT 1; DELETE FROM agents",
			wantErr: true,
		},
		{
			name:    "unterminated literal",
			sql:     "SELECT * FROM agents WHERE name = 'auth",
			wantErr: true,
		},
		{
			name:    "unterminated block comment",
			sql:     "SELECT 1 /* DELETE FROM agents",
			wantErr: true,
		},
		{
			name: "select",
			sql:  "SELECT name FROM agents",
			want: "SELECT name FROM agents",
		},
		{
			name: "keyword inside a literal is a search, not a write",
			sql:  "SELECT agent_id FROM agent_transcripts WHERE content LIKE '%update%'",
			want: "SELECT agent_id FROM agent_transcripts WHERE content LIKE '%update%'",
		},
		{
			name: "escaped quote inside a literal does not end it",
			sql:  "SELECT agent_id FROM agent_transcripts WHERE content LIKE '%don''t drop it%'",
			want: "SELECT agent_id FROM agent_transcripts WHERE content LIKE '%don''t drop it%'",
		},
		{
			name: "semicolon inside a literal is not a second statement",
			sql:  "SELECT agent_id FROM agent_transcripts WHERE content LIKE '%a; b%'",
			want: "SELECT agent_id FROM agent_transcripts WHERE content LIKE '%a; b%'",
		},
		{
			name: "keyword as a quoted identifier",
			sql:  `SELECT "update" FROM agents`,
			want: `SELECT "update" FROM agents`,
		},
		{
			name: "leading comment before the select",
			sql:  "/* which agents are waiting */ SELECT name FROM agents",
			want: "/* which agents are waiting */ SELECT name FROM agents",
		},
		{
			name: "trailing line comment",
			sql:  "SELECT name FROM agents -- drop the rest",
			want: "SELECT name FROM agents -- drop the rest",
		},
		{
			name: "trailing semicolon trimmed",
			sql:  "SELECT 1;",
			want: "SELECT 1",
		},
		{
			name: "with cte read only",
			sql:  "WITH x AS (SELECT 1 AS n) SELECT n FROM x",
			want: "WITH x AS (SELECT 1 AS n) SELECT n FROM x",
		},
		{
			name: "lowercase select",
			sql:  "select 1",
			want: "select 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateReadOnly(tt.sql)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestQuery covers a truncated result saying so, since the row cap is the one thing a caller
// cannot see coming.
func TestQuery(t *testing.T) {
	const overCap = `
WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 1500)
SELECT i FROM n`

	tests := []struct {
		name          string
		sql           string
		wantCols      []string
		wantRows      int
		wantTruncated bool
		check         func(t *testing.T, res *queryResult)
	}{
		{
			name:     "a query matching nothing returns no rows rather than an error",
			sql:      "SELECT name FROM agents WHERE name = 'nobody'",
			wantCols: []string{"name"},
			wantRows: 0,
		},
		{
			name:     "the columns asked for come back in order",
			sql:      "SELECT name, state FROM agents",
			wantCols: []string{"name", "state"},
			wantRows: 1,
			check: func(t *testing.T, res *queryResult) {
				assert.Equal(t, "auth", res.Rows[0]["name"])
				assert.Equal(t, string(agentstore.StateWaiting), res.Rows[0]["state"])
			},
		},
		{
			name:     "a word that is also a write verb searches rather than being refused",
			sql:      `SELECT agent_id FROM agent_transcripts WHERE content LIKE '%update%'`,
			wantCols: []string{"agent_id"},
			wantRows: 1,
		},
		{
			name:          "a result past the cap is cut off and flagged",
			sql:           overCap,
			wantCols:      []string{"i"},
			wantRows:      maxRows,
			wantTruncated: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := openTools(t).query(t.Context(), tt.sql)
			require.NoError(t, err)

			assert.Equal(t, tt.wantCols, res.Columns)
			assert.Equal(t, tt.wantRows, res.RowCount)
			require.Len(t, res.Rows, tt.wantRows)
			assert.Equal(t, tt.wantTruncated, res.Truncated)

			if tt.check != nil {
				tt.check(t, res)
			}
		})
	}
}

// TestQueryRejectsWrite confirms the pool's read-only session refuses a write in Postgres, so
// the keyword check is not the only thing standing between a model and the database.
func TestQueryRejectsWrite(t *testing.T) {
	tests := []struct {
		name string
		sql  string
	}{
		{
			name: "a delete",
			sql:  "DELETE FROM agents",
		},
		{
			name: "an update",
			sql:  "UPDATE agents SET state = 'idle'",
		},
	}

	tools := openTools(t)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tools.db.Exec(t.Context(), tt.sql)
			require.ErrorContains(t, err, "read-only transaction")

			var count int
			require.NoError(t, tools.db.QueryRow(t.Context(), "SELECT COUNT(*) FROM agents").Scan(&count))
			assert.Equal(t, 1, count, "the row is still there")
		})
	}
}

// TestCapString covers the per-cell limit, which keeps one wide column from
// filling a response on its own.
func TestCapString(t *testing.T) {
	tests := []struct {
		name          string
		in            string
		wantTruncated bool
	}{
		{
			name: "a short value is passed through",
			in:   "short",
		},
		{
			name:          "an oversized value is cut and marked",
			in:            strings.Repeat("x", maxCellChars+10),
			wantTruncated: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := capString(tt.in)

			if !tt.wantTruncated {
				assert.Equal(t, tt.in, got)
				return
			}

			assert.True(t, strings.HasSuffix(got, "…(truncated)"))
			assert.Len(t, got, maxCellChars+len("…(truncated)"))
		})
	}
}

// TestNormalizeValue covers the driver values that are not JSON-friendly as
// they come back.
func TestNormalizeValue(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want any
	}{
		{
			name: "a null column",
			in:   nil,
			want: nil,
		},
		{
			name: "a blob becomes text",
			in:   []byte("blob"),
			want: "blob",
		},
		{
			name: "a number is left alone",
			in:   int64(7),
			want: int64(7),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeValue(tt.in))
		})
	}
}
