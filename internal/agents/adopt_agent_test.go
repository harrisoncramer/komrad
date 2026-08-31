package agents

import (
	"testing"

	"weave/internal/harness"
	agentstore "weave/internal/store/agent"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAdoptAgentForksTheConversation pins the shape of an adoption where the CLI can carry a
// conversation into an id of weave's own, which is the case that leaves the person's own
// conversation alone.
func TestAdoptAgentForksTheConversation(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	manager, h := newManagerWithHarness(tmux, st, "")

	dir := t.TempDir()
	source := uuid.NewString()
	h.seed(source, harness.Session{Dir: dir, Branch: "someones/work"})

	created, err := manager.adoptAgent.AdoptAgent(t.Context(), AdoptAgentParams{
		SessionID:  source,
		Prompt:     "carry on with it",
		WorkflowID: seedWorkflow(t, st),
	})
	require.NoError(t, err)
	require.NotNil(t, created)

	stored := agentByID(t, st, created.ID)
	assert.Equal(t, source, stored.AdoptedSession, "the conversation it was put on is recorded")
	assert.NotEqual(t, source, stored.SessionID, "and the agent holds a conversation of weave's own")
	assert.NotEmpty(t, stored.SessionID)
	assert.Equal(t, "someones/work", stored.AdoptedBranch, "the branch the log recorded comes along for pull request detection")
	assert.True(t, sameDir(dir, stored.Cwd), "the agent runs where the conversation was held")

	require.NotNil(t, stored.SetupAction)
	assert.Empty(t, *stored.SetupAction, "nothing is provisioned for a directory somebody else made")
	require.NotNil(t, stored.CleanupAction)
	assert.Empty(t, *stored.CleanupAction, "and nothing tears it down either")

	assert.Contains(t, stored.Prompt, "This is a copy", "the agent is told the person keeps their own conversation")
	assert.Contains(t, stored.Prompt, "carry on with it", "and still asked to do what it was asked to do")

	launch := lastLaunch(t, h)
	assert.Equal(t, source, launch.ResumeFrom, "the CLI is pointed at the conversation being adopted")
	assert.Equal(t, stored.SessionID, launch.SessionID, "and told to carry it into the id weave minted")
	assert.NotEmpty(t, launch.Context, "an adopted agent is briefed, since a resume carries no briefing of its own")
}

// TestAdoptAgentTakesOverWhereItCannotFork covers a CLI that names its own conversations, where
// adopting one means writing into the person's own log.
func TestAdoptAgentTakesOverWhereItCannotFork(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	manager, h := newManagerWithHarness(tmux, st, "")
	h.noFork = true

	source := uuid.NewString()
	h.seed(source, harness.Session{Dir: t.TempDir()})

	created, err := manager.adoptAgent.AdoptAgent(t.Context(), AdoptAgentParams{
		SessionID:  source,
		WorkflowID: seedWorkflow(t, st),
	})
	require.NoError(t, err)

	stored := agentByID(t, st, created.ID)
	assert.Equal(t, source, stored.SessionID, "the agent holds the person's own conversation, since there is no copy to hold")
	assert.Equal(t, source, stored.AdoptedSession)
	assert.Contains(t, stored.Prompt, "This is not a copy", "and it is told so, since what it says lands in their history")
}

// TestAdoptAgentRefusesToDoubleUp pins that a conversation only ever has one agent on it, since
// two CLIs on one history is the failure adoption exists to avoid.
func TestAdoptAgentRefusesToDoubleUp(t *testing.T) {
	tests := []struct {
		name string
		// hold puts an agent on a conversation and returns that agent and the conversation it
		// holds, which is either one it adopted or one weave started for it.
		hold      func(t *testing.T, st *testStore, manager *testAgents, h *fakeHarness, workflow string) (string, string)
		wantErrIs error
	}{
		{
			name: "a conversation already adopted",
			hold: func(t *testing.T, _ *testStore, manager *testAgents, h *fakeHarness, workflow string) (string, string) {
				source := uuid.NewString()
				h.seed(source, harness.Session{Dir: t.TempDir()})

				adopted, err := manager.adoptAgent.AdoptAgent(t.Context(), AdoptAgentParams{
					SessionID:  source,
					WorkflowID: workflow,
				})
				require.NoError(t, err)

				return adopted.ID, source
			},
		},
		{
			name: "a conversation weave started itself",
			hold: func(t *testing.T, st *testStore, manager *testAgents, h *fakeHarness, workflow string) (string, string) {
				created, err := manager.launch.NewAgent(t.Context(), NewAgentParams{
					Title:      "weaves-own",
					Dir:        t.TempDir(),
					WorkflowID: workflow,
				})
				require.NoError(t, err)

				// The conversation is on disk like any other, so what rules it out is the agent
				// already holding it rather than anything about the log.
				h.seed(created.SessionID, harness.Session{Dir: t.TempDir()})

				return created.ID, created.SessionID
			},
			wantErrIs: ErrSessionIsWeaves,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := newFakeTmux()
			st := newStore(t)
			manager, h := newManagerWithHarness(tmux, st, "")
			workflow := seedWorkflow(t, st)

			first, source := tt.hold(t, st, manager, h, workflow)

			again, err := manager.adoptAgent.AdoptAgent(t.Context(), AdoptAgentParams{
				SessionID:  source,
				WorkflowID: workflow,
			})

			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)
				assert.Len(t, liveAgents(t, st), 1, "and nothing is started beside the agent already on it")

				return
			}

			require.NoError(t, err)
			assert.Equal(t, first, again.ID, "asking twice hands back the agent already on it")
			assert.Len(t, liveAgents(t, st), 1, "rather than starting a second one")
		})
	}
}

