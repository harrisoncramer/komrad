package agents

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"weave/internal/harness"
	"weave/internal/harness/claude"
	"weave/internal/harness/codex"
	agentstore "weave/internal/store/agent"
	transcriptstore "weave/internal/store/transcript"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAppendTurns covers the join and the offsets together.
func TestAppendTurns(t *testing.T) {
	tests := []struct {
		name     string
		existing string
		turns    []harness.Turn
		want     string
	}{
		{
			name:     "nothing new leaves the transcript alone",
			existing: "earlier",
			want:     "earlier",
		},
		{
			name:  "the first turns start the transcript",
			turns: []harness.Turn{{Role: "user", Content: "question"}, {Role: "assistant", Content: "answer"}},
			want:  "question\n\nanswer",
		},
		{
			name:     "later turns are separated from what is already there",
			existing: "earlier",
			turns:    []harness.Turn{{Role: "user", Content: "later"}},
			want:     "earlier\n\nlater",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content, indexed := appendTurns(tt.existing, tt.turns)
			assert.Equal(t, tt.want, content)
			require.Len(t, indexed, len(tt.turns))

			for i, got := range indexed {
				assert.Equal(t, tt.turns[i].Role, got.Role)
				assert.Equal(t, tt.turns[i].Content, got.Content)
				assert.Equal(t, got.Content, content[got.Offset:got.Offset+int64(len(got.Content))],
					"the offset locates the turn in the text it was appended to")
			}
		})
	}
}

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
				assert.Equal(t, transcriptstore.Transcript{}, tr)
				return
			}
			assert.Positive(t, tr.SyncedOffset)

			if tt.appended != "" {
				assert.Greater(t, tr.SyncedOffset, first.SyncedOffset, "the offset advances with the log")
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
	assert.Equal(t, "a turn worth keeping", stranded.Content, "the rest of the pass is synced anyway")

	empty, err := st.GetTranscript(ctx, unsyncable.ID)
	require.NoError(t, err)
	assert.Empty(t, empty.Content, "the agent that failed has nothing stored")
}

type failingTranscripts struct {
	transcriptReadWriter
	failFor string
}

func (f failingTranscripts) Upsert(ctx context.Context, params transcriptstore.UpsertParams) error {
	if params.AgentID == f.failFor {
		return errors.New("this log will never be accepted")
	}

	return f.transcriptReadWriter.Upsert(ctx, params)
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
	assert.Equal(t, "how do I use trigram search", tr.Content)
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
	require.Equal(t, "the conversation that was", before.Content)

	require.NoError(t, os.WriteFile(path, nil, 0o644))
	require.NoError(t, syncer.Sync(ctx))

	emptied, err := st.GetTranscript(ctx, agent.ID)
	require.NoError(t, err)
	assert.Empty(t, emptied.Content, "the transcript of a log that is gone is gone with it")
	assert.Zero(t, emptied.SyncedOffset, "and the next pass reads the new log from its start")

	require.NoError(t, os.WriteFile(path, []byte(
		`{"type":"user","message":{"content":"a longer conversation that replaced the first one"}}`+"\n",
	), 0o644))
	require.NoError(t, syncer.Sync(ctx))

	after, err := st.GetTranscript(ctx, agent.ID)
	require.NoError(t, err)
	assert.Equal(t, "a longer conversation that replaced the first one", after.Content)

	found, err := st.Transcripts.Search(ctx, transcriptstore.SearchParams{Query: "was", Limit: 10})
	require.NoError(t, err)
	assert.Empty(t, found.Matches, "and search no longer answers with the conversation that went")
}
