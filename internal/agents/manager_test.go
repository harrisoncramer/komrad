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
		taken    []string // names already owned by a live store row
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
			st := newStore(t)
			for _, name := range tt.taken {
				seed(t, st, name, "%1", store.StateNew)
			}
			manager := newManager(newFakeTmux(), st, "")

			assert.Equal(t, tt.wantName, manager.uniqueSessionName(tt.title, tt.prompt))
		})
	}
}

// TestNameTaken covers name reservation: a live (non-deleted) store row owns its
// name, since the name is a store-only label now.
func TestNameTaken(t *testing.T) {
	st := newStore(t)
	seed(t, st, "taken", "%1", store.StateDone)
	manager := newManager(newFakeTmux(), st, "")

	assert.True(t, manager.nameTaken("taken"))
	assert.False(t, manager.nameTaken("free"))
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

			manager.launchClaude("session", tt.flags, tt.prompt, "", tt.sessionID, tt.resume)

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

	manager.launchClaude("session", "", "line one\nline two", "", "sess", false)

	command := tmux.lastKeys("session")[0]
	assert.Contains(t, command, "--session-id sess")
	assert.Contains(t, command, "$(cat ")
	assert.Contains(t, command, "; rm ")
}

// TestLaunchClaudeInjectsWorkflowContext confirms a non-empty workflow context
// is appended as a system prompt through the same temp-file trick, and that
// resuming a session skips it since --append-system-prompt does not persist.
func TestLaunchClaudeInjectsWorkflowContext(t *testing.T) {
	tmux := newFakeTmux()
	manager := newManager(tmux, newStore(t), "")

	manager.launchClaude("session", "", "", "shared notes", "sess", false)
	command := tmux.lastKeys("session")[0]
	assert.Contains(t, command, "--append-system-prompt \"$(cat ")
	assert.Contains(t, command, "; rm ")

	tmux = newFakeTmux()
	manager = newManager(tmux, newStore(t), "")
	manager.launchClaude("session", "", "", "shared notes", "sess", true)
	assert.NotContains(t, tmux.lastKeys("session")[0], "--append-system-prompt")
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

	assert.Equal(t, "%My-Agent", got.Pane, "the new window's pane is the agent's handle")
	assert.Equal(t, "/work", got.Cwd)
	assert.Equal(t, "--default-flag --extra", got.Flags)
	assert.Equal(t, "do the thing", got.Prompt)
	assert.Equal(t, store.StateNew, got.State)
	assert.NotEmpty(t, got.ClaudeSessionID, "a session id is generated at launch")
	assert.Equal(t, "/work", tmux.newWindows["My-Agent"])
	assert.NotEmpty(t, tmux.lastKeys("%My-Agent"), "claude is launched into the new pane")
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
	assert.Equal(t, want, tmux.newWindows["scoped"], "tmux is given the expanded path")

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

	workflow, err := manager.NewWorkflow(store.WorkflowConfig{Name: "backend"})
	require.NoError(t, err)

	manager.NewSession(NewSessionParams{Title: "scoped", WorkflowID: &workflow.ID})

	got, err := st.Get(context.Background(), "scoped")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, got.WorkflowID, "a chat created in a workflow carries its id")
	assert.Equal(t, workflow.ID, *got.WorkflowID)
}

// TestNewSessionInjectsWorkflowContext confirms a chat spawned inside a
// workflow launches with the workflow's context appended as a system prompt,
// while one created outside any workflow does not.
func TestNewSessionInjectsWorkflowContext(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	manager := newManager(tmux, st, "")

	workflow, err := manager.NewWorkflow(store.WorkflowConfig{Name: "backend", Context: "shared notes"})
	require.NoError(t, err)

	manager.NewSession(NewSessionParams{Title: "scoped", WorkflowID: &workflow.ID})
	assert.Contains(t, tmux.lastKeys("%scoped")[0], "--append-system-prompt")

	manager.NewSession(NewSessionParams{Title: "rootless"})
	assert.NotContains(t, tmux.lastKeys("%rootless")[0], "--append-system-prompt")
}

func TestNewWorkflowDefaultsBlankName(t *testing.T) {
	manager := newManager(newFakeTmux(), newStore(t), "")

	workflow, err := manager.NewWorkflow(store.WorkflowConfig{Name: "   "})
	require.NoError(t, err)
	require.NotNil(t, workflow)
	assert.Equal(t, "workflow", workflow.Name, "a blank name falls back to a default label")
}

func TestRenameWorkflow(t *testing.T) {
	t.Run("renames by id", func(t *testing.T) {
		st := newStore(t)
		manager := newManager(newFakeTmux(), st, "")
		workflow, err := manager.NewWorkflow(store.WorkflowConfig{Name: "backend"})
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
		workflow, err := manager.NewWorkflow(store.WorkflowConfig{Name: "backend"})
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
	workflow, err := manager.NewWorkflow(store.WorkflowConfig{Name: "backend"})
	require.NoError(t, err)
	agent := seed(t, st, "chat", "%1", store.StateNew)

	manager.MoveChat(agent, &workflow.ID)
	scoped, err := manager.ListWorkflows()
	require.NoError(t, err)
	require.Len(t, scoped, 1)
	moved, err := st.Get(context.Background(), "chat")
	require.NoError(t, err)
	require.NotNil(t, moved.WorkflowID)
	assert.Equal(t, workflow.ID, *moved.WorkflowID)

	manager.MoveChat(agent, nil)
	unassigned, err := st.Get(context.Background(), "chat")
	require.NoError(t, err)
	assert.Nil(t, unassigned.WorkflowID, "a nil target unassigns the chat")
}

// TestDeleteWorkflowOrphansThroughManager confirms the manager wrapper preserves
// the store's orphan-on-delete guarantee.
func TestDeleteWorkflowOrphansThroughManager(t *testing.T) {
	st := newStore(t)
	manager := newManager(newFakeTmux(), st, "")
	workflow, err := manager.NewWorkflow(store.WorkflowConfig{Name: "backend"})
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
	workflow, err := manager.NewWorkflow(store.WorkflowConfig{Name: "backend"})
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
	t.Run("renames the store row only", func(t *testing.T) {
		st := newStore(t)
		agent := seed(t, st, "old", "%1", store.StateNew)
		tmux := newFakeTmux()
		manager := newManager(tmux, st, "")

		manager.Rename(agent, "New Name")

		assert.Empty(t, tmux.renamed, "the name is a store-only label; no tmux object is renamed")
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
		got, _ := st.Get(context.Background(), "same")
		require.NotNil(t, got, "the row keeps its name")
	})
}

func TestDelete(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "target", "%1", store.StateThinking)
	tmux := newFakeTmux()
	manager := newManager(tmux, st, "")

	manager.Delete(agent)

	assert.Contains(t, tmux.killedPanes, "%1", "the agent's pane is killed")
	got, _ := st.Get(context.Background(), "target")
	assert.Nil(t, got, "the row is hidden from listings")
}
