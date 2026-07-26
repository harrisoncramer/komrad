package session

import (
	"context"
	"errors"
	"testing"

	"claude-agents/internal/agents"
	"claude-agents/internal/pgtest"
	"claude-agents/internal/store"
	sessionv1 "claude-agents/proto/gen/session/v1"

	sessionstore "claude-agents/internal/store/session"
	transcriptstore "claude-agents/internal/store/transcript"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateSession(t *testing.T) {
	workflow := "wf-1"

	tests := []struct {
		name     string
		req      *sessionv1.CreateSessionRequest
		launchEr error
		wantCode connect.Code
		check    func(t *testing.T, got agents.NewSessionParams)
	}{
		{
			name:     "a launch that cannot start is an internal error",
			req:      &sessionv1.CreateSessionRequest{Prompt: "go"},
			launchEr: errors.New("tmux is not running"),
			wantCode: connect.CodeInternal,
		},
		{
			name:     "an unknown workflow is the caller's mistake",
			req:      &sessionv1.CreateSessionRequest{Prompt: "go", WorkflowId: &workflow},
			launchEr: agents.ErrWorkflowNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "an unusable directory is refused",
			req:      &sessionv1.CreateSessionRequest{Prompt: "go", Dir: "/no/such/dir"},
			launchEr: agents.ErrBadDirectory,
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name: "an omitted workflow stays unassigned rather than becoming an empty id",
			req:  &sessionv1.CreateSessionRequest{Prompt: "go"},
			check: func(t *testing.T, got agents.NewSessionParams) {
				assert.Nil(t, got.WorkflowID)
			},
		},
		{
			name: "every field reaches the launcher",
			req: &sessionv1.CreateSessionRequest{
				Prompt:     "audit the migrations",
				Title:      "audit",
				Dir:        "/repo",
				Flags:      "--model opus",
				WorkflowId: &workflow,
			},
			check: func(t *testing.T, got agents.NewSessionParams) {
				assert.Equal(t, "audit the migrations", got.Prompt)
				assert.Equal(t, "audit", got.Title)
				assert.Equal(t, "/repo", got.Dir)
				assert.Equal(t, "--model opus", got.Flags)
				require.NotNil(t, got.WorkflowID)
				assert.Equal(t, workflow, *got.WorkflowID)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, launcher := serviceWithLauncher(t)
			launcher.err = tt.launchEr

			created, err := svc.CreateSession(t.Context(), connect.NewRequest(tt.req))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
				return
			}

			require.NoError(t, err)
			assert.Equal(t, "spawned", created.Msg.GetSession().GetName())
			tt.check(t, launcher.got)
		})
	}
}

func TestGetSession(t *testing.T) {
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
			name:     "an archived chat is not listed either",
			seed:     "alpha",
			lookup:   "beta",
			wantCode: connect.CodeNotFound,
		},
		{
			name:   "a chat that exists",
			seed:   "alpha",
			lookup: "alpha",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, st := service(t)

			if tt.seed != "" {
				seedChat(t, st, sessionstore.Agent{Name: tt.seed, Cwd: "/tmp", State: sessionstore.StateThinking})
			}

			got, err := svc.GetSession(t.Context(), connect.NewRequest(&sessionv1.GetSessionRequest{Name: tt.lookup}))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
				return
			}

			require.NoError(t, err)
			assert.Equal(t, "/tmp", got.Msg.GetSession().GetCwd())
			assert.Equal(t, sessionstore.StateThinking, got.Msg.GetSession().GetState())
		})
	}
}

// TestListSessions covers the listing and the liveness flag on it, which is
// observed at read time so a client can tell a running chat from a dormant one
// without tmux of its own.
func TestListSessions(t *testing.T) {
	tests := []struct {
		name  string
		seed  []string
		alive bool
	}{
		{
			name: "an empty store lists nothing",
		},
		{
			name:  "a dormant chat reports itself as such",
			seed:  []string{"alpha"},
			alive: false,
		},
		{
			name:  "a live chat is flagged alive",
			seed:  []string{"alpha", "beta"},
			alive: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, st, launcher := serviceWithLauncher(t)
			launcher.alive = tt.alive

			for _, name := range tt.seed {
				seedChat(t, st, sessionstore.Agent{Name: name, Pane: "%1", State: sessionstore.StateThinking})
			}

			listed, err := svc.ListSessions(t.Context(), connect.NewRequest(&sessionv1.ListSessionsRequest{}))
			require.NoError(t, err)
			require.Len(t, listed.Msg.GetSessions(), len(tt.seed))

			for _, s := range listed.Msg.GetSessions() {
				assert.Equal(t, tt.alive, s.GetAlive())
			}
		})
	}
}

