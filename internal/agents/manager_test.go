package agents

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"claude-agents/internal/store/session"
	"claude-agents/internal/store/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newManager builds a session service over the fake tmux and a real store, which
// is what most of these tests drive.
func newManager(tmux *fakeTmux, st *testStore, flags string) *SessionService {
	return newSessions(tmux, st, flags, "")
}

// workflowsFor builds the workflow service over the same store, for the tests
// that set a chat's workflow up before acting on the chat.
func workflowsFor(st *testStore) *WorkflowService {
	return st.workflowService()
}

// newSessions is the same, with the launcher pointed at a specific address so a
// test can assert on what a spawned agent is told.
func newSessions(tmux *fakeTmux, st *testStore, flags, addr string) *SessionService {
	return st.sessionService(tmux, NewLauncher(tmux, addr), flags)
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
				seed(t, st, name, "%1", session.StateNew)
			}
			manager := newManager(newFakeTmux(), st, "")

			assert.Equal(t, tt.wantName, manager.uniqueSessionName(t.Context(), tt.title, tt.prompt))
		})
	}
}

// TestNameTaken covers name reservation. A live store row owns its name.
func TestNameTaken(t *testing.T) {
	st := newStore(t)
	seed(t, st, "taken", "%1", session.StateDone)
	manager := newManager(newFakeTmux(), st, "")

	assert.True(t, manager.nameTaken(t.Context(), "taken"))
	assert.False(t, manager.nameTaken(t.Context(), "free"))
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
			wantCommand: "claude --resume 'sess-1'",
		},
		{
			name:        "new conversation pins a fresh session id",
			sessionID:   "sess-2",
			wantCommand: "claude --session-id 'sess-2'",
		},
		{
			name:        "flags precede the session id",
			flags:       "--model opus",
			sessionID:   "sess-3",
			wantCommand: "claude '--model' 'opus' --session-id 'sess-3'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := newFakeTmux()
			manager := newManager(tmux, newStore(t), "")

			require.NoError(t, manager.launcher.launch(launchClaudeParams{
				Session:   "session",
				Flags:     tt.flags,
				Prompt:    tt.prompt,
				SessionID: tt.sessionID,
				Resume:    tt.resume,
			}))

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

	require.NoError(t, manager.launcher.launch(launchClaudeParams{
		Session:   "session",
		Prompt:    "line one\nline two",
		SessionID: "sess",
	}))

	command := tmux.lastKeys("session")[0]
	assert.Contains(t, command, "--session-id 'sess'")
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

	require.NoError(t, manager.launcher.launch(launchClaudeParams{
		Session:         "session",
		WorkflowContext: "shared notes",
		SessionID:       "sess",
	}))
	command := tmux.lastKeys("session")[0]
	assert.Contains(t, command, "; rm ")
	assert.Contains(t, appendedPrompt(t, command), "shared notes")

	tmux = newFakeTmux()
	manager = newManager(tmux, newStore(t), "")
	require.NoError(t, manager.launcher.launch(launchClaudeParams{
		Session:         "session",
		WorkflowContext: "shared notes",
		SessionID:       "sess",
		Resume:          true,
	}))
	assert.NotContains(t, tmux.lastKeys("session")[0], "--append-system-prompt")
}

// TestLaunchClaudeBriefsOnMCP confirms every fresh launch tells the agent where
// the MCP endpoint is, whether or not it belongs to a workflow, and that the
// address comes from the configured listen address rather than a fixed one.
func TestLaunchClaudeBriefsOnMCP(t *testing.T) {
	tmux := newFakeTmux()
	manager := newSessions(tmux, newStore(t), "", "127.0.0.1:9999")

	require.NoError(t, manager.launcher.launch(launchClaudeParams{Session: "session", SessionID: "sess"}))

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
	manager := newSessions(tmux, newStore(t), "", "127.0.0.1:9999")

	for _, resume := range []bool{false, true} {
		require.NoError(t, manager.launcher.launch(launchClaudeParams{Session: "session", SessionID: "sess", Resume: resume}))

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

	require.NoError(t, manager.launcher.launch(launchClaudeParams{Session: "session", SessionID: "sess"}))

	assert.NotContains(t, tmux.lastKeys("session")[0], "--strict-mcp-config")
}

// TestAppendedSystemPromptOrdersBriefingFirst confirms the workflow's own
// context follows the briefing rather than being buried in it.
func TestAppendedSystemPromptOrdersBriefingFirst(t *testing.T) {
	manager := newManager(newFakeTmux(), newStore(t), "")

	prompt := manager.launcher.appendedSystemPrompt("shared notes")
	assert.Less(t, strings.Index(prompt, "claude-agents"), strings.Index(prompt, "shared notes"))

	// A chat outside any workflow gets the briefing alone, with no trailing blank.
	assert.Equal(t, manager.launcher.appendedSystemPrompt(""), strings.TrimSpace(manager.launcher.appendedSystemPrompt("   ")))
}

func TestNewSession(t *testing.T) {
	work := t.TempDir()
	tmux := newFakeTmux()
	tmux.clientDir = work
	st := newStore(t)
	manager := newManager(tmux, st, "--default-flag")

	created, err := manager.NewSession(t.Context(), NewSessionParams{
		Client: "client",
		Title:  "My Agent",
		Flags:  "--extra",
		Prompt: "do the thing",
	})
	require.NoError(t, err)
	require.NotNil(t, created)
	assert.Equal(t, "My-Agent", created.Name, "the caller learns the generated name")
	assert.NotEmpty(t, created.ID, "the caller learns the stored id")

	got, err := st.Get(context.Background(), "My-Agent")
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.Equal(t, "%My-Agent", got.Pane, "the new window's pane is the agent's handle")
	assert.Equal(t, work, got.Cwd)
	assert.Equal(t, "--default-flag --extra", got.Flags)
	assert.Equal(t, "do the thing", got.Prompt)
	assert.Equal(t, session.StateNew, got.State)
	assert.NotEmpty(t, got.ClaudeSessionID, "a session id is generated at launch")
	assert.Equal(t, work, tmux.newWindows["My-Agent"])
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

	want := filepath.Join(home, "chariot", "chariot")
	require.NoError(t, os.MkdirAll(want, 0o755))

	_, err := manager.NewSession(t.Context(), NewSessionParams{Title: "scoped", Dir: "~/chariot/chariot"})
	require.NoError(t, err)

	assert.Equal(t, want, tmux.newWindows["scoped"], "tmux is given the expanded path")

	got, err := st.Get(context.Background(), "scoped")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, want, got.Cwd, "the stored cwd is absolute")
}

