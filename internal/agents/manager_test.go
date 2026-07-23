package agents

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"claude-agents/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newManager(tmux *fakeTmux, st *store.Store, flags string) *AgentManager {
	return NewAgentManager(Params{Tmux: tmux, Store: st, SelfPath: "self", ClaudeFlags: flags})
}

func TestStatus(t *testing.T) {
	tests := []struct {
		name         string
		states       []string
		wantEmpty    bool
		wantContains []string
	}{
		{
			name:      "no agents renders nothing",
			states:    nil,
			wantEmpty: true,
		},
		{
			name:      "only new agents render nothing",
			states:    []string{store.StateNew, store.StateNew},
			wantEmpty: true,
		},
		{
			name:         "done agents alone render a done segment",
			states:       []string{store.StateDone},
			wantContains: []string{"1 done"},
		},
		{
			name:         "a mix renders every non-empty segment",
			states:       []string{store.StateWaiting, store.StateThinking, store.StateThinking, store.StateDone, store.StateNew},
			wantContains: []string{"1 waiting", "2 working", "1 done"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			for i, state := range tt.states {
				seed(t, st, string(rune('a'+i)), "%1", state)
			}
			manager := newManager(newFakeTmux(), st, "")

			got := manager.Status()
			if tt.wantEmpty {
				assert.Empty(t, got)
				return
			}
			for _, want := range tt.wantContains {
				assert.Contains(t, got, want)
			}
		})
	}
}

func TestUniqueSessionName(t *testing.T) {
	tests := []struct {
		name     string
		title    string
		prompt   string
		taken    []string // names already alive in tmux
		wantName string
	}{
		{
			name:     "title collision appends a numeric suffix",
			title:    "agent",
			taken:    []string{"agent"},
			wantName: "agent-2",
		},
		{
			name:     "title with tmux-illegal chars is sanitized",
			title:    "My Agent: v1.0",
			wantName: "My-Agent--v1-0",
		},
		{
			name:     "blank title falls back to a prompt slug",
			prompt:   "Fix the login bug\nand more",
			wantName: "fix-the-login-bug",
		},
		{
			name:     "blank title and prompt fall back to agent",
			wantName: "agent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := newFakeTmux()
			for _, name := range tt.taken {
				tmux.alive[name] = true
			}
			manager := newManager(tmux, newStore(t), "")

			assert.Equal(t, tt.wantName, manager.uniqueSessionName(tt.title, tt.prompt))
		})
	}
}

// TestNameTaken covers both handles that reserve a name: a live tmux session and
// a live (non-deleted) store row whose tmux session is already gone.
func TestNameTaken(t *testing.T) {
	t.Run("a live tmux session reserves the name", func(t *testing.T) {
		tmux := newFakeTmux()
		tmux.alive["taken"] = true
		manager := newManager(tmux, newStore(t), "")

		assert.True(t, manager.nameTaken("taken"))
		assert.False(t, manager.nameTaken("free"))
	})

	t.Run("a dormant store row still reserves the name", func(t *testing.T) {
		st := newStore(t)
		seed(t, st, "dormant", "%1", store.StateDone)
		manager := newManager(newFakeTmux(), st, "")

		assert.True(t, manager.nameTaken("dormant"))
	})
}

func TestLaunchClaude(t *testing.T) {
	tests := []struct {
		name        string
		flags       string
		prompt      string
		sessionID   string
		resume      bool
		wantCommand string
	}{
		{
			name:        "resume pins the prior conversation by id",
			sessionID:   "sess-1",
			resume:      true,
			wantCommand: "claude --resume sess-1",
		},
		{
			name:        "new conversation pins a fresh session id",
			sessionID:   "sess-2",
			wantCommand: "claude --session-id sess-2",
		},
		{
			name:        "flags precede the session id",
			flags:       "--model opus",
			sessionID:   "sess-3",
			wantCommand: "claude --model opus --session-id sess-3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := newFakeTmux()
			manager := newManager(tmux, newStore(t), "")

			manager.launchClaude("session", tt.flags, tt.prompt, tt.sessionID, tt.resume)

			keys := tmux.lastKeys("session")
			require.Len(t, keys, 2)
			assert.Equal(t, tt.wantCommand, keys[0])
			assert.Equal(t, "Enter", keys[1])
		})
	}
}