// TestUpdateSession covers renaming by id. A name a live chat already holds is a
// conflict rather than a server fault, and the driver's own message stays
// server-side.
func TestUpdateSession(t *testing.T) {
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
			name:     "a name a live chat already holds",
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
				chat := seedChat(t, st, sessionstore.Agent{Name: name})
				if name == tt.rename {
					id = chat.ID
				}
			}

			_, err := svc.UpdateSession(ctx, connect.NewRequest(&sessionv1.UpdateSessionRequest{Id: id, Name: tt.to}))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
				assert.NotContains(t, err.Error(), "constraint", "the driver's message stays server-side")
				return
			}

			require.NoError(t, err)

			renamed, err := st.chats.Get(ctx, tt.to)
			require.NoError(t, err)
			require.NotNil(t, renamed)
			assert.Equal(t, id, renamed.ID, "a rename preserves the id")

			stale, err := st.chats.Get(ctx, tt.rename)
			require.NoError(t, err)
			assert.Nil(t, stale, "the old name no longer resolves")
		})
	}
}

func TestSetState(t *testing.T) {
	tests := []struct {
		name     string
		seed     string
		target   string
		state    string
		wantCode connect.Code
	}{
		{
			name:     "a name nobody holds is not a quiet success",
			target:   "ghost",
			state:    sessionstore.StateDone,
			wantCode: connect.CodeNotFound,
		},
		{
			name:   "a chat moves to done",
			seed:   "alpha",
			target: "alpha",
			state:  sessionstore.StateDone,
		},
		{
			name:   "and to waiting",
			seed:   "alpha",
			target: "alpha",
			state:  sessionstore.StateWaiting,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, st := service(t)
			ctx := t.Context()

			if tt.seed != "" {
				seedChat(t, st, sessionstore.Agent{Name: tt.seed, State: sessionstore.StateThinking})
			}

			_, err := svc.SetState(ctx, connect.NewRequest(&sessionv1.SetStateRequest{Name: tt.target, State: tt.state}))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
				return
			}

			require.NoError(t, err)

			got, err := st.chats.Get(ctx, tt.target)
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, tt.state, got.State)
		})
	}
}

