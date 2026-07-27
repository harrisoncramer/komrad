package agents

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	agentstore "claude-agents/internal/store/agent"
	"claude-agents/internal/store/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewAgent covers a launch end to end. A refused launch has to record
// nothing. A row pointing at a pane that does not exist reads as a live agent
// forever, and tmux resolves an empty pane target to whatever the user is
// looking at.
func TestNewAgent(t *testing.T) {
	tests := []struct {
		name        string
		dir         func(t *testing.T) string
		title       string
		windowErr   error
		badWorkflow bool
		wantErrIs   error
		wantErr     string
	}{
		{
			name:      "a directory nobody can enter",
			dir:       func(*testing.T) string { return "/no/such/directory" },
			title:     "nowhere",
			wantErrIs: ErrBadDirectory,
		},
		{
			name: "a path that is a file",
			dir: func(t *testing.T) string {
				file := filepath.Join(t.TempDir(), "a-file")
				require.NoError(t, os.WriteFile(file, []byte("not a directory"), 0o644))

				return file
			},
			title:   "a-file",
			wantErr: "not a directory",
		},
		{
			name:        "a workflow that does not exist, which would render nowhere",
			dir:         func(t *testing.T) string { return t.TempDir() },
			title:       "orphan",
			badWorkflow: true,
			wantErrIs:   ErrWorkflowNotFound,
		},
		{
			name:      "a window tmux refuses",
			dir:       func(t *testing.T) string { return t.TempDir() },
			title:     "doomed",
			windowErr: errors.New("tmux refused the window name"),
			wantErr:   "failed to open a window",
		},
		{
			name:  "a launch that works",
			dir:   func(t *testing.T) string { return t.TempDir() },
			title: "My Agent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := newFakeTmux()
			tmux.newWindowErr = tt.windowErr
			st := newStore(t)
			manager := newManager(tmux, st, "--default-flag")

			params := NewAgentParams{
				Title:  tt.title,
				Dir:    tt.dir(t),
				Flags:  "--extra",
				Prompt: "do the thing",
			}
			if tt.badWorkflow {
				missing := "no-such-workflow"
				params.WorkflowID = &missing
			}

			created, err := manager.NewAgent(t.Context(), params)

			if tt.wantErrIs != nil || tt.wantErr != "" {
				require.Error(t, err)
				if tt.wantErrIs != nil {
					require.ErrorIs(t, err, tt.wantErrIs)
				}
				if tt.wantErr != "" {
					require.ErrorContains(t, err, tt.wantErr)
				}

				assert.Empty(t, tmux.sent, "nothing is typed when the launch was refused")

				agents, listErr := st.List(t.Context())
				require.NoError(t, listErr)
				assert.Empty(t, agents, "a refused launch records nothing")

				return
			}

			require.NoError(t, err)
			require.NotNil(t, created)
			assert.Equal(t, "My-Agent", created.Name, "the caller learns the generated name")
			assert.NotEmpty(t, created.ID, "and the stored id")

			stored, err := st.Get(t.Context(), "My-Agent")
			require.NoError(t, err)
			require.NotNil(t, stored)
			assert.Equal(t, "%My-Agent", stored.Pane, "the new window's pane is the agent's handle")
			assert.Equal(t, params.Dir, stored.Cwd)
			assert.Equal(t, "--default-flag --extra", stored.Flags, "the configured flags come first")
			assert.Equal(t, "do the thing", stored.Prompt)
			assert.Equal(t, agentstore.StateNew, stored.State)
			assert.NotEmpty(t, stored.ClaudeSessionID, "a conversation id is pinned at launch")
			assert.NotEmpty(t, tmux.lastKeys("%My-Agent"), "claude is launched into the new pane")
		})
	}
}

// TestNewAgentResolvesDirectory covers where an agent starts. tmux does no tilde
// expansion, so a ~ path has to be absolute before the window is opened or the
// stored cwd would not be where the agent is running.
func TestNewAgentResolvesDirectory(t *testing.T) {
	tests := []struct {
		name      string
		dir       string
		useClient bool
	}{
		{
			name:      "a blank directory falls back to the invoking client's",
			useClient: true,
		},
		{
			name: "a ~ path is expanded before tmux sees it",
			dir:  "~/chariot/chariot",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)

			want := filepath.Join(home, "chariot", "chariot")
			require.NoError(t, os.MkdirAll(want, 0o755))

			tmux := newFakeTmux()
			if tt.useClient {
				tmux.clientDir = want
			}

			st := newStore(t)
			manager := newManager(tmux, st, "")

			_, err := manager.NewAgent(t.Context(), NewAgentParams{Title: "scoped", Dir: tt.dir})
			require.NoError(t, err)

			assert.Equal(t, want, tmux.newWindows["scoped"], "tmux is given an absolute path")

			stored, err := st.Get(t.Context(), "scoped")
			require.NoError(t, err)
			require.NotNil(t, stored)
			assert.Equal(t, want, stored.Cwd, "and the stored cwd matches it")
		})
	}
}

