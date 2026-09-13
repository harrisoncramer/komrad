package agents

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"weave/internal/harness/claude"
	"weave/internal/harness/codex"
	agentstore "weave/internal/store/agent"
	transcriptstore "weave/internal/store/transcript"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTranscriptSyncer covers a sync pass end to end, including how a second pass treats a
// log that has moved on since the first.
func TestTranscriptSyncer(t *testing.T) {
	tests := []struct {
		name      string
		log       string
		wantFirst string
		appended  string
		rewritten string
		want      string
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
			name: "a turn carrying a NUL is stored without it",
			log:  `{"type":"user","message":{"content":"before\u0000after"}}` + "\n",
			want: "beforeafter",
		},
		{
			name: "an agent whose log is not on disk is skipped, not an error",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			ctx := t.Context()
			projects := t.TempDir()

			agent := seedWithSessionID(t, st, testSessionID, "agent")

			var path string
			if tt.log != "" {
				path = writeLog(t, projects, tt.log)
			}

			syncer := NewTranscriptSyncer(NewTranscriptSyncerParams{
				Agents:      st.Agents,
				Transcripts: st.Transcripts,
				Harnesses:   mustRegistry(t, claude.NewHarness(claude.NewHarnessParams{ProjectsDir: projects})),
			})
			require.NoError(t, syncer.Sync(ctx))

			first, err := st.GetTranscript(ctx, agent.ID)
			require.NoError(t, err)
			if tt.wantFirst != "" {
				assert.Equal(t, tt.wantFirst, first)
			}
			firstOffset := syncedOffset(t, st, agent.ID, testSessionID)

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
			assert.Equal(t, tt.want, tr)

			if tt.want == "" {
				return
			}

			offset := syncedOffset(t, st, agent.ID, testSessionID)
			assert.Positive(t, offset)

			if tt.appended != "" {
				assert.Greater(t, offset, firstOffset, "the offset advances with the log")
			}

			if tt.searchFor != "" {
				found, err := st.Transcripts.Search(ctx, transcriptstore.SearchParams{Query: tt.searchFor, Limit: 10})
				require.NoError(t, err)
				require.Len(t, found.Matches, 1)
				assert.Equal(t, agent.ID, found.Matches[0].AgentID)
			}
		})
	}
}

// TestSyncSurvivesOneUnsyncableAgent pins that one agent's bad log does not strand every older
// agent behind it.
func TestSyncSurvivesOneUnsyncableAgent(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()
	projects := t.TempDir()

	readable := seedWithSessionID(t, st, "claude-readable", "readable")
	unsyncable := seedWithSessionID(t, st, "claude-unsyncable", "unsyncable")

	writeLogFor(t, projects, "claude-readable", `{"type":"user","message":{"content":"a turn worth keeping"}}`+"\n")
	writeLogFor(t, projects, "claude-unsyncable", `{"type":"user","message":{"content":"never accepted"}}`+"\n")

	store := failingTranscripts{transcriptReadWriter: st.Transcripts, failFor: unsyncable.ID}
	syncer := NewTranscriptSyncer(NewTranscriptSyncerParams{
		Agents:      st.Agents,
		Transcripts: store,
		Harnesses:   mustRegistry(t, claude.NewHarness(claude.NewHarnessParams{ProjectsDir: projects})),
	})

	err := syncer.Sync(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsyncable", "the pass names the agent it could not fold in")

	stranded, err := st.GetTranscript(ctx, readable.ID)
	require.NoError(t, err)
	assert.Equal(t, "a turn worth keeping", stranded, "the rest of the pass is synced anyway")

	empty, err := st.GetTranscript(ctx, unsyncable.ID)
	require.NoError(t, err)
	assert.Empty(t, empty, "the agent that failed has nothing stored")
}

type failingTranscripts struct {
	transcriptReadWriter
	failFor string
}

func (f failingTranscripts) AppendTurns(ctx context.Context, params transcriptstore.AppendTurnsParams) error {
	if params.AgentID == f.failFor {
		return errors.New("this log will never be accepted")
	}

	return f.transcriptReadWriter.AppendTurns(ctx, params)
}

// syncedOffset reads how far a session's log has been copied.
func syncedOffset(t *testing.T, st *testStore, agentID, sessionID string) int64 {
	t.Helper()
	session, err := st.Transcripts.OpenSession(t.Context(), transcriptstore.OpenSessionParams{
		AgentID:   agentID,
		SessionID: sessionID,
	})
	require.NoError(t, err)

	return session.SyncedOffset
}

const testSessionID = "claude-abc"

func seedWithSessionID(t *testing.T, st *testStore, sessionID, name string) agentstore.Agent {
	t.Helper()
	ctx := t.Context()
	require.NoError(t, st.Create(ctx, agentstore.Agent{
		Name:      name,
		State:     agentstore.StateIdle,
		SessionID: sessionID,
	}))
	got, err := st.Get(ctx, name)
	require.NoError(t, err)
	require.NotNil(t, got)

	return *got
}

func writeLog(t *testing.T, projectsDir, body string) string {
	t.Helper()

	return writeLogFor(t, projectsDir, testSessionID, body)
}

func writeLogFor(t *testing.T, projectsDir, sessionID, body string) string {
	t.Helper()
	dir := filepath.Join(projectsDir, "-some-project")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, sessionID+".jsonl")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))

	return path
}

