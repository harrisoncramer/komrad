package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"weave/internal/agents"
	"weave/internal/storetest"
	"weave/lib/xptr"
	agentv1 "weave/proto/gen/agent/v1"

	agentstore "weave/internal/store/agent"
	transcriptstore "weave/internal/store/transcript"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateAgent(t *testing.T) {
	workflow := "wf-1"

	tests := []struct {
		name     string
		req      *agentv1.CreateAgentRequest
		launchEr error
		wantCode connect.Code
		check    func(t *testing.T, got agents.NewAgentParams)
	}{
		{
			name:     "a launch that cannot start is an internal error",
			req:      &agentv1.CreateAgentRequest{Prompt: "go"},
			launchEr: errors.New("tmux is not running"),
			wantCode: connect.CodeInternal,
		},
		{
			name:     "an unknown workflow is the caller's mistake",
			req:      &agentv1.CreateAgentRequest{Prompt: "go", WorkflowId: workflow},
			launchEr: agents.ErrWorkflowNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "an unusable directory is refused",
			req:      &agentv1.CreateAgentRequest{Prompt: "go", Dir: "/no/such/dir"},
			launchEr: agents.ErrBadDirectory,
			wantCode: connect.CodeInvalidArgument,
		},
		{
			// The workflow supplies the directory, so a request that names one and
			// nothing else is complete.
			name: "an omitted directory reaches the launcher empty, for the workflow to fill",
			req:  &agentv1.CreateAgentRequest{Prompt: "go", WorkflowId: workflow},
			check: func(t *testing.T, got agents.NewAgentParams) {
				assert.Empty(t, got.Dir)
				assert.Equal(t, workflow, got.WorkflowID)
			},
		},
		{
			name: "every field reaches the launcher",
			req: &agentv1.CreateAgentRequest{
				Prompt:     "audit the migrations",
				Title:      "audit",
				Dir:        "/repo",
				Flags:      "--model opus",
				WorkflowId: workflow,
			},
			check: func(t *testing.T, got agents.NewAgentParams) {
				assert.Equal(t, "audit the migrations", got.Prompt)
				assert.Equal(t, "audit", got.Title)
				assert.Equal(t, "/repo", got.Dir)
				assert.Equal(t, "--model opus", got.Flags)
				assert.Equal(t, workflow, got.WorkflowID)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, launcher := serviceWithLauncher(t)
			launcher.err = tt.launchEr

			created, err := svc.CreateAgent(t.Context(), connect.NewRequest(tt.req))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
				return
			}

			require.NoError(t, err)
			assert.Equal(t, "spawned", created.Msg.GetAgent().GetName())
			tt.check(t, launcher.got)
		})
	}
}

func TestGetAgent(t *testing.T) {
	tests := []struct {
		name     string
		seed     string
		lookup   string
		wantCode connect.Code
	}{
		{
			name:     "a name nobody holds",
			lookup:   "ghost",
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "an archived agent is not listed either",
			seed:     "alpha",
			lookup:   "beta",
			wantCode: connect.CodeNotFound,
		},
		{
			name:   "an agent that exists",
			seed:   "alpha",
			lookup: "alpha",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, st := service(t)

			if tt.seed != "" {
				st.SeedAgent(t, agentstore.Agent{Name: tt.seed, Cwd: "/tmp", State: agentstore.StateThinking})
			}

			got, err := svc.GetAgent(t.Context(), connect.NewRequest(&agentv1.GetAgentRequest{Name: tt.lookup}))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
				return
			}

			require.NoError(t, err)
			assert.Equal(t, "/tmp", got.Msg.GetAgent().GetCwd())
			assert.Equal(t, agentstore.StateThinking, got.Msg.GetAgent().GetState())

			created := got.Msg.GetAgent().GetCreatedAt()
			require.NotEmpty(t, created, "the creation time is emitted")
			ts, err := time.Parse(time.RFC3339, created)
			require.NoError(t, err, "as RFC 3339")
			assert.WithinDuration(t, time.Now(), ts, time.Minute)
		})
	}
}