// TestNewAgentInsideWorkflow covers what an agent inherits from the bucket it is
// created in. The context is a snapshot taken at launch, so a later edit reaches
// only agents spawned after it.
func TestNewAgentInsideWorkflow(t *testing.T) {
	tests := []struct {
		name        string
		context     string
		scoped      bool
		wantContext bool
	}{
		{
			name:   "an agent created outside every workflow carries none",
			scoped: false,
		},
		{
			name:   "a workflow with no context adds nothing",
			scoped: true,
		},
		{
			name:        "a workflow's context is appended to the system prompt",
			context:     "shared notes",
			scoped:      true,
			wantContext: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := newFakeTmux()
			st := newStore(t)
			manager := newManager(tmux, st, "")

			params := NewAgentParams{Title: "agent", Dir: t.TempDir()}
			if tt.scoped {
				created, err := workflowsFor(st).NewWorkflow(t.Context(), workflow.Config{
					Name:    "backend",
					Context: tt.context,
				})
				require.NoError(t, err)
				params.WorkflowID = &created.ID
			}

			_, err := manager.NewAgent(t.Context(), params)
			require.NoError(t, err)

			stored, err := st.Get(t.Context(), "agent")
			require.NoError(t, err)
			require.NotNil(t, stored)

			if tt.scoped {
				require.NotNil(t, stored.WorkflowID, "the agent carries its workflow's id")
				assert.Equal(t, *params.WorkflowID, *stored.WorkflowID)
			} else {
				assert.Nil(t, stored.WorkflowID)
			}

			prompt := appendedPrompt(t, tmux.lastKeys("%agent")[0])
			assert.Contains(t, prompt, "/mcp", "every agent is told about the endpoint")
			if tt.wantContext {
				assert.Contains(t, prompt, "shared notes")
				return
			}

			assert.NotContains(t, prompt, "shared notes")
		})
	}
}

// TestUniqueAgentName covers the name an agent ends up with. The store's unique
// index is the authority, so a taken name has to be stepped past rather than
// reused, which would clobber the agent holding it.
func TestUniqueAgentName(t *testing.T) {
	tests := []struct {
		name     string
		title    string
		prompt   string
		taken    []string
		wantName string
	}{
		{
			name:     "no title and no prompt falls back to a label",
			wantName: "agent",
		},
		{
			name:     "a taken title steps to the next free suffix",
			title:    "agent",
			taken:    []string{"agent"},
			wantName: "agent-2",
		},
		{
			name:     "characters tmux rejects are replaced",
			title:    "My Agent: v1.0",
			wantName: "My-Agent--v1-0",
		},
		{
			name:     "a blank title falls back to a slug of the prompt",
			prompt:   "Fix the login bug\nand more",
			wantName: "fix-the-login-bug",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			for _, name := range tt.taken {
				seed(t, st, name, "%1", agentstore.StateNew)
			}

			manager := newManager(newFakeTmux(), st, "")

			assert.Equal(t, tt.wantName, manager.uniqueAgentName(t.Context(), tt.title, tt.prompt))
		})
	}
}

func TestNameTaken(t *testing.T) {
	tests := []struct {
		name  string
		check string
		want  bool
	}{
		{
			name:  "a name nobody holds",
			check: "free",
			want:  false,
		},
		{
			name:  "a name a live agent holds",
			check: "taken",
			want:  true,
		},
	}

	st := newStore(t)
	seed(t, st, "taken", "%1", agentstore.StateDone)
	manager := newManager(newFakeTmux(), st, "")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, manager.nameTaken(t.Context(), tt.check))
		})
	}
}

// TestArchiveAgent covers hiding an agent. The record and its transcript stay,
// so search still reaches it, but the agent is stopped.
func TestArchiveAgent(t *testing.T) {
	tests := []struct {
		name      string
		seed      string
		target    string
		wantErrIs error
	}{
		{
			name:      "a name nobody holds is not a quiet success",
			target:    "ghost",
			wantErrIs: ErrAgentNotFound,
		},
		{
			name:   "a live agent is stopped and hidden",
			seed:   "target",
			target: "target",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			tmux := newFakeTmux()
			manager := newManager(tmux, st, "")

			if tt.seed != "" {
				seed(t, st, tt.seed, "%1", agentstore.StateThinking)
			}

			err := manager.ArchiveAgent(t.Context(), tt.target)
			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)
				return
			}

			require.NoError(t, err)
			assert.Contains(t, tmux.killedPanes, "%1", "the agent's pane is killed")

			stored, err := st.Get(t.Context(), tt.target)
			require.NoError(t, err)
			assert.Nil(t, stored, "the agent is hidden from listings")
		})
	}
}

// TestRestore covers bringing an archived agent back. Its name may have been
// taken while it sat hidden, in which case restoring under that name would
// collide on the unique index.
func TestRestore(t *testing.T) {
	tests := []struct {
		name         string
		reuseName    bool
		wantSameName bool
	}{
		{
			name:         "a name taken since is stepped past",
			reuseName:    true,
			wantSameName: false,
		},
		{
			name:         "a free name is kept",
			wantSameName: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			manager := newManager(newFakeTmux(), st, "")
			ctx := t.Context()

			require.NoError(t, st.Upsert(ctx, agentstore.Agent{
				Name:            "foo",
				State:           agentstore.StateDone,
				ClaudeSessionID: "sess-old",
			}))
			original, err := st.Get(ctx, "foo")
			require.NoError(t, err)
			require.NoError(t, st.SoftDelete(ctx, "foo"))

			if tt.reuseName {
				require.NoError(t, st.Upsert(ctx, agentstore.Agent{Name: "foo", State: agentstore.StateNew}))
			}

			archived := *original
			archived.Deleted = true
			require.NoError(t, manager.restore(ctx, archived))

			restored := agentByID(t, st, original.ID)
			assert.False(t, restored.Deleted, "the agent is back in the listing")
			assert.Equal(t, agentstore.StateNew, restored.State)
			assert.Equal(t, "sess-old", restored.ClaudeSessionID, "the conversation is preserved")

			if tt.wantSameName {
				assert.Equal(t, "foo", restored.Name)
				return
			}

			assert.NotEqual(t, "foo", restored.Name, "the restored agent takes a fresh name")

			live, err := st.Get(ctx, "foo")
			require.NoError(t, err)
			require.NotNil(t, live)
			assert.NotEqual(t, original.ID, live.ID, "the namesake that took the name is untouched")
		})
	}
}
