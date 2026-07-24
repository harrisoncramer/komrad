package agents

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
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

// TestNameTaken covers name reservation. A live store row owns its name.
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

			manager.launchClaude(launchClaudeParams{
				Session:   "session",
				Flags:     tt.flags,
				Prompt:    tt.prompt,
				SessionID: tt.sessionID,
				Resume:    tt.resume,
			})

			keys := tmux.lastKeys("session")
			require.Len(t, keys, 2)
			// Every launch also carries the MCP config, and a fresh one appends a
			// system prompt, so the expectation covers the rest of the line.
			command := stripMCPConfig(keys[0])
			assert.True(t, strings.HasPrefix(command, tt.wantCommand), "want %q to start with %q", command, tt.wantCommand)
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

	manager.launchClaude(launchClaudeParams{
		Session:   "session",
		Prompt:    "line one\nline two",
		SessionID: "sess",
	})

	command := tmux.lastKeys("session")[0]
	assert.Contains(t, command, "--session-id sess")
	assert.Contains(t, command, "$(cat ")
	assert.Contains(t, command, "; rm ")
	// The prompt is positional and --mcp-config is variadic, so the two must not
	// be able to run together.
	assert.Contains(t, command, " -- \"$(cat ")
}

// mcpConfigRef matches the --mcp-config flag and the path it points at.
var mcpConfigRef = regexp.MustCompile(` --mcp-config '([^']+)'`)

// stripMCPConfig removes the MCP config flag from a launch command, so the
// assertions about the rest of the line are not written around a path that
// varies by machine.
func stripMCPConfig(command string) string {
	return mcpConfigRef.ReplaceAllString(command, "")
}

// appendedPromptRef matches the temp file the launch command sources its
// appended system prompt from.
var appendedPromptRef = regexp.MustCompile(`--append-system-prompt "\$\(cat '([^']+)'`)

// appendedPrompt returns the system prompt text the command appends. The shell
// removes the file as the command runs, but a test never runs it, so the file is
// still on disk to read.
func appendedPrompt(t *testing.T, command string) string {
	t.Helper()

	match := appendedPromptRef.FindStringSubmatch(command)
	require.Len(t, match, 2, "command should append a system prompt: %s", command)

	text, err := os.ReadFile(match[1])
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(match[1]) })

	return string(text)
}

// TestLaunchClaudeInjectsWorkflowContext confirms a non-empty workflow context
// is appended as a system prompt through the temp-file trick, and that resuming
// a session skips it since --append-system-prompt does not persist.
func TestLaunchClaudeInjectsWorkflowContext(t *testing.T) {
	tmux := newFakeTmux()
	manager := newManager(tmux, newStore(t), "")

	manager.launchClaude(launchClaudeParams{
		Session:         "session",
		WorkflowContext: "shared notes",
		SessionID:       "sess",
	})
	command := tmux.lastKeys("session")[0]
	assert.Contains(t, command, "; rm ")
	assert.Contains(t, appendedPrompt(t, command), "shared notes")

	tmux = newFakeTmux()
	manager = newManager(tmux, newStore(t), "")
	manager.launchClaude(launchClaudeParams{
		Session:         "session",
		WorkflowContext: "shared notes",
		SessionID:       "sess",
		Resume:          true,
	})
	assert.NotContains(t, tmux.lastKeys("session")[0], "--append-system-prompt")
}

// TestLaunchClaudeBriefsOnMCP confirms every fresh launch tells the agent where
// the MCP endpoint is, whether or not it belongs to a workflow, and that the
// address comes from the configured listen address rather than a fixed one.
func TestLaunchClaudeBriefsOnMCP(t *testing.T) {
	tmux := newFakeTmux()
	manager := NewAgentManager(Params{Tmux: tmux, Store: newStore(t), SelfPath: "self", ServerAddr: "127.0.0.1:9999"})

	manager.launchClaude(launchClaudeParams{Session: "session", SessionID: "sess"})

	prompt := appendedPrompt(t, tmux.lastKeys("session")[0])
	assert.Contains(t, prompt, "http://127.0.0.1:9999/mcp")
	assert.Contains(t, prompt, "list_tables")
	assert.Contains(t, prompt, "session_transcripts")
}