// TestListAgents covers the listing and the liveness flag on it, which is
// observed at read time so a client can tell a running agent from a dormant one
// without tmux of its own.
func TestListAgents(t *testing.T) {
	tests := []struct {
		name  string
		seed  []string
		alive bool
	}{
		{
			name: "an empty store lists nothing",
		},
		{
			name:  "a dormant agent reports itself as such",
			seed:  []string{"alpha"},
			alive: false,
		},
		{
			name:  "a live agent is flagged alive",
			seed:  []string{"alpha", "beta"},
			alive: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, st, launcher := serviceWithLauncher(t)
			launcher.alive = tt.alive

			for _, name := range tt.seed {
				st.SeedAgent(t, agentstore.Agent{Name: name, Pane: "%1", State: agentstore.StateThinking})
			}

			listed, err := svc.ListAgents(t.Context(), connect.NewRequest(&agentv1.ListAgentsRequest{}))
			require.NoError(t, err)
			require.Len(t, listed.Msg.GetAgents(), len(tt.seed))

			for _, s := range listed.Msg.GetAgents() {
				assert.Equal(t, tt.alive, s.GetAlive())
			}
		})
	}
}

// TestListAgentsIdempotencyKey covers the creator's key reaching the listing. A
// caller that dispatched a batch of work under keys of its own has nothing else to
// match a listed agent back to the piece of work it covers, since the name is the
// user's to rename at any time.
func TestListAgentsIdempotencyKey(t *testing.T) {
	tests := []struct {
		name string
		key  *string
		want string
	}{
		{
			name: "an agent created without a claim carries no key",
			want: "",
		},
		{
			name: "a claimed agent reports the key it was created under",
			key:  xptr.Ptr("triage:ENG-5260:2026-07-29T15:30:00Z"),
			want: "triage:ENG-5260:2026-07-29T15:30:00Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, st := service(t)

			st.SeedAgent(t, agentstore.Agent{
				Name:           "alpha",
				Pane:           "%1",
				State:          agentstore.StateThinking,
				IdempotencyKey: tt.key,
			})

			listed, err := svc.ListAgents(t.Context(), connect.NewRequest(&agentv1.ListAgentsRequest{}))
			require.NoError(t, err)
			require.Len(t, listed.Msg.GetAgents(), 1)

			assert.Equal(t, tt.want, listed.Msg.GetAgents()[0].GetIdempotencyKey())
		})
	}
}

// TestListAgentsDeleted covers the flag that swaps which set is listed. The
// panel reads the live agents and its trash reads the deleted ones through the
// same call, so a request that ignored the flag would show one of them the
// other's rows.
func TestListAgentsDeleted(t *testing.T) {
	tests := []struct {
		name      string
		deleted   bool
		wantNames []string
		// wantDeletedAt is whether the listed agents carry a deletion time, which is what
		// the trash orders on and the only thing that says a row is in it.
		wantDeletedAt bool
	}{
		{
			name:      "the default listing is the live agents",
			wantNames: []string{"live"},
		},
		{
			name:          "asking for the deleted ones excludes the live",
			deleted:       true,
			wantNames:     []string{"archived"},
			wantDeletedAt: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, st, _ := serviceWithLauncher(t)
			ctx := t.Context()

			st.SeedAgent(t, agentstore.Agent{Name: "archived", Pane: "%1", State: agentstore.StateThinking})
			require.NoError(t, st.Agents.Archive(ctx, "archived"))
			st.SeedAgent(t, agentstore.Agent{Name: "live", Pane: "%2", State: agentstore.StateThinking})

			listed, err := svc.ListAgents(ctx, connect.NewRequest(&agentv1.ListAgentsRequest{Deleted: tt.deleted}))
			require.NoError(t, err)

			names := make([]string, 0, len(listed.Msg.GetAgents()))
			for _, agent := range listed.Msg.GetAgents() {
				names = append(names, agent.GetName())
			}
			assert.Equal(t, tt.wantNames, names)

			for _, agent := range listed.Msg.GetAgents() {
				if !tt.wantDeletedAt {
					assert.Nil(t, agent.DeletedAt, "a live agent carries no deletion time")

					continue
				}

				require.NotNil(t, agent.DeletedAt, "a deleted agent says when it was deleted")
				_, err := time.Parse(time.RFC3339, agent.GetDeletedAt())
				assert.NoError(t, err, "and says it as an RFC 3339 timestamp")
			}
		})
	}
}