// TestLaunchClaudePassesPromptThroughTempFile confirms an opening prompt is fed
// via a temp file that is removed as the command runs, so multi-line input
// survives the shell.
func TestLaunchClaudePassesPromptThroughTempFile(t *testing.T) {
	tmux := newFakeTmux()
	manager := newManager(tmux, newStore(t), "")

	manager.launchClaude("session", "", "line one\nline two", "sess", false)

	command := tmux.lastKeys("session")[0]
	assert.Contains(t, command, "--session-id sess")
	assert.Contains(t, command, "$(cat ")
	assert.Contains(t, command, "; rm ")
}

func TestEnvWithout(t *testing.T) {
	t.Setenv("CLAUDE_AGENTS_DROP_ME", "1")
	t.Setenv("CLAUDE_AGENTS_KEEP_ME", "2")

	env := envWithout("CLAUDE_AGENTS_DROP_ME")

	var kept bool
	for _, entry := range env {
		assert.False(t, strings.HasPrefix(entry, "CLAUDE_AGENTS_DROP_ME="), "dropped key must not survive")
		if entry == "CLAUDE_AGENTS_KEEP_ME=2" {
			kept = true
		}
	}
	assert.True(t, kept, "unrelated entries must be preserved")
}

func TestNewSession(t *testing.T) {
	tmux := newFakeTmux()
	tmux.clientDir = "/work"
	tmux.paneIDs["My-Agent"] = "%42"
	st := newStore(t)
	manager := newManager(tmux, st, "--default-flag")

	manager.NewSession(NewSessionParams{
		Client: "client",
		Title:  "My Agent",
		Flags:  "--extra",
		Prompt: "do the thing",
	})

	got, err := st.Get(context.Background(), "My-Agent")
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.Equal(t, "%42", got.Pane)
	assert.Equal(t, "/work", got.Cwd)
	assert.Equal(t, "--default-flag --extra", got.Flags)
	assert.Equal(t, "do the thing", got.Prompt)
	assert.Equal(t, store.StateNew, got.State)
	assert.NotEmpty(t, got.ClaudeSessionID, "a session id is generated at launch")
	assert.Equal(t, "/work", tmux.newSessions["My-Agent"])
	assert.NotEmpty(t, tmux.lastKeys("My-Agent"), "claude is launched into the new session")
}

// TestNewSessionExpandsTildeDir pins the fix for a `~`-prefixed directory: tmux
// does no tilde expansion, so the manager must resolve it to an absolute path
// before creating the session, and store that absolute path.
func TestNewSessionExpandsTildeDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	tmux := newFakeTmux()
	st := newStore(t)
	manager := newManager(tmux, st, "")

	manager.NewSession(NewSessionParams{Title: "scoped", Dir: "~/chariot/chariot"})

	want := filepath.Join(home, "chariot", "chariot")
	assert.Equal(t, want, tmux.newSessions["scoped"], "tmux is given the expanded path")

	got, err := st.Get(context.Background(), "scoped")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, want, got.Cwd, "the stored cwd is absolute")
}

func TestNewSessionScopesToWorkflow(t *testing.T) {
	tmux := newFakeTmux()
	tmux.clientDir = "/work"
	st := newStore(t)
	manager := newManager(tmux, st, "")

	workflow, err := manager.NewWorkflow("backend", "", "")
	require.NoError(t, err)

	manager.NewSession(NewSessionParams{Title: "scoped", WorkflowID: &workflow.ID})

	got, err := st.Get(context.Background(), "scoped")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, got.WorkflowID, "a chat created in a workflow carries its id")
	assert.Equal(t, workflow.ID, *got.WorkflowID)
}

func TestNewWorkflowDefaultsBlankName(t *testing.T) {
	manager := newManager(newFakeTmux(), newStore(t), "")

	workflow, err := manager.NewWorkflow("   ", "", "")
	require.NoError(t, err)
	require.NotNil(t, workflow)
	assert.Equal(t, "workflow", workflow.Name, "a blank name falls back to a default label")
}

func TestRenameWorkflow(t *testing.T) {
	t.Run("renames by id", func(t *testing.T) {
		st := newStore(t)
		manager := newManager(newFakeTmux(), st, "")
		workflow, err := manager.NewWorkflow("backend", "", "")
		require.NoError(t, err)

		manager.RenameWorkflow(*workflow, "platform")

		got, err := st.GetWorkflow(context.Background(), workflow.ID)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, "platform", got.Name)
	})

	t.Run("a blank or unchanged name is a no-op", func(t *testing.T) {
		st := newStore(t)
		manager := newManager(newFakeTmux(), st, "")
		workflow, err := manager.NewWorkflow("backend", "", "")
		require.NoError(t, err)

		manager.RenameWorkflow(*workflow, "   ")
		manager.RenameWorkflow(*workflow, "backend")

		got, err := st.GetWorkflow(context.Background(), workflow.ID)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, "backend", got.Name)
	})
}

