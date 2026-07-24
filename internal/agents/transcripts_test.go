package agents

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"claude-agents/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFlattenTranscript(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "string user prompt is kept",
			in:   `{"type":"user","message":{"content":"hello there"}}`,
			want: "hello there",
		},
		{
			name: "assistant text blocks are kept, thinking and tool_use dropped",
			in: `{"type":"assistant","message":{"content":[` +
				`{"type":"thinking","thinking":"secret reasoning"},` +
				`{"type":"text","text":"visible answer"},` +
				`{"type":"tool_use","name":"Bash","input":{}}` +
				`]}}`,
			want: "visible answer",
		},
		{
			name: "user tool_result blocks are dropped",
			in:   `{"type":"user","message":{"content":[{"type":"tool_result","content":"exit 0"}]}}`,
			want: "",
		},
		{
			name: "meta lines are skipped",
			in:   `{"type":"user","isMeta":true,"message":{"content":"injected context"}}`,
			want: "",
		},
		{
			name: "non-conversation types are skipped",
			in: `{"type":"mode","mode":"normal"}` + "\n" +
				`{"type":"file-history-snapshot"}` + "\n" +
				`{"type":"system","content":"hook fired"}`,
			want: "",
		},
		{
			name: "malformed lines are ignored",
			in:   "not json\n" + `{"type":"user","message":{"content":"survives"}}`,
			want: "survives",
		},
		{
			name: "turns are joined in order, blank-line separated",
			in: `{"type":"user","message":{"content":"question"}}` + "\n" +
				`{"type":"assistant","message":{"content":[{"type":"text","text":"answer"}]}}`,
			want: "question\n\nanswer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, flattenTranscript([]byte(tt.in)))
		})
	}
}

// testClaudeID is the claude session id every test log is keyed to. The store id
// is generated on insert, so this only needs to be stable within a test.
const testClaudeID = "claude-abc"

// seedWithClaudeID inserts an agent carrying testClaudeID and returns it with its
// generated store id, so a transcript can be keyed to the right row.
func seedWithClaudeID(t *testing.T, st *store.Store, claudeID string) store.Agent {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, st.Upsert(ctx, store.Agent{Name: "chat", State: store.StateNew, ClaudeSessionID: claudeID}))
	got, err := st.Get(ctx, "chat")
	require.NoError(t, err)
	require.NotNil(t, got)

	return *got
}

// writeLog writes a session's JSONL log under a project subdirectory of the
// given projects dir, the way Claude Code lays them out.
func writeLog(t *testing.T, projectsDir, body string) string {
	t.Helper()
	dir := filepath.Join(projectsDir, "-some-project")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, testClaudeID+".jsonl")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))

	return path
}

func TestTranscriptSyncerIndexesLog(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	projects := t.TempDir()

	agent := seedWithClaudeID(t, st, testClaudeID)
	writeLog(t, projects,
		`{"type":"user","message":{"content":"how do I use trigram search"}}`+"\n"+
			`{"type":"assistant","message":{"content":[{"type":"text","text":"use fts5"}]}}`+"\n")

	syncer := NewTranscriptSyncer(st, projects)
	require.NoError(t, syncer.Sync(ctx))

	tr, err := st.GetTranscript(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, "how do I use trigram search\n\nuse fts5", tr.Content)
	assert.Positive(t, tr.SyncedOffset)

	hits, err := st.SearchTranscripts(ctx, "trigram", 10)
	require.NoError(t, err)
	require.Len(t, hits, 1)
	assert.Equal(t, agent.ID, hits[0].SessionID)
}

func TestTranscriptSyncerIsIncremental(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	projects := t.TempDir()

	agent := seedWithClaudeID(t, st, testClaudeID)
	path := writeLog(t, projects, `{"type":"user","message":{"content":"first turn"}}`+"\n")

	syncer := NewTranscriptSyncer(st, projects)
	require.NoError(t, syncer.Sync(ctx))
	first, err := st.GetTranscript(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, "first turn", first.Content)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	require.NoError(t, err)
	_, err = f.WriteString(`{"type":"user","message":{"content":"second turn"}}` + "\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	require.NoError(t, syncer.Sync(ctx))
	second, err := st.GetTranscript(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, "first turn\n\nsecond turn", second.Content, "only the appended turn is folded in")
	assert.Greater(t, second.SyncedOffset, first.SyncedOffset)
}

func TestTranscriptSyncerLeavesPartialLine(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	projects := t.TempDir()

	agent := seedWithClaudeID(t, st, testClaudeID)
	// A complete line followed by a half-written one with no trailing newline.
	path := writeLog(t, projects,
		`{"type":"user","message":{"content":"complete"}}`+"\n"+`{"type":"user","message":{"conte`)

	syncer := NewTranscriptSyncer(st, projects)
	require.NoError(t, syncer.Sync(ctx))
	tr, err := st.GetTranscript(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, "complete", tr.Content, "the partial line is not parsed yet")

	// Finish the partial line, and the next sync picks it up.
	require.NoError(t, os.WriteFile(path,
		[]byte(`{"type":"user","message":{"content":"complete"}}`+"\n"+`{"type":"user","message":{"content":"rest"}}`+"\n"), 0o644))
	require.NoError(t, syncer.Sync(ctx))
	tr, err = st.GetTranscript(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, "complete\n\nrest", tr.Content)
}

func TestTranscriptSyncerReindexesOnShrink(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	projects := t.TempDir()

	agent := seedWithClaudeID(t, st, testClaudeID)
	path := writeLog(t, projects,
		`{"type":"user","message":{"content":"original long conversation here"}}`+"\n")

	syncer := NewTranscriptSyncer(st, projects)
	require.NoError(t, syncer.Sync(ctx))

	// Rewrite the log shorter than the synced offset, as a --resume rewrite would.
	require.NoError(t, os.WriteFile(path, []byte(`{"type":"user","message":{"content":"new"}}`+"\n"), 0o644))
	require.NoError(t, syncer.Sync(ctx))

	tr, err := st.GetTranscript(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, "new", tr.Content, "a shrunk log is reindexed from scratch, not appended to")
}

func TestTranscriptSyncerSkipsMissingLog(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	projects := t.TempDir()

	agent := seedWithClaudeID(t, st, "claude-nolog")
	syncer := NewTranscriptSyncer(st, projects)
	require.NoError(t, syncer.Sync(ctx), "a session whose log is not on disk is skipped, not an error")

	tr, err := st.GetTranscript(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, store.Transcript{}, tr)
}