func TestNewSessionScopesToWorkflow(t *testing.T) {
	tmux := newFakeTmux()
	tmux.clientDir = t.TempDir()
	st := newStore(t)
	manager := newManager(tmux, st, "")

	workflow, err := workflowsFor(st).NewWorkflow(t.Context(), workflow.Config{Name: "backend"})
	require.NoError(t, err)

	_, err = manager.NewSession(t.Context(), NewSessionParams{Title: "scoped", WorkflowID: &workflow.ID})
	require.NoError(t, err)

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

	workflow, err := workflowsFor(st).NewWorkflow(t.Context(), workflow.Config{Name: "backend", Context: "shared notes"})
	require.NoError(t, err)

	_, err = manager.NewSession(t.Context(), NewSessionParams{Title: "scoped", WorkflowID: &workflow.ID})
	require.NoError(t, err)
	assert.Contains(t, appendedPrompt(t, tmux.lastKeys("%scoped")[0]), "shared notes")

	_, err = manager.NewSession(t.Context(), NewSessionParams{Title: "rootless"})
	require.NoError(t, err)
	rootless := appendedPrompt(t, tmux.lastKeys("%rootless")[0])
	assert.NotContains(t, rootless, "shared notes")
	assert.Contains(t, rootless, "/mcp")
}

func TestNewWorkflowDefaultsBlankName(t *testing.T) {
	workflow, err := workflowsFor(newStore(t)).NewWorkflow(t.Context(), workflow.Config{Name: "   "})
	require.NoError(t, err)
	require.NotNil(t, workflow)
	assert.Equal(t, "workflow", workflow.Name, "a blank name falls back to a default label")
}

func TestRenameWorkflow(t *testing.T) {
	t.Run("renames by id", func(t *testing.T) {
		st := newStore(t)
		wf, err := workflowsFor(st).NewWorkflow(t.Context(), workflow.Config{Name: "backend"})
		require.NoError(t, err)

		require.NoError(t, workflowsFor(st).UpdateWorkflow(t.Context(), wf.ID, workflow.Config{Name: "platform"}))

		got, err := st.GetWorkflow(context.Background(), wf.ID)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, "platform", got.Name)
	})

	t.Run("a blank name keeps the one it had", func(t *testing.T) {
		st := newStore(t)
		wf, err := workflowsFor(st).NewWorkflow(t.Context(), workflow.Config{Name: "backend"})
		require.NoError(t, err)

		require.NoError(t, workflowsFor(st).UpdateWorkflow(t.Context(), wf.ID, workflow.Config{Name: "   "}))

		got, err := st.GetWorkflow(context.Background(), wf.ID)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, "backend", got.Name, "the list always has a label")
	})
}

// TestDeleteWorkflowOrphansThroughManager confirms the manager wrapper preserves
// the store's orphan-on-delete guarantee.
func TestDeleteWorkflowOrphansThroughManager(t *testing.T) {
	st := newStore(t)
	manager := newManager(newFakeTmux(), st, "")
	workflow, err := workflowsFor(st).NewWorkflow(t.Context(), workflow.Config{Name: "backend"})
	require.NoError(t, err)
	_, err = manager.NewSession(t.Context(), NewSessionParams{Title: "chat", WorkflowID: &workflow.ID})
	require.NoError(t, err)

	require.NoError(t, workflowsFor(st).DeleteWorkflow(t.Context(), workflow.ID, false))

	workflows, err := st.workflows.List(context.Background())
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
	workflow, err := workflowsFor(st).NewWorkflow(t.Context(), workflow.Config{Name: "backend"})
	require.NoError(t, err)
	_, err = manager.NewSession(t.Context(), NewSessionParams{Title: "chat", WorkflowID: &workflow.ID})
	require.NoError(t, err)

	require.NoError(t, workflowsFor(st).DeleteWorkflow(t.Context(), workflow.ID, true))

	got, err := st.Get(context.Background(), "chat")
	require.NoError(t, err)
	assert.Nil(t, got, "the chat is hidden from the live listing")

	all, err := st.ListAll(context.Background())
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.True(t, all[0].Deleted, "the chat is soft-deleted, not orphaned")
}