// TestUpdateAgent covers renaming by id. A name a live agent already holds is a
// conflict rather than a server fault, and the driver's own message stays
// server-side.
func TestUpdateAgent(t *testing.T) {
	tests := []struct {
		name     string
		seed     []string
		rename   string
		to       string
		wantCode connect.Code
	}{
		{
			name:     "an id nobody holds",
			to:       "whatever",
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "a name a live agent already holds",
			seed:     []string{"alpha", "beta"},
			rename:   "beta",
			to:       "alpha",
			wantCode: connect.CodeAlreadyExists,
		},
		{
			name:   "a free name is taken and the id is kept",
			seed:   []string{"alpha"},
			rename: "alpha",
			to:     "renamed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, st := service(t)
			ctx := t.Context()

			id := "ghost"
			for _, name := range tt.seed {
				agent := st.SeedAgent(t, agentstore.Agent{Name: name})
				if name == tt.rename {
					id = agent.ID
				}
			}

			_, err := svc.UpdateAgent(ctx, connect.NewRequest(&agentv1.UpdateAgentRequest{Id: id, Name: tt.to}))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
				assert.NotContains(t, err.Error(), "constraint", "the driver's message stays server-side")
				return
			}

			require.NoError(t, err)

			renamed, err := st.Agents.Get(ctx, tt.to)
			require.NoError(t, err)
			require.NotNil(t, renamed)
			assert.Equal(t, id, renamed.ID, "a rename preserves the id")

			stale, err := st.Agents.Get(ctx, tt.rename)
			require.NoError(t, err)
			assert.Nil(t, stale, "the old name no longer resolves")
		})
	}
}

// TestSetHidden covers hiding over the wire. Hiding is the panel's own view of an
// agent rather than anything about the agent, so what matters is that the flag comes
// back on the agent the caller named and that naming nobody is refused.
func TestSetHidden(t *testing.T) {
	tests := []struct {
		name string
		seed bool
		// hideFirst is written before the call, so bringing an agent back has something
		// to bring back.
		hideFirst bool
		hidden    bool
		wantCode  connect.Code
	}{
		{
			name:     "an id nobody holds is not a quiet success",
			hidden:   true,
			wantCode: connect.CodeNotFound,
		},
		{
			name:   "an agent is hidden",
			seed:   true,
			hidden: true,
		},
		{
			name:      "and brought back again",
			seed:      true,
			hideFirst: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, st := service(t)
			ctx := t.Context()

			id := uuid.NewString()
			if tt.seed {
				id = st.SeedAgent(t, agentstore.Agent{Name: "alpha", State: agentstore.StateThinking}).ID
			}
			if tt.hideFirst {
				changed, err := st.Agents.SetHidden(ctx, id, true)
				require.NoError(t, err)
				require.True(t, changed)
			}

			_, err := svc.SetHidden(ctx, connect.NewRequest(&agentv1.SetHiddenRequest{Id: id, Hidden: tt.hidden}))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
				return
			}

			require.NoError(t, err)

			got, err := svc.GetAgent(ctx, connect.NewRequest(&agentv1.GetAgentRequest{Name: "alpha"}))
			require.NoError(t, err)
			assert.Equal(t, tt.hidden, got.Msg.GetAgent().GetHidden())
		})
	}
}

