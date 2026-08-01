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
	"weave/internal/store/transcript"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAppendTurns covers the join and the offsets together, since the offset a turn
// records is only right if it points at that turn inside the text the same call
// built.
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
			ctx := context.Background()
			projects := t.TempDir()

			agent := seedWithSessionID(t, st, testSessionID, "agent")

			var path string
			if tt.log != "" {
				path = writeLog(t, projects, tt.log)
			}

			syncer := NewTranscriptSyncer(TranscriptSyncerParams{
				Agents:      st.Agents,
				Sessions:    st.Agents,
				Transcripts: st.Transcripts,
				Harnesses:   mustRegistry(t, claude.New(claude.Options{ProjectsDir: projects})),
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

// TestSyncSurvivesOneUnsyncableAgent covers what a pass does with an agent it
// cannot fold in.
//
// The syncer walks every agent newest first on a timer, and the failures that
// reach it are properties of one agent's log rather than of the machine, so the
// same agent fails on every pass. Giving up at it would leave every older agent
// behind it never indexed again. The failure is injected here because the store
// accepts everything the flattener now produces, and this is about the walk
// rather than about any one reason a write is refused.
func TestSyncSurvivesOneUnsyncableAgent(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	projects := t.TempDir()

	// Seeded oldest first, so the failing agent is the newest and the walk reaches
	// it before the one it would strand.
	readable := seedWithSessionID(t, st, "claude-readable", "readable")
	unsyncable := seedWithSessionID(t, st, "claude-unsyncable", "unsyncable")

	writeLogFor(t, projects, "claude-readable", `{"type":"user","message":{"content":"a turn worth keeping"}}`+"\n")
	writeLogFor(t, projects, "claude-unsyncable", `{"type":"user","message":{"content":"never accepted"}}`+"\n")

	store := failingTranscripts{transcriptStore: st.Transcripts, failFor: unsyncable.ID}
	syncer := NewTranscriptSyncer(TranscriptSyncerParams{
		Agents:      st.Agents,
		Sessions:    st.Agents,
		Transcripts: store,
		Harnesses:   mustRegistry(t, claude.New(claude.Options{ProjectsDir: projects})),
	})

	// The failure is still reported, so serve logs it rather than leaving an agent
	// quietly never indexed.
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

// failingTranscripts refuses one agent's writes and passes every other call
// through to the real store.
type failingTranscripts struct {
	transcriptStore
	failFor string
}

func (f failingTranscripts) Upsert(ctx context.Context, params transcript.UpsertParams) error {
	if params.AgentID == f.failFor {
		return errors.New("this log will never be accepted")
	}

	return f.transcriptStore.Upsert(ctx, params)
}

// testSessionID is the claude session id every test log is keyed to. The store id
// is generated on insert, so this only needs to be stable within a test.
const testSessionID = "claude-abc"

// seedWithSessionID inserts an agent under the given name carrying a claude session
// id, and returns it with its generated store id, so a transcript can be keyed to
// the right row.
func seedWithSessionID(t *testing.T, st *testStore, sessionID, name string) agentstore.Agent {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, st.Create(ctx, agentstore.Agent{Name: name, State: agentstore.StateIdle, SessionID: sessionID}))
	got, err := st.Get(ctx, name)
	require.NoError(t, err)
	require.NotNil(t, got)

	return *got
}

// writeLog writes an agent's JSONL log under a project subdirectory of the
// given projects dir, the way Claude Code lays them out.
func writeLog(t *testing.T, projectsDir, body string) string {
	t.Helper()

	return writeLogFor(t, projectsDir, testSessionID, body)
}

// writeLogFor is writeLog for a named session, which a test seeding more than one
// agent needs.
func writeLogFor(t *testing.T, projectsDir, sessionID, body string) string {
	t.Helper()
	dir := filepath.Join(projectsDir, "-some-project")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, sessionID+".jsonl")
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

// TestSyncReportsAnUnknownHarness covers an agent whose harness nothing implements.
// Its transcript can never advance again, so the pass reports it rather than
// skipping it: a search that quietly finds nothing is how that would go unnoticed.
func TestSyncReportsAnUnknownHarness(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()

	require.NoError(t, st.Create(ctx, agentstore.Agent{
		Name:      "orphan",
		State:     agentstore.StateIdle,
		Harness:   "gemini",
		SessionID: testSessionID,
	}))

	syncer := NewTranscriptSyncer(TranscriptSyncerParams{
		Agents:      st.Agents,
		Sessions:    st.Agents,
		Transcripts: st.Transcripts,
		Harnesses:   mustRegistry(t, claude.New(claude.Options{ProjectsDir: t.TempDir(), SessionsDir: t.TempDir()})),
	})

	err := syncer.Sync(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "orphan", "the pass names the agent it could not read")
	assert.Contains(t, err.Error(), "gemini", "and the harness it is waiting on")
}

// TestSyncAdoptsSessions covers a harness that names its own conversations. The panel
// cannot tell codex which id to use, so the syncer asks afterwards which conversation
// the CLI started for that agent and records it.
func TestSyncAdoptsSessions(t *testing.T) {
	tests := []struct {
		name string
		// found is what the harness reports for the agent, and held is a conversation the
		// agent was already recorded as holding.
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
			ctx := context.Background()

			require.NoError(t, st.Create(ctx, agentstore.Agent{
				Name: "adopter", State: agentstore.StateIdle, SessionID: tt.held,
			}))

			syncer := NewTranscriptSyncer(TranscriptSyncerParams{
				Agents:      st.Agents,
				Sessions:    st.Agents,
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

// TestSyncAdoptedSessionIsIndexed covers the pass after an adoption, driving the real
// codex harness over a real rollout. Adopting is only worth anything if the transcript
// that turns up under that id is then read.
func TestSyncAdoptedSessionIsIndexed(t *testing.T) {
	st := newStore(t)
	ctx := context.Background()
	sessions := t.TempDir()
	cwd := t.TempDir()

	const sessionID = "019fb3e7-2cb6-7ab3-86a0-51a670a33140"

	require.NoError(t, st.Create(ctx, agentstore.Agent{
		Name: "adopter", State: agentstore.StateIdle, Harness: "codex", Cwd: cwd,
	}))
	created, err := st.Get(ctx, "adopter")
	require.NoError(t, err)
	require.NotNil(t, created)

	// The originator is what the launch would have stamped on the rollout, which is the
	// whole of what makes this conversation this agent's.
	dir := filepath.Join(sessions, "2026", "07", "30")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	rollout := filepath.Join(dir, "rollout-2026-07-30T12-41-39-"+sessionID+".jsonl")
	require.NoError(t, os.WriteFile(rollout,
		[]byte(`{"type":"session_meta","payload":{"id":"`+sessionID+`","originator":"weave-`+created.ID+`"}}`+"\n"+
			`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"how do I use trigram search"}]}}`+"\n"),
		0o600))

	syncer := NewTranscriptSyncer(TranscriptSyncerParams{
		Agents:      st.Agents,
		Sessions:    st.Agents,
		Transcripts: st.Transcripts,
		Harnesses:   mustRegistry(t, codex.New(codex.Options{SessionsDir: sessions})),
	})

	// The first pass adopts and reads in one go, since the rollout is already on disk.
	require.NoError(t, syncer.Sync(ctx))

	adopted, err := st.Get(ctx, "adopter")
	require.NoError(t, err)
	require.NotNil(t, adopted)
	assert.Equal(t, sessionID, adopted.SessionID, "the id codex chose is recorded")

	tr, err := st.GetTranscript(ctx, adopted.ID)
	require.NoError(t, err)
	assert.Equal(t, "how do I use trigram search", tr.Content)
}