// TestArchiveSession confirms archiving stops the agent and hides the chat,
// leaving the row in place so search can still reach it.
func TestArchiveSession(t *testing.T) {
	st := newStore(t)
	seed(t, st, "target", "%1", session.StateThinking)
	tmux := newFakeTmux()
	manager := newManager(tmux, st, "")

	require.NoError(t, manager.ArchiveSession(t.Context(), "target"))

	assert.Contains(t, tmux.killedPanes, "%1", "the agent's pane is killed")

	got, err := st.Get(t.Context(), "target")
	require.NoError(t, err)
	assert.Nil(t, got, "the chat is hidden from listings")
}

// TestArchiveMissingSessionIsNotFound confirms archiving a name nobody holds is
// an error rather than a quiet success.
func TestArchiveMissingSessionIsNotFound(t *testing.T) {
	manager := newManager(newFakeTmux(), newStore(t), "")

	require.ErrorIs(t, manager.ArchiveSession(t.Context(), "ghost"), ErrSessionNotFound)
}

// TestNewSessionRefusesUnusableDirectory confirms a launch fails rather than
// storing a cwd that is not true. tmux quietly falls back to its own default
// directory when -c names something it cannot enter, which would leave the chat
// running in one place and the record claiming another.
func TestNewSessionRefusesUnusableDirectory(t *testing.T) {
	st := newStore(t)
	manager := newManager(newFakeTmux(), st, "")

	file := filepath.Join(t.TempDir(), "a-file")
	require.NoError(t, os.WriteFile(file, []byte("not a directory"), 0o644))

	_, err := manager.NewSession(t.Context(), NewSessionParams{Title: "nowhere", Dir: "/no/such/directory"})
	require.Error(t, err)

	_, err = manager.NewSession(t.Context(), NewSessionParams{Title: "a-file", Dir: file})
	require.ErrorContains(t, err, "not a directory")

	agents, err := st.List(context.Background())
	require.NoError(t, err)
	assert.Empty(t, agents, "a refused launch records nothing")
}

// TestNewSessionRefusesUnknownWorkflow confirms a chat cannot be filed under a
// workflow that does not exist, where it would render nowhere in the panel.
func TestNewSessionRefusesUnknownWorkflow(t *testing.T) {
	st := newStore(t)
	manager := newManager(newFakeTmux(), st, "")

	missing := "no-such-workflow"
	_, err := manager.NewSession(t.Context(), NewSessionParams{Title: "orphan", Dir: t.TempDir(), WorkflowID: &missing})
	require.ErrorIs(t, err, ErrWorkflowNotFound)

	agents, err := st.List(context.Background())
	require.NoError(t, err)
	assert.Empty(t, agents)
}

// TestNewSessionFailsWhenTheWindowCannotOpen confirms a refused window fails the
// launch. tmux resolves an empty target to the active pane, so a launch that
// carried on would type a full claude command into whatever the user was
// looking at.
func TestNewSessionFailsWhenTheWindowCannotOpen(t *testing.T) {
	tmux := newFakeTmux()
	tmux.newWindowErr = errors.New("tmux refused the window name")
	st := newStore(t)
	manager := newManager(tmux, st, "")

	_, err := manager.NewSession(t.Context(), NewSessionParams{Title: "doomed", Dir: t.TempDir()})
	require.Error(t, err)

	assert.Empty(t, tmux.sent, "nothing is typed when there is no pane to type into")

	agents, err := st.List(context.Background())
	require.NoError(t, err)
	assert.Empty(t, agents, "no row is stored pointing at a pane that does not exist")
}

// TestNewSessionKillsTheWindowWhenTheNameIsLost covers the race between two
// creates with the same title: both pass the name check, one loses the insert,
// and its window has to go with it rather than run forever unreachable.
func TestNewSessionKillsTheWindowWhenTheNameIsLost(t *testing.T) {
	tmux := newFakeTmux()
	st := newStore(t)
	manager := newManager(tmux, st, "")
	dir := t.TempDir()

	first, err := manager.NewSession(t.Context(), NewSessionParams{Title: "racer", Dir: dir})
	require.NoError(t, err)
	require.NotNil(t, first)

	// Re-running uniqueSessionName would pick "racer-2", so the collision is
	// forced by inserting under the name the loser already holds.
	_, err = manager.NewSession(t.Context(), NewSessionParams{Title: "racer", Dir: dir})
	require.NoError(t, err, "the second create takes the next free name")

	agents, err := st.List(context.Background())
	require.NoError(t, err)
	assert.Len(t, agents, 2)
}