// TestSetMarked covers the mark over the wire. The server writes the stamp, so the
// request carries only a direction, and an id that matches nothing is refused rather
// than reported as a mark that never landed.
func TestSetMark(t *testing.T) {
	tests := []struct {
		name string
		seed bool
		// markFirst is put on the agent before the call, so changing and clearing a
		// mark both have one to work from.
		markFirst *string
		mark      *string
		wantCode  connect.Code
	}{
		{
			name:     "an id nobody holds is not a quiet success",
			mark:     xptr.Ptr("●"),
			wantCode: connect.CodeNotFound,
		},
		{
			name: "an agent is marked",
			seed: true,
			mark: xptr.Ptr("●"),
		},
		{
			name:      "an agent moves from one mark to another",
			seed:      true,
			markFirst: xptr.Ptr("●"),
			mark:      xptr.Ptr("✓"),
		},
		{
			name:      "and the mark is cleared again",
			seed:      true,
			markFirst: xptr.Ptr("●"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, st := service(t)
			ctx := t.Context()

			id := uuid.NewString()
			if tt.seed {
				id = st.SeedAgent(t, agentstore.Agent{Name: "alpha", State: agentstore.StateThinking}).ID
			}
			if tt.markFirst != nil {
				changed, err := st.Agents.SetMark(ctx, id, tt.markFirst)
				require.NoError(t, err)
				require.True(t, changed)
			}

			_, err := svc.SetMark(ctx, connect.NewRequest(&agentv1.SetMarkRequest{Id: id, Mark: tt.mark}))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
				return
			}

			require.NoError(t, err)

			got, err := svc.GetAgent(ctx, connect.NewRequest(&agentv1.GetAgentRequest{Name: "alpha"}))
			require.NoError(t, err)
			assert.Equal(t, tt.mark, got.Msg.GetAgent().Mark)
		})
	}
}

// TestSetMetadata covers an agent recording facts about its own work and reading
// them back off the resource, which is where the panel gets them.
func TestSetMetadata(t *testing.T) {
	tests := []struct {
		name string
		seed bool
		// recordFirst is written before the call, so revising and clearing a fact both
		// have one to work from.
		recordFirst []agentstore.Metadata
		key         string
		value       *string
		wantCode    connect.Code
		// wantStored is the metadata the agent should carry afterwards, compared as
		// stored pairs rather than protos so the ordering the store promised is what is
		// asserted.
		wantStored []agentstore.Metadata
	}{
		{
			name:     "an id nobody holds is not a quiet success",
			key:      "pr",
			value:    xptr.Ptr("https://github.com/o/r/pull/1"),
			wantCode: connect.CodeNotFound,
		},
		{
			// The key is gone either way, so failing a request that got what it asked
			// for would have an agent retrying a clear that already held.
			name: "clearing a key nothing held is served, not refused",
			seed: true,
			key:  "pr",
		},
		{
			name:  "an agent records a fact",
			seed:  true,
			key:   "linear_issue",
			value: xptr.Ptr("https://linear.app/x/issue/ENG-1"),
			wantStored: []agentstore.Metadata{
				{Key: "linear_issue", Value: "https://linear.app/x/issue/ENG-1"},
			},
		},
		{
			name: "revising one fact leaves the others alone",
			seed: true,
			recordFirst: []agentstore.Metadata{
				{Key: "branch", Value: "harry/x"},
				{Key: "pr", Value: "https://github.com/o/r/pull/1"},
			},
			key:   "pr",
			value: xptr.Ptr("https://github.com/o/r/pull/2"),
			wantStored: []agentstore.Metadata{
				{Key: "branch", Value: "harry/x"},
				{Key: "pr", Value: "https://github.com/o/r/pull/2"},
			},
		},
		{
			name: "and one is cleared without taking the rest",
			seed: true,
			recordFirst: []agentstore.Metadata{
				{Key: "branch", Value: "harry/x"},
				{Key: "pr", Value: "https://github.com/o/r/pull/1"},
			},
			key: "pr",
			wantStored: []agentstore.Metadata{
				{Key: "branch", Value: "harry/x"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, st := service(t)
			ctx := t.Context()

			id := uuid.NewString()
			if tt.seed {
				id = st.SeedAgent(t, agentstore.Agent{Name: "alpha", State: agentstore.StateThinking}).ID
			}
			for _, entry := range tt.recordFirst {
				changed, err := st.Agents.SetMetadata(ctx, agentstore.SetMetadataParams{ID: id, Key: entry.Key, Value: xptr.Ptr(entry.Value)})
				require.NoError(t, err)
				require.True(t, changed)
			}

			_, err := svc.SetMetadata(ctx, connect.NewRequest(&agentv1.SetMetadataRequest{Id: id, Key: tt.key, Value: tt.value}))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
				return
			}

			require.NoError(t, err)

			got, err := svc.GetAgent(ctx, connect.NewRequest(&agentv1.GetAgentRequest{Name: "alpha"}))
			require.NoError(t, err)
			assert.Equal(t, tt.wantStored, storedMetadata(got.Msg.GetAgent()))
		})
	}
}

