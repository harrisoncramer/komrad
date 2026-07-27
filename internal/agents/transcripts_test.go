package agents

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	agentstore "claude-agents/internal/store/agent"
	"claude-agents/internal/store/transcript"

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

// TestTranscriptSyncer covers a sync pass end to end. What lands in the store,
// and how a second pass treats a log that has moved on since the first.
func TestTranscriptSyncer(t *testing.T) {
	tests := []struct {
		name string
		// log is the JSONL on disk before the first sync. Empty writes no file at
		// all, standing in for an agent whose log has not appeared yet.
		log string
		// wantFirst, when set, is the content expected after the first sync, before
		// the log changes underneath it.
		wantFirst string
		// appended and rewritten change the log between the two syncs. Setting
		// either runs a second sync. Leaving both empty means one sync.
		appended  string
		rewritten string
		want      string
		// searchFor, when set, must find this agent's transcript by trigram.
		searchFor string
	}{
		{
			name: "a log is flattened into a transcript and indexed for search",
			log: `{"type":"user","message":{"content":"how do I use trigram search"}}` + "\n" +
				`{"type":"assistant","message":{"content":[{"type":"text","text":"use fts5"}]}}` + "\n",
			want:      "how do I use trigram search\n\nuse fts5",
			searchFor: "trigram",
		},
		{
			name:      "a second pass folds in only what was appended",
			log:       `{"type":"user","message":{"content":"first turn"}}` + "\n",
			wantFirst: "first turn",
			appended:  `{"type":"user","message":{"content":"second turn"}}` + "\n",
			want:      "first turn\n\nsecond turn",
		},
		{
			name: "a half-written line waits for its newline",
			log: `{"type":"user","message":{"content":"complete"}}` + "\n" +
				`{"type":"user","message":{"conte`,
			wantFirst: "complete",
			rewritten: `{"type":"user","message":{"content":"complete"}}` + "\n" +
				`{"type":"user","message":{"content":"rest"}}` + "\n",
			want: "complete\n\nrest",
		},
		{
			name:      "a log that shrank is reindexed from scratch, not appended to",
			log:       `{"type":"user","message":{"content":"original long conversation here"}}` + "\n",
			rewritten: `{"type":"user","message":{"content":"new"}}` + "\n",
			want:      "new",
		},
		{
			name: "an agent whose log is not on disk is skipped, not an error",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			ctx := context.Background()
			projects := t.TempDir()

			agent := seedWithClaudeID(t, st, testClaudeID)

			var path string
			if tt.log != "" {
				path = writeLog(t, projects, tt.log)
			}

			syncer := NewTranscriptSyncer(st.Agents, st.Transcripts, projects)
			require.NoError(t, syncer.Sync(ctx))

			first, err := st.GetTranscript(ctx, agent.ID)
			require.NoError(t, err)
			if tt.wantFirst != "" {
				assert.Equal(t, tt.wantFirst, first.Content)
			}

			if tt.appended != "" {
				appendToLog(t, path, tt.appended)
			}
			if tt.rewritten != "" {
				require.NoError(t, os.WriteFile(path, []byte(tt.rewritten), 0o644))
			}
			if tt.appended != "" || tt.rewritten != "" {
				require.NoError(t, syncer.Sync(ctx))
			}

			tr, err := st.GetTranscript(ctx, agent.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.want, tr.Content)

			if tt.want == "" {
				assert.Equal(t, transcript.Transcript{}, tr)
				return
			}
			assert.Positive(t, tr.SyncedOffset)

			if tt.appended != "" {
				assert.Greater(t, tr.SyncedOffset, first.SyncedOffset, "the offset advances with the log")
			}

			if tt.searchFor != "" {
				hits, err := st.Transcripts.Search(ctx, transcript.SearchParams{Query: tt.searchFor, Limit: 10})
				require.NoError(t, err)
				require.Len(t, hits, 1)
				assert.Equal(t, agent.ID, hits[0].AgentID)
			}
		})
	}
}

// testClaudeID is the claude session id every test log is keyed to. The store id
// is generated on insert, so this only needs to be stable within a test.
const testClaudeID = "claude-abc"

// seedWithClaudeID inserts an agent carrying testClaudeID and returns it with its
// generated store id, so a transcript can be keyed to the right row.
func seedWithClaudeID(t *testing.T, st *testStore, claudeID string) agentstore.Agent {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, st.Upsert(ctx, agentstore.Agent{Name: "agent", State: agentstore.StateNew, ClaudeSessionID: claudeID}))
	got, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NotNil(t, got)

	return *got
}

// writeLog writes an agent's JSONL log under a project subdirectory of the
// given projects dir, the way Claude Code lays them out.
func writeLog(t *testing.T, projectsDir, body string) string {
	t.Helper()
	dir := filepath.Join(projectsDir, "-some-project")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, testClaudeID+".jsonl")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))

	return path
}

// appendToLog adds to an existing log the way a running agent does, leaving
// what is already there byte for byte.
func appendToLog(t *testing.T, path, body string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	require.NoError(t, err)
	_, err = f.WriteString(body)
	require.NoError(t, err)
	require.NoError(t, f.Close())
}