// TestMoveChat covers reassigning a chat into a workflow and unassigning it back
// to the root with a nil target.
func TestMoveChat(t *testing.T) {
	st := newStore(t)
	manager := newManager(newFakeTmux(), st, "")
	workflow, err := manager.NewWorkflow("backend", "", "")
	require.NoError(t, err)
	agent := seed(t, st, "chat", "%1", store.StateNew)

	manager.MoveChat(agent, &workflow.ID)
	scoped, err := manager.ListWorkflows()
	require.NoError(t, err)
	require.Len(t, scoped, 1)
	inWorkflow, err := st.ListByWorkflow(context.Background(), workflow.ID)
	require.NoError(t, err)
	require.Len(t, inWorkflow, 1)
	assert.Equal(t, "chat", inWorkflow[0].Name)

	manager.MoveChat(agent, nil)
	inWorkflow, err = st.ListByWorkflow(context.Background(), workflow.ID)
	require.NoError(t, err)
	assert.Empty(t, inWorkflow, "a nil target unassigns the chat")
}

// TestDeleteWorkflowOrphansThroughManager confirms the manager wrapper preserves
// the store's orphan-on-delete guarantee.
func TestDeleteWorkflowOrphansThroughManager(t *testing.T) {
	st := newStore(t)
	manager := newManager(newFakeTmux(), st, "")
	workflow, err := manager.NewWorkflow("backend", "", "")
	require.NoError(t, err)
	manager.NewSession(NewSessionParams{Title: "chat", WorkflowID: &workflow.ID})

	manager.DeleteWorkflow(*workflow)

	workflows, err := manager.ListWorkflows()
	require.NoError(t, err)
	assert.Empty(t, workflows, "the workflow is hidden after deletion")

	got, err := st.Get(context.Background(), "chat")
	require.NoError(t, err)
	require.NotNil(t, got, "its chat survives")
	assert.Nil(t, got.WorkflowID, "orphaned back to unassigned")
}

// TestDeleteWorkflowWithChatsThroughManager confirms the purge path soft-deletes
// the workflow's chats instead of orphaning them, while keeping them findable in
// the all-inclusive search listing.
func TestDeleteWorkflowWithChatsThroughManager(t *testing.T) {
	st := newStore(t)
	manager := newManager(newFakeTmux(), st, "")
	workflow, err := manager.NewWorkflow("backend", "", "")
	require.NoError(t, err)
	manager.NewSession(NewSessionParams{Title: "chat", WorkflowID: &workflow.ID})

	manager.DeleteWorkflowWithChats(*workflow)

	got, err := st.Get(context.Background(), "chat")
	require.NoError(t, err)
	assert.Nil(t, got, "the chat is hidden from the live listing")

	all, err := st.ListAll(context.Background())
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.True(t, all[0].Deleted, "the chat is soft-deleted, not orphaned")
}

func TestWorkflowSearchCandidatesScoped(t *testing.T) {
	st := newStore(t)
	manager := newManager(newFakeTmux(), st, "")
	workflow, err := manager.NewWorkflow("backend", "", "")
	require.NoError(t, err)
	require.NoError(t, st.Upsert(context.Background(), store.Agent{Name: "scoped", State: store.StateNew, WorkflowID: &workflow.ID}))
	require.NoError(t, st.Upsert(context.Background(), store.Agent{Name: "unscoped", State: store.StateNew}))

	scoped, err := manager.WorkflowSearchCandidates(workflow.ID)
	require.NoError(t, err)
	require.Len(t, scoped, 1)
	assert.Equal(t, "scoped", scoped[0].Name)

	all, err := manager.SearchCandidates()
	require.NoError(t, err)
	assert.Len(t, all, 2, "the global candidates span every chat")
}

func TestNotify(t *testing.T) {
	t.Run("marks the current session done when no session is given", func(t *testing.T) {
		st := newStore(t)
		seed(t, st, "current", "%1", store.StateThinking)
		tmux := newFakeTmux()
		tmux.current = "current"
		manager := newManager(tmux, st, "")

		manager.Notify("", "%1", "message")
		assert.Equal(t, store.StateDone, stateOf(t, st, "current"))
	})

	t.Run("an explicit main session takes precedence", func(t *testing.T) {
		st := newStore(t)
		seed(t, st, "main", "%1", store.StateThinking)
		tmux := newFakeTmux()
		tmux.current = "other"
		manager := newManager(tmux, st, "")

		manager.Notify("main", "%1", "")
		assert.Equal(t, store.StateDone, stateOf(t, st, "main"))
	})
}