// TestSearchTranscripts covers what a hit carries back to the caller.
//
// Every field on it is load-bearing. The turn's position and its byte offset are
// how the panel scrolls a match into view rather than hunting for the words
// again, so two fields crossed on the way out send the reader somewhere else in
// the conversation while still looking like a working search. Whether the agent is
// archived decides how the hit is labelled, since search reaches agents the listing
// does not.
func TestSearchTranscripts(t *testing.T) {
	tests := []struct {
		name string
		// turns are the seeded agent's conversation, in order.
		turns []string
		// archive puts the agent in the trash before the search, which search still
		// reaches.
		archive bool
		query   string
		// wantSeqs are the turns expected to match, in any order, since how two hits
		// rank against each other is the store's business.
		wantSeqs []int32
	}{
		{
			name:  "a query nothing said matches nothing",
			turns: []string{"talking about postgres"},
			query: "kubernetes",
		},
		{
			name:     "a hit names the turn that matched rather than the first one",
			turns:    []string{"opening question", "the reconciler derives its markers"},
			query:    "reconciler",
			wantSeqs: []int32{1},
		},
		{
			name:     "an archived agent is still searchable, and says so",
			turns:    []string{"the reconciler derives its markers"},
			archive:  true,
			query:    "reconciler",
			wantSeqs: []int32{0},
		},
		{
			name:     "a subject raised twice is a hit per turn",
			turns:    []string{"first on postgres", "later on postgres again"},
			query:    "postgres",
			wantSeqs: []int32{0, 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, st := service(t)
			ctx := t.Context()

			agent := st.SeedAgent(t, agentstore.Agent{Name: "alpha", State: agentstore.StateThinking})
			content := seedTranscript(t, st, agent.ID, tt.turns)
			if tt.archive {
				require.NoError(t, st.Agents.Archive(ctx, "alpha"))
			}

			found, err := svc.SearchTranscripts(ctx, connect.NewRequest(&agentv1.SearchTranscriptsRequest{
				Query: tt.query,
			}))
			require.NoError(t, err)
			require.Len(t, found.Msg.GetHits(), len(tt.wantSeqs))

			seqs := make([]int32, 0, len(tt.wantSeqs))
			for _, hit := range found.Msg.GetHits() {
				seqs = append(seqs, hit.GetSeq())

				assert.Equal(t, agent.ID, hit.GetAgentId())
				assert.Equal(t, "alpha", hit.GetName(), "the hit carries the agent's name, not its id alone")
				assert.Equal(t, tt.archive, hit.GetDeleted())
				assert.Equal(t, "user", hit.GetRole())
				assert.Contains(t, hit.GetSnippet(), tt.query, "the snippet holds the text that matched")

				// The offset has to land on the turn the hit names, which is the only
				// thing that makes the two fields worth sending together.
				turn := tt.turns[hit.GetSeq()]
				assert.Equal(t, turn, content[hit.GetContentOffset():hit.GetContentOffset()+int64(len(turn))])
			}

			assert.ElementsMatch(t, tt.wantSeqs, seqs)
		})
	}
}