func appendToLog(t *testing.T, path, body string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	require.NoError(t, err)
	_, err = f.WriteString(body)
	require.NoError(t, err)
	require.NoError(t, f.Close())
}

// TestSyncReportsAnUnknownHarness pins that an agent nothing can index is reported rather than
// skipped.
func TestSyncReportsAnUnknownHarness(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, agentstore.Agent{
		Name:      "orphan",
		State:     agentstore.StateIdle,
		Harness:   "gemini",
		SessionID: testSessionID,
	}))

	syncer := NewTranscriptSyncer(NewTranscriptSyncerParams{
		Agents:      st.Agents,
		Transcripts: st.Transcripts,
		Harnesses:   mustRegistry(t, claude.NewHarness(claude.NewHarnessParams{ProjectsDir: t.TempDir()})),
	})

	err := syncer.Sync(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "orphan", "the pass names the agent it could not read")
	assert.Contains(t, err.Error(), "gemini", "and the harness it is waiting on")
}

// TestSyncAdoptsSessions pins that the syncer asks codex afterwards which conversation it
// started.
func TestSyncAdoptsSessions(t *testing.T) {
	tests := []struct {
		name  string
		found string
		held  string
		want  string
	}{
		{
			name: "nothing to adopt yet, which is every pass before the CLI writes a rollout",
		},
		{
			name:  "an agent already holding a conversation is not asked again",
			found: "sess-other",
			held:  "sess-held",
			want:  "sess-held",
		},
		{
			name:  "the conversation the CLI started for this agent",
			found: "sess-mine",
			want:  "sess-mine",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			ctx := t.Context()

			require.NoError(t, st.Create(ctx, agentstore.Agent{
				Name: "adopter", State: agentstore.StateIdle, SessionID: tt.held,
			}))

			syncer := NewTranscriptSyncer(NewTranscriptSyncerParams{
				Agents:      st.Agents,
				Transcripts: st.Transcripts,
				Harnesses:   fakeHarnesses{harness: &fakeHarness{found: tt.found}},
			})
			require.NoError(t, syncer.Sync(ctx))

			adopter, err := st.Get(ctx, "adopter")
			require.NoError(t, err)
			require.NotNil(t, adopter)
			assert.Equal(t, tt.want, adopter.SessionID)
		})
	}
}

// TestSyncAdoptedSessionIsIndexed pins that the transcript turning up under an adopted id is
// then read, driving the real codex harness over a real rollout.
func TestSyncAdoptedSessionIsIndexed(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()
	sessions := t.TempDir()
	cwd := t.TempDir()

	const sessionID = "019fb3e7-2cb6-7ab3-86a0-51a670a33140"

	require.NoError(t, st.Create(ctx, agentstore.Agent{
		Name: "adopter", State: agentstore.StateIdle, Harness: "codex", Cwd: cwd,
	}))
	created, err := st.Get(ctx, "adopter")
	require.NoError(t, err)
	require.NotNil(t, created)

	dir := filepath.Join(sessions, "2026", "07", "30")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	rollout := filepath.Join(dir, "rollout-2026-07-30T12-41-39-"+sessionID+".jsonl")
	require.NoError(t, os.WriteFile(rollout,
		[]byte(`{"type":"session_meta","payload":{"id":"`+sessionID+`","originator":"weave-`+created.ID+`"}}`+"\n"+
			`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"how do I use trigram search"}]}}`+"\n"),
		0o600))

	syncer := NewTranscriptSyncer(NewTranscriptSyncerParams{
		Agents:      st.Agents,
		Transcripts: st.Transcripts,
		Harnesses:   mustRegistry(t, codex.NewHarness(codex.NewHarnessParams{SessionsDir: sessions})),
	})

	require.NoError(t, syncer.Sync(ctx))

	adopted, err := st.Get(ctx, "adopter")
	require.NoError(t, err)
	require.NotNil(t, adopted)
	assert.Equal(t, sessionID, adopted.SessionID, "the id codex chose is recorded")

	tr, err := st.GetTranscript(ctx, adopted.ID)
	require.NoError(t, err)
	assert.Equal(t, "how do I use trigram search", tr)
}

