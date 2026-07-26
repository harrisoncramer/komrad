package mcp

import (
	"context"
	"strings"
	"testing"

	"claude-agents/internal/pgtest"
	"claude-agents/internal/store"

	"claude-agents/internal/store/session"
	"claude-agents/internal/store/transcript"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// openTools builds the tools over a real migrated database opened read-only, so
// the schema under test is the one the app actually runs.
func openTools(t *testing.T) *dbTools {
	t.Helper()

	url := pgtest.URL(t)
	pool, err := store.Open(t.Context(), url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })

	chats := session.New(pool.DB())
	transcripts := transcript.New(pool.DB())

	require.NoError(t, chats.Upsert(context.Background(), session.Agent{
		Name:  "auth",
		Pane:  "%9",
		Cwd:   "/repo",
		State: session.StateWaiting,
	}))

	agents, err := chats.List(context.Background())
	require.NoError(t, err)
	require.Len(t, agents, 1)
	require.NoError(t, transcripts.Upsert(context.Background(), transcript.UpsertParams{SessionID: agents[0].ID, Content: "we should update the parser", Offset: 0}))

	readOnly, err := store.OpenReadOnly(t.Context(), url)
	require.NoError(t, err)
	t.Cleanup(readOnly.Close)

	return &dbTools{db: readOnly}
}

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
			sql:     "INSERT INTO sessions (name) VALUES ('a')",
			wantErr: true,
		},
		{
			name:    "update",
			sql:     "UPDATE sessions SET state = 'done'",
			wantErr: true,
		},
		{
			name:    "delete",
			sql:     "DELETE FROM sessions",
			wantErr: true,
		},
		{
			name:    "drop",
			sql:     "DROP TABLE sessions",
			wantErr: true,
		},
		{
			name:    "pragma",
			sql:     "PRAGMA table_info(sessions)",
			wantErr: true,
		},
		{
			name:    "attach",
			sql:     "SELECT 1 FROM sessions; ATTACH DATABASE 'x' AS y",
			wantErr: true,
		},
		{
			name:    "multi statement",
			sql:     "SELECT 1; SELECT 2",
			wantErr: true,
		},
		{
			name:    "writing cte",
			sql:     "WITH x AS (UPDATE sessions SET state = 'z' RETURNING id) SELECT * FROM x",
			wantErr: true,
		},
		{
			name:    "not a select",
			sql:     "EXPLAIN SELECT 1",
			wantErr: true,
		},
		{
			name:    "statement hidden behind a line comment",
			sql:     "SELECT 1 -- ok\n; DROP TABLE sessions",
			wantErr: true,
		},
		{
			name:    "write after a literal that looks like it swallows it",
			sql:     "SELECT 'a' UNION SELECT 1; DELETE FROM sessions",
			wantErr: true,
		},
		{
			name:    "unterminated literal",
			sql:     "SELECT * FROM sessions WHERE name = 'auth",
			wantErr: true,
		},
		{
			name:    "unterminated block comment",
			sql:     "SELECT 1 /* DELETE FROM sessions",
			wantErr: true,
		},
		{
			name: "select",
			sql:  "SELECT name FROM sessions",
			want: "SELECT name FROM sessions",
		},
		{
			name: "keyword inside a literal is a search, not a write",
			sql:  "SELECT session_id FROM session_transcripts WHERE content LIKE '%update%'",
			want: "SELECT session_id FROM session_transcripts WHERE content LIKE '%update%'",
		},
		{
			name: "escaped quote inside a literal does not end it",
			sql:  "SELECT session_id FROM session_transcripts WHERE content LIKE '%don''t drop it%'",
			want: "SELECT session_id FROM session_transcripts WHERE content LIKE '%don''t drop it%'",
		},
		{
			name: "semicolon inside a literal is not a second statement",
			sql:  "SELECT session_id FROM session_transcripts WHERE content LIKE '%a; b%'",
			want: "SELECT session_id FROM session_transcripts WHERE content LIKE '%a; b%'",
		},
		{
			name: "keyword as a quoted identifier",
			sql:  `SELECT "update" FROM sessions`,
			want: `SELECT "update" FROM sessions`,
		},
		{
			name: "leading comment before the select",
			sql:  "/* which chats are waiting */ SELECT name FROM sessions",
			want: "/* which chats are waiting */ SELECT name FROM sessions",
		},
		{
			name: "trailing line comment",
			sql:  "SELECT name FROM sessions -- drop the rest",
			want: "SELECT name FROM sessions -- drop the rest",
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

func TestQueryReturnsRows(t *testing.T) {
	res, err := openTools(t).query(context.Background(), "SELECT name, state FROM sessions")
	require.NoError(t, err)

	assert.Equal(t, []string{"name", "state"}, res.Columns)
	require.Len(t, res.Rows, 1)
	assert.Equal(t, "auth", res.Rows[0]["name"])
	assert.Equal(t, session.StateWaiting, res.Rows[0]["state"])
	assert.False(t, res.Truncated)
}

// TestQuerySearchesTranscripts confirms a transcript search for a word that is
// also a write verb runs and returns its rows, which is the whole point of
// scanning only the executable text.
func TestQuerySearchesTranscripts(t *testing.T) {
	const search = `SELECT session_id FROM session_transcripts WHERE content LIKE '%update%'`

	res, err := openTools(t).query(context.Background(), search)
	require.NoError(t, err)
	assert.Equal(t, 1, res.RowCount)
}

// TestQueryCapsRows confirms a result larger than the cap is cut off and flagged
// rather than returned whole.
func TestQueryCapsRows(t *testing.T) {
	const generate = `
WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 1500)
SELECT i FROM n`

	res, err := openTools(t).query(context.Background(), generate)
	require.NoError(t, err)

	assert.Equal(t, maxRows, res.RowCount)
	assert.True(t, res.Truncated)
}

// TestQueryRejectsWrite confirms the connection itself refuses a write, so the
// keyword check is not the only thing standing between a model and the database.
// The pool defaults every session to a read-only transaction, so this fails in
// Postgres rather than in the parsing above it.
func TestQueryRejectsWrite(t *testing.T) {
	tools := openTools(t)

	_, err := tools.db.Exec(context.Background(), "DELETE FROM sessions")
	require.ErrorContains(t, err, "read-only transaction")

	var count int
	require.NoError(t, tools.db.QueryRow(context.Background(), "SELECT COUNT(*) FROM sessions").Scan(&count))
	assert.Equal(t, 1, count)
}

func TestCapString(t *testing.T) {
	assert.Equal(t, "short", capString("short"))

	long := capString(strings.Repeat("x", maxCellChars+10))
	assert.True(t, strings.HasSuffix(long, "…(truncated)"))
	assert.Len(t, long, maxCellChars+len("…(truncated)"))
}

func TestNormalizeValue(t *testing.T) {
	assert.Nil(t, normalizeValue(nil))
	assert.Equal(t, "blob", normalizeValue([]byte("blob")))
	assert.Equal(t, int64(7), normalizeValue(int64(7)))
}