// TestGetTranscript covers the one distinction this handler exists to make. An
// agent nobody holds and an agent whose log has not been synced yet would both
// read as empty content, which tells a caller nothing about which it got, so the
// agent is looked up first and only the unknown one is refused.
func TestGetTranscript(t *testing.T) {
	tests := []struct {
		name string
		// agentID, when set, is fetched instead of the seeded agent's own id.
		agentID string
		// turns are the seeded agent's conversation, empty for one whose log has not
		// been read yet.
		turns    []string
		wantCode connect.Code
		want     string
	}{
		{
			name:     "an id no agent holds is refused",
			agentID:  uuid.NewString(),
			wantCode: connect.CodeNotFound,
		},
		{
			// The store refuses a malformed id before it reaches a query, which must
			// read as an unknown agent rather than as a server fault.
			name:     "and so is an id that is not an id",
			agentID:  "not-a-uuid",
			wantCode: connect.CodeNotFound,
		},
		{
			name: "an agent with nothing synced yet is empty rather than missing",
			want: "",
		},
		{
			name:  "a synced agent hands back the whole conversation",
			turns: []string{"opening question", "the answer"},
			want:  "opening question\n\nthe answer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, st := service(t)

			agent := st.SeedAgent(t, agentstore.Agent{Name: "alpha", State: agentstore.StateThinking})
			seedTranscript(t, st, agent.ID, tt.turns)

			id := tt.agentID
			if id == "" {
				id = agent.ID
			}

			got, err := svc.GetTranscript(t.Context(), connect.NewRequest(&agentv1.GetTranscriptRequest{AgentId: id}))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got.Msg.GetContent())
		})
	}
}

// TestAgentLifecycleGoesThroughTheService covers delete, archive, and restore.
// All three hand off to the thing that can stop or relaunch an agent rather than
// editing rows behind its back, which is what leaves an agent running with
// nothing able to name it.
func TestAgentLifecycleGoesThroughTheService(t *testing.T) {
	tests := []struct {
		name       string
		call       func(t *testing.T, svc *Service) error
		fail       func(l *fakeLauncher)
		wantCode   connect.Code
		wantCalled func(t *testing.T, l *fakeLauncher)
	}{
		{
			name: "deleting an agent nobody holds",
			call: func(t *testing.T, svc *Service) error {
				_, err := svc.DeleteAgent(t.Context(), connect.NewRequest(&agentv1.DeleteAgentRequest{Id: "ghost"}))
				return err
			},
			fail:     func(l *fakeLauncher) { l.deleteErr = agents.ErrAgentNotFound },
			wantCode: connect.CodeNotFound,
		},
		{
			name: "archiving an agent nobody holds",
			call: func(t *testing.T, svc *Service) error {
				_, err := svc.ArchiveAgent(t.Context(), connect.NewRequest(&agentv1.ArchiveAgentRequest{Name: "ghost"}))
				return err
			},
			fail:     func(l *fakeLauncher) { l.archiveErr = agents.ErrAgentNotFound },
			wantCode: connect.CodeNotFound,
		},
		{
			name: "restoring an id nobody holds",
			call: func(t *testing.T, svc *Service) error {
				_, err := svc.RestoreAgent(t.Context(), connect.NewRequest(&agentv1.RestoreAgentRequest{Id: "ghost"}))
				return err
			},
			fail:     func(l *fakeLauncher) { l.restoreErr = agents.ErrAgentNotFound },
			wantCode: connect.CodeNotFound,
		},
		{
			name: "deleting stops the agent",
			call: func(t *testing.T, svc *Service) error {
				_, err := svc.DeleteAgent(t.Context(), connect.NewRequest(&agentv1.DeleteAgentRequest{Id: "alpha"}))
				return err
			},
			wantCalled: func(t *testing.T, l *fakeLauncher) {
				assert.Equal(t, []string{"alpha"}, l.deleted)
			},
		},
		{
			name: "archiving stops the agent",
			call: func(t *testing.T, svc *Service) error {
				_, err := svc.ArchiveAgent(t.Context(), connect.NewRequest(&agentv1.ArchiveAgentRequest{Name: "alpha"}))
				return err
			},
			wantCalled: func(t *testing.T, l *fakeLauncher) {
				assert.Equal(t, []string{"alpha"}, l.archived)
			},
		},
		{
			name: "restoring relaunches it",
			call: func(t *testing.T, svc *Service) error {
				_, err := svc.RestoreAgent(t.Context(), connect.NewRequest(&agentv1.RestoreAgentRequest{Id: "id-1"}))
				return err
			},
			wantCalled: func(t *testing.T, l *fakeLauncher) {
				assert.Equal(t, []string{"id-1"}, l.restored)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, launcher := serviceWithLauncher(t)
			if tt.fail != nil {
				tt.fail(launcher)
			}

			err := tt.call(t, svc)
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
				return
			}

			require.NoError(t, err)
			tt.wantCalled(t, launcher)
		})
	}
}