func TestRename(t *testing.T) {
	t.Run("renames the tmux session and store row", func(t *testing.T) {
		st := newStore(t)
		agent := seed(t, st, "old", "%1", store.StateNew)
		tmux := newFakeTmux()
		manager := newManager(tmux, st, "")

		manager.Rename(agent, "New Name")

		require.Len(t, tmux.renamed, 1)
		assert.Equal(t, [2]string{"old", "New-Name"}, tmux.renamed[0])
		got, _ := st.Get(context.Background(), "New-Name")
		require.NotNil(t, got)
		assert.Equal(t, agent.ID, got.ID)
	})

	t.Run("a blank or unchanged name is a no-op", func(t *testing.T) {
		st := newStore(t)
		agent := seed(t, st, "same", "%1", store.StateNew)
		tmux := newFakeTmux()
		manager := newManager(tmux, st, "")

		manager.Rename(agent, "   ")
		manager.Rename(agent, "same")
		assert.Empty(t, tmux.renamed)
	})
}

func TestDelete(t *testing.T) {
	t.Run("kills the session and soft-deletes the row", func(t *testing.T) {
		st := newStore(t)
		agent := seed(t, st, "target", "%1", store.StateThinking)
		tmux := newFakeTmux()
		tmux.clientSes = "target"
		manager := newManager(tmux, st, "")

		manager.Delete("client", agent)

		assert.Contains(t, tmux.killed, "target")
		assert.Contains(t, tmux.switched, "client", "a client viewing the target is bounced away first")
		got, _ := st.Get(context.Background(), "target")
		assert.Nil(t, got, "the row is hidden from listings")
	})

	t.Run("does not switch a client that is elsewhere", func(t *testing.T) {
		st := newStore(t)
		agent := seed(t, st, "target", "%1", store.StateThinking)
		tmux := newFakeTmux()
		tmux.clientSes = "somewhere-else"
		manager := newManager(tmux, st, "")

		manager.Delete("client", agent)
		assert.Empty(t, tmux.switched)
	})
}

// TestPreviewResizesOnlyDetached confirms a preview capture resizes a detached
// agent's window to fill the box, but leaves an attached session alone so a live
// client's view is never disturbed.
func TestPreviewResizesOnlyDetached(t *testing.T) {
	t.Run("detached agent is resized before capture", func(t *testing.T) {
		tmux := newFakeTmux()
		tmux.captures["%1"] = "frame"
		manager := newManager(tmux, newStore(t), "")

		got := manager.Preview(store.Agent{Name: "a", Pane: "%1"}, 80, 24)
		assert.Equal(t, "frame", got)
		assert.Equal(t, [2]int{80, 24}, tmux.resized["a"])
	})

	t.Run("attached session is left alone", func(t *testing.T) {
		tmux := newFakeTmux()
		tmux.attached["a"] = true
		manager := newManager(tmux, newStore(t), "")

		manager.Preview(store.Agent{Name: "a", Pane: "%1"}, 80, 24)
		assert.NotContains(t, tmux.resized, "a")
	})
}

// TestScrollPreviewGuards confirms scrolling is a no-op without a pane or box,
// and for an attached session, and otherwise sends wheel events to the pane.
func TestScrollPreviewGuards(t *testing.T) {
	tests := []struct {
		name       string
		agent      store.Agent
		width      int
		height     int
		attached   bool
		wantScroll bool
	}{
		{
			name:   "no pane does not scroll",
			agent:  store.Agent{Name: "a"},
			width:  80,
			height: 24,
		},
		{
			name:   "zero box does not scroll",
			agent:  store.Agent{Name: "a", Pane: "%1"},
			width:  0,
			height: 24,
		},
		{
			name:     "attached session does not scroll",
			agent:    store.Agent{Name: "a", Pane: "%1"},
			width:    80,
			height:   24,
			attached: true,
		},
		{
			name:       "detached agent with a box scrolls",
			agent:      store.Agent{Name: "a", Pane: "%1"},
			width:      80,
			height:     24,
			wantScroll: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := newFakeTmux()
			tmux.attached["a"] = tt.attached
			manager := newManager(tmux, newStore(t), "")

			manager.ScrollPreview(tt.agent, tt.width, tt.height, true)
			assert.Equal(t, tt.wantScroll, len(tmux.scrolls) == 1)
		})
	}
}