// TestSessionLifecycleGoesThroughTheService covers delete, archive, and restore.
// All three hand off to the thing that can stop or relaunch an agent rather than
// editing rows behind its back, which is what leaves an agent running with
// nothing able to name it.
func TestSessionLifecycleGoesThroughTheService(t *testing.T) {
	tests := []struct {
		name       string
		call       func(t *testing.T, svc *Service) error
		fail       func(l *fakeLauncher)
		wantCode   connect.Code
		wantCalled func(t *testing.T, l *fakeLauncher)
	}{
		{
			name: "deleting a chat nobody holds",
			call: func(t *testing.T, svc *Service) error {
				_, err := svc.DeleteSession(t.Context(), connect.NewRequest(&sessionv1.DeleteSessionRequest{Name: "ghost"}))
				return err
			},
			fail:     func(l *fakeLauncher) { l.deleteErr = agents.ErrSessionNotFound },
			wantCode: connect.CodeNotFound,
		},
		{
			name: "archiving a chat nobody holds",
			call: func(t *testing.T, svc *Service) error {
				_, err := svc.ArchiveSession(t.Context(), connect.NewRequest(&sessionv1.ArchiveSessionRequest{Name: "ghost"}))
				return err
			},
			fail:     func(l *fakeLauncher) { l.archiveErr = agents.ErrSessionNotFound },
			wantCode: connect.CodeNotFound,
		},
		{
			name: "restoring an id nobody holds",
			call: func(t *testing.T, svc *Service) error {
				_, err := svc.RestoreSession(t.Context(), connect.NewRequest(&sessionv1.RestoreSessionRequest{Id: "ghost"}))
				return err
			},
			fail:     func(l *fakeLauncher) { l.restoreErr = agents.ErrSessionNotFound },
			wantCode: connect.CodeNotFound,
		},
		{
			name: "deleting stops the agent",
			call: func(t *testing.T, svc *Service) error {
				_, err := svc.DeleteSession(t.Context(), connect.NewRequest(&sessionv1.DeleteSessionRequest{Name: "alpha"}))
				return err
			},
			wantCalled: func(t *testing.T, l *fakeLauncher) {
				assert.Equal(t, []string{"alpha"}, l.deleted)
			},
		},
		{
			name: "archiving stops the agent",
			call: func(t *testing.T, svc *Service) error {
				_, err := svc.ArchiveSession(t.Context(), connect.NewRequest(&sessionv1.ArchiveSessionRequest{Name: "alpha"}))
				return err
			},
			wantCalled: func(t *testing.T, l *fakeLauncher) {
				assert.Equal(t, []string{"alpha"}, l.archived)
			},
		},
		{
			name: "restoring relaunches it",
			call: func(t *testing.T, svc *Service) error {
				_, err := svc.RestoreSession(t.Context(), connect.NewRequest(&sessionv1.RestoreSessionRequest{Id: "id-1"}))
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

// fakeLauncher records what a request asked for and hands back a stored chat, so
// the handlers can be exercised without a tmux server.
type fakeLauncher struct {
	got        agents.NewSessionParams
	created    *sessionstore.Agent
	err        error
	alive      bool
	deleted    []string
	deleteErr  error
	archived   []string
	archiveErr error
	restored   []string
	restoreErr error
}

func (f *fakeLauncher) NewSession(_ context.Context, p agents.NewSessionParams) (*sessionstore.Agent, error) {
	f.got = p
	if f.err != nil {
		return nil, f.err
	}

	return f.created, nil
}

// PaneAlive answers for the fake's chats. Nothing here runs tmux, so a chat is
// live exactly when the test says it is.
func (f *fakeLauncher) PaneAlive(string) bool { return f.alive }

func (f *fakeLauncher) DeleteSession(_ context.Context, name string) error {
	f.deleted = append(f.deleted, name)

	return f.deleteErr
}

func (f *fakeLauncher) ArchiveSession(_ context.Context, name string) error {
	f.archived = append(f.archived, name)

	return f.archiveErr
}

func (f *fakeLauncher) RestoreSession(_ context.Context, id string) (*sessionstore.Agent, error) {
	f.restored = append(f.restored, id)
	if f.restoreErr != nil {
		return nil, f.restoreErr
	}

	return f.created, nil
}

// stores is what a test arranges rows through: the two domain stores this
// handler reads, built over one pool. The handler itself still receives them
// separately, exactly as the composition root passes them.
type stores struct {
	chats       *sessionstore.Store
	transcripts *transcriptstore.Store
}

func service(t *testing.T) (*Service, *stores) {
	t.Helper()
	svc, st, _ := serviceWithLauncher(t)

	return svc, st
}

func serviceWithLauncher(t *testing.T) (*Service, *stores, *fakeLauncher) {
	t.Helper()

	pool, err := store.Open(t.Context(), pgtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })

	st := &stores{
		chats:       sessionstore.New(pool.DB()),
		transcripts: transcriptstore.New(pool.DB()),
	}

	launcher := &fakeLauncher{created: &sessionstore.Agent{ID: "id-1", Name: "spawned", Cwd: "/repo", State: sessionstore.StateNew}}

	return NewService(st.chats, st.transcripts, launcher), st, launcher
}

// seedChat records a chat and returns it as stored, since most rows here exist
// only to be acted on by id.
func seedChat(t *testing.T, st *stores, agent sessionstore.Agent) *sessionstore.Agent {
	t.Helper()

	require.NoError(t, st.chats.Upsert(t.Context(), agent))

	stored, err := st.chats.Get(t.Context(), agent.Name)
	require.NoError(t, err)
	require.NotNil(t, stored)

	return stored
}