// fakeLauncher records what a request asked for and hands back a stored agent, so
// the handlers can be exercised without a tmux server.
type fakeLauncher struct {
	got        agents.NewAgentParams
	created    *agentstore.Agent
	err        error
	alive      bool
	deleted    []string
	deleteErr  error
	archived   []string
	archiveErr error
	restored   []string
	restoreErr error
}

func (f *fakeLauncher) NewAgent(_ context.Context, p agents.NewAgentParams) (*agentstore.Agent, error) {
	f.got = p
	if f.err != nil {
		return nil, f.err
	}

	return f.created, nil
}

// AgentRunning answers for the fake's agents. Nothing here runs tmux, so an agent
// is live exactly when the test says it is.
func (f *fakeLauncher) AgentRunning(agentstore.Agent) bool { return f.alive }

func (f *fakeLauncher) DeleteAgent(_ context.Context, id string) error {
	f.deleted = append(f.deleted, id)

	return f.deleteErr
}

func (f *fakeLauncher) ArchiveAgent(_ context.Context, name string) error {
	f.archived = append(f.archived, name)

	return f.archiveErr
}

func (f *fakeLauncher) RestoreAgent(_ context.Context, id string) (*agentstore.Agent, error) {
	f.restored = append(f.restored, id)
	if f.restoreErr != nil {
		return nil, f.restoreErr
	}

	return f.created, nil
}

func service(t *testing.T) (*Service, *storetest.Stores) {
	t.Helper()
	svc, st, _ := serviceWithLauncher(t)

	return svc, st
}

// seedTranscript stores a conversation and indexes its turns the way a sync pass
// leaves them, blank-line separated with each turn stamped where it landed, and
// returns the text it built. A test asserting on an offset compares against this
// rather than counting bytes by hand.
func seedTranscript(t *testing.T, st *storetest.Stores, agentID string, turns []string) string {
	t.Helper()

	if len(turns) == 0 {
		return ""
	}

	var content strings.Builder

	indexed := make([]transcriptstore.Turn, 0, len(turns))
	for _, text := range turns {
		if content.Len() > 0 {
			content.WriteString("\n\n")
		}

		indexed = append(indexed, transcriptstore.Turn{
			Role:    "user",
			Content: text,
			Offset:  int64(content.Len()),
		})
		content.WriteString(text)
	}

	ctx := t.Context()
	require.NoError(t, st.Transcripts.Upsert(ctx, transcriptstore.UpsertParams{
		AgentID: agentID,
		Content: content.String(),
		Offset:  int64(content.Len()),
	}))
	require.NoError(t, st.Transcripts.AppendTurns(ctx, agentID, indexed))

	return content.String()
}

// storedMetadata flattens a rendered agent's metadata back to stored pairs, so a
// test states what the agent should carry without spelling out protos.
func storedMetadata(agent *agentv1.Agent) []agentstore.Metadata {
	if len(agent.GetMetadata()) == 0 {
		return nil
	}

	stored := make([]agentstore.Metadata, 0, len(agent.GetMetadata()))
	for _, entry := range agent.GetMetadata() {
		stored = append(stored, agentstore.Metadata{Key: entry.GetKey(), Value: entry.GetValue()})
	}

	return stored
}

func serviceWithLauncher(t *testing.T) (*Service, *storetest.Stores, *fakeLauncher) {
	t.Helper()

	st := storetest.Open(t)
	launcher := &fakeLauncher{created: &agentstore.Agent{ID: "id-1", Name: "spawned", Cwd: "/repo", State: agentstore.StateIdle}}

	return NewService(st.Agents, st.Transcripts, launcher), st, launcher
}