// TestLaunchClaudeRegistersMCPServer confirms every launch, resumed or fresh,
// points claude at a config file registering the panel's endpoint, and that the
// file says what it should.
func TestLaunchClaudeRegistersMCPServer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	tmux := newFakeTmux()
	manager := NewAgentManager(Params{Tmux: tmux, Store: newStore(t), SelfPath: "self", ServerAddr: "127.0.0.1:9999"})

	for _, resume := range []bool{false, true} {
		manager.launchClaude(launchClaudeParams{Session: "session", SessionID: "sess", Resume: resume})

		match := mcpConfigRef.FindStringSubmatch(tmux.lastKeys("session")[0])
		require.Len(t, match, 2, "resume=%v should carry the mcp config", resume)
		assert.Equal(t, filepath.Join(home, ".claude", "claude-agents-mcp.json"), match[1])

		raw, err := os.ReadFile(match[1])
		require.NoError(t, err)

		var config struct {
			MCPServers map[string]struct {
				Type string `json:"type"`
				URL  string `json:"url"`
			} `json:"mcpServers"`
		}
		require.NoError(t, json.Unmarshal(raw, &config))

		server, ok := config.MCPServers["claude-agents"]
		require.True(t, ok, "the panel's own server should be registered")
		assert.Equal(t, "http", server.Type)
		assert.Equal(t, "http://127.0.0.1:9999/mcp", server.URL)
	}
}

// TestLaunchClaudeKeepsOtherMCPServers confirms the launch does not pass
// --strict-mcp-config, which would trade the user's own registered servers for
// this one instead of adding to them.
func TestLaunchClaudeKeepsOtherMCPServers(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	tmux := newFakeTmux()
	manager := newManager(tmux, newStore(t), "")

	manager.launchClaude(launchClaudeParams{Session: "session", SessionID: "sess"})

	assert.NotContains(t, tmux.lastKeys("session")[0], "--strict-mcp-config")
}

// TestAppendedSystemPromptOrdersBriefingFirst confirms the workflow's own
// context follows the briefing rather than being buried in it.
func TestAppendedSystemPromptOrdersBriefingFirst(t *testing.T) {
	manager := newManager(newFakeTmux(), newStore(t), "")

	prompt := manager.appendedSystemPrompt("shared notes")
	assert.Less(t, strings.Index(prompt, "claude-agents"), strings.Index(prompt, "shared notes"))

	// A chat outside any workflow gets the briefing alone, with no trailing blank.
	assert.Equal(t, manager.appendedSystemPrompt(""), strings.TrimSpace(manager.appendedSystemPrompt("   ")))
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

// TestNewSessionExpandsTildeDir checks a ~-prefixed directory. tmux does no tilde
// expansion, so the manager must resolve it to an absolute path before creating
// the session, and store that absolute path.
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
// workflow launches with the workflow's context appended to its system prompt,
// while one created outside any workflow gets the MCP briefing only.
func TestNewSessionInjectsWorkflowContext(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	manager := newManager(tmux, st, "")

	workflow, err := manager.NewWorkflow(store.WorkflowConfig{Name: "backend", Context: "shared notes"})
	require.NoError(t, err)

	manager.NewSession(NewSessionParams{Title: "scoped", WorkflowID: &workflow.ID})
	assert.Contains(t, appendedPrompt(t, tmux.lastKeys("%scoped")[0]), "shared notes")

	manager.NewSession(NewSessionParams{Title: "rootless"})
	rootless := appendedPrompt(t, tmux.lastKeys("%rootless")[0])
	assert.NotContains(t, rootless, "shared notes")
	assert.Contains(t, rootless, "/mcp")
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

func TestRename(t *testing.T) {
	t.Run("renames the store row only", func(t *testing.T) {
		st := newStore(t)
		agent := seed(t, st, "old", "%1", store.StateNew)
		tmux := newFakeTmux()
		manager := newManager(tmux, st, "")

		manager.Rename(agent, "New Name")

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
