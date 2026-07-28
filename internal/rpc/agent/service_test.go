package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"claude-agents/internal/agents"
	"claude-agents/internal/storetest"
	"claude-agents/lib/xptr"
	agentv1 "claude-agents/proto/gen/agent/v1"

	agentstore "claude-agents/internal/store/agent"

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

// TestListAgentsDeleted covers the flag that swaps which set is listed. The
// panel reads the live agents and its trash reads the deleted ones through the
// same call, so a request that ignored the flag would show one of them the
// other's rows.
func TestListAgentsDeleted(t *testing.T) {
	tests := []struct {
		name      string
		deleted   bool
		wantNames []string
	}{
		{
			name:      "the default listing is the live agents",
			wantNames: []string{"live"},
		},
		{
			name:      "asking for the deleted ones excludes the live",
			deleted:   true,
			wantNames: []string{"archived"},
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
			state:    agentstore.StateDone,
			wantCode: connect.CodeNotFound,
		},
		{
			name:   "an agent moves to done",
			seed:   "alpha",
			target: "alpha",
			state:  agentstore.StateDone,
		},
		{
			name:   "and to waiting",
			seed:   "alpha",
			target: "alpha",
			state:  agentstore.StateWaiting,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, st := service(t)
			ctx := t.Context()

			if tt.seed != "" {
				st.SeedAgent(t, agentstore.Agent{Name: tt.seed, State: agentstore.StateThinking})
			}

			_, err := svc.SetState(ctx, connect.NewRequest(&agentv1.SetStateRequest{Name: tt.target, State: tt.state}))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
				return
			}

			require.NoError(t, err)

			got, err := st.Agents.Get(ctx, tt.target)
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, tt.state, got.State)
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

// PaneAlive answers for the fake's agents. Nothing here runs tmux, so an agent is
// live exactly when the test says it is.
func (f *fakeLauncher) PaneAlive(string) bool { return f.alive }

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

func serviceWithLauncher(t *testing.T) (*Service, *storetest.Stores, *fakeLauncher) {
	t.Helper()

	st := storetest.Open(t)
	launcher := &fakeLauncher{created: &agentstore.Agent{ID: "id-1", Name: "spawned", Cwd: "/repo", State: agentstore.StateNew}}

	return NewService(st.Agents, st.Transcripts, launcher), st, launcher
}