// TestAdoptAgentRefusesWhatItCannotRun covers the conversations there is nothing to adopt from,
// and the directory that is somebody else's to work in.
func TestAdoptAgentRefusesWhatItCannotRun(t *testing.T) {
	tests := []struct {
		name string
		// setup arranges the case and returns the session id to adopt.
		setup     func(t *testing.T, st *testStore, manager *testAgents, h *fakeHarness) string
		wantErrIs error
	}{
		{
			name: "a session id no log holds",
			setup: func(*testing.T, *testStore, *testAgents, *fakeHarness) string {
				return uuid.NewString()
			},
			wantErrIs: ErrSessionNotFound,
		},
		{
			name: "a conversation held in a directory that is gone",
			setup: func(t *testing.T, _ *testStore, _ *testAgents, h *fakeHarness) string {
				source := uuid.NewString()
				h.seed(source, harness.Session{Dir: t.TempDir() + "/gone"})

				return source
			},
			wantErrIs: ErrSessionDirGone,
		},
		{
			name: "a conversation held where another agent works",
			setup: func(t *testing.T, st *testStore, manager *testAgents, h *fakeHarness) string {
				dir := t.TempDir()
				_, err := manager.launch.NewAgent(t.Context(), NewAgentParams{
					Title:      "already-there",
					Dir:        dir,
					WorkflowID: seedWorkflow(t, st),
				})
				require.NoError(t, err)

				source := uuid.NewString()
				h.seed(source, harness.Session{Dir: dir})

				return source
			},
			wantErrIs: ErrAdoptDirInUse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := newFakeTmux()
			st := newStore(t)
			manager, h := newManagerWithHarness(tmux, st, "")

			source := tt.setup(t, st, manager, h)

			_, err := manager.adoptAgent.AdoptAgent(t.Context(), AdoptAgentParams{
				SessionID:  source,
				WorkflowID: seedWorkflow(t, st),
			})
			require.ErrorIs(t, err, tt.wantErrIs)
		})
	}
}

func lastLaunch(t *testing.T, h *fakeHarness) harness.Launch {
	t.Helper()

	h.mu.Lock()
	defer h.mu.Unlock()

	require.NotEmpty(t, h.launches, "the harness was never asked to build a command")

	return h.launches[len(h.launches)-1]
}

func liveAgents(t *testing.T, st *testStore) []agentstore.Agent {
	t.Helper()

	listed, err := st.Agents.List(t.Context())
	require.NoError(t, err)

	return listed
}