// TestSyncStartsOverOnALogEmptiedBetweenPasses pins the pass that sees nothing but a restart,
// which is what a CLI that truncates its log before rewriting it leaves behind.
func TestSyncStartsOverOnALogEmptiedBetweenPasses(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()
	projects := t.TempDir()

	agent := seedWithSessionID(t, st, testSessionID, "agent")
	path := writeLog(t, projects, `{"type":"user","message":{"content":"the conversation that was"}}`+"\n")

	syncer := NewTranscriptSyncer(NewTranscriptSyncerParams{
		Agents:      st.Agents,
		Transcripts: st.Transcripts,
		Harnesses:   mustRegistry(t, claude.NewHarness(claude.NewHarnessParams{ProjectsDir: projects})),
	})
	require.NoError(t, syncer.Sync(ctx))

	before, err := st.GetTranscript(ctx, agent.ID)
	require.NoError(t, err)
	require.Equal(t, "the conversation that was", before)

	require.NoError(t, os.WriteFile(path, nil, 0o644))
	require.NoError(t, syncer.Sync(ctx))

	emptied, err := st.GetTranscript(ctx, agent.ID)
	require.NoError(t, err)
	assert.Empty(t, emptied, "the transcript of a log that is gone is gone with it")
	assert.Zero(t, syncedOffset(t, st, agent.ID, testSessionID), "and the next pass reads the new log from its start")

	require.NoError(t, os.WriteFile(path, []byte(
		`{"type":"user","message":{"content":"a longer conversation that replaced the first one"}}`+"\n",
	), 0o644))
	require.NoError(t, syncer.Sync(ctx))

	after, err := st.GetTranscript(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, "a longer conversation that replaced the first one", after)

	found, err := st.Transcripts.Search(ctx, transcriptstore.SearchParams{Query: "was", Limit: 10})
	require.NoError(t, err)
	assert.Empty(t, found.Matches, "and search no longer answers with the conversation that went")
}

// TestSyncKeepsEarlierConversations pins that a second conversation is copied alongside the
// first rather than replacing it, which is what a log read from its start used to do.
func TestSyncKeepsEarlierConversations(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()
	projects := t.TempDir()

	agent := seedWithSessionID(t, st, "claude-first", "agent")
	writeLogFor(t, projects, "claude-first", `{"type":"user","message":{"content":"what the first conversation said"}}`+"\n")

	syncer := NewTranscriptSyncer(NewTranscriptSyncerParams{
		Agents:      st.Agents,
		Transcripts: st.Transcripts,
		Harnesses:   mustRegistry(t, claude.NewHarness(claude.NewHarnessParams{ProjectsDir: projects})),
	})
	require.NoError(t, syncer.Sync(ctx))

	moveToSession(t, st, agent.ID, "claude-second")
	writeLogFor(t, projects, "claude-second", `{"type":"user","message":{"content":"what the second conversation said"}}`+"\n")
	require.NoError(t, syncer.Sync(ctx))

	stored, err := st.GetTranscript(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, "what the first conversation said\n\nwhat the second conversation said", stored,
		"the second conversation follows the first rather than replacing it")

	found, err := st.Transcripts.Search(ctx, transcriptstore.SearchParams{Query: "first", Limit: 10})
	require.NoError(t, err)
	require.Len(t, found.Matches, 1, "the conversation the agent has moved on from is still searchable")
	assert.Equal(t, agent.ID, found.Matches[0].AgentID)
}

// TestSyncStartsOverOnlyWithinOneConversation pins that a log read again from its start clears
// what that conversation held and nothing of the conversations before it.
func TestSyncStartsOverOnlyWithinOneConversation(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()
	projects := t.TempDir()

	agent := seedWithSessionID(t, st, "claude-first", "agent")
	writeLogFor(t, projects, "claude-first", `{"type":"user","message":{"content":"what the first conversation said"}}`+"\n")

	syncer := NewTranscriptSyncer(NewTranscriptSyncerParams{
		Agents:      st.Agents,
		Transcripts: st.Transcripts,
		Harnesses:   mustRegistry(t, claude.NewHarness(claude.NewHarnessParams{ProjectsDir: projects})),
	})
	require.NoError(t, syncer.Sync(ctx))

	moveToSession(t, st, agent.ID, "claude-second")
	second := writeLogFor(t, projects, "claude-second", `{"type":"user","message":{"content":"a long second conversation"}}`+"\n")
	require.NoError(t, syncer.Sync(ctx))

	require.NoError(t, os.WriteFile(second, []byte(`{"type":"user","message":{"content":"rewritten"}}`+"\n"), 0o644))
	require.NoError(t, syncer.Sync(ctx))

	stored, err := st.GetTranscript(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, "what the first conversation said\n\nrewritten", stored)
}

// moveToSession puts an agent on a second conversation, which the store's own setter refuses
// once one is recorded.
func moveToSession(t *testing.T, st *testStore, agentID, sessionID string) {
	t.Helper()
	_, err := st.Pool.GetRawPool().Exec(t.Context(), "UPDATE agents SET session_id = $1 WHERE id = $2", sessionID, agentID)
	require.NoError(t, err)
}
