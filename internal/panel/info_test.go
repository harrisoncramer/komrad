package panel

import (
	"strings"
	"testing"
	"time"

	"claude-agents/internal/agents"
	"claude-agents/internal/appconfig"
	"claude-agents/internal/store"

	tea "github.com/charmbracelet/bubbletea"
)

// stageTmux is a tmux client that records only what the stage does, so a test
// can tell a swapped-in chat pane from a respawned bat preview. Every other call
// is an inert stub.
type stageTmux struct {
	swaps    int
	respawns []string
}

func (t *stageTmux) SetGlobal(string, string)                {}
func (t *stageTmux) ShowGlobal(string) string                { return "" }
func (t *stageTmux) SetSessionOption(string, string, string) {}
func (t *stageTmux) DetachSession(string)                    {}
func (t *stageTmux) ClientPath(string) string                { return "" }
func (t *stageTmux) SendKeys(string, ...string) error        { return nil }
func (t *stageTmux) PaneAlive(string) bool                   { return true }
func (t *stageTmux) CapturePane(string) string               { return "" }
func (t *stageTmux) CurrentPane() (string, error)            { return "%0", nil }
func (t *stageTmux) SplitRight(string, string) string        { return "%1" }
func (t *stageTmux) NewWindow(string, string, string) (string, error) {
	return "%2", nil
}
func (t *stageTmux) SwapPanes(string, string) { t.swaps++ }
func (t *stageTmux) KillPane(string)          {}

func (t *stageTmux) RespawnPane(_, _ string, argv ...string) {
	t.respawns = append(t.respawns, strings.Join(argv, " "))
}

// infoModel builds a chats model holding one live chat, with the cursor on it.
// Row 0 is the no-workflow header, so the chat is row 1.
func infoModel(tmux *stageTmux) model {
	manager := agents.NewAgentManager(agents.Params{Tmux: tmux})
	manager.InitStage()

	return model{
		manager:    manager,
		keys:       appconfig.DefaultKeybindings(),
		screen:     screenChats,
		chats:      []chatItem{{agent: store.Agent{Name: "auth", Pane: "%9", Cwd: "/repo"}, alive: true}},
		chatCursor: 1,
	}
}

func pressInfo(t *testing.T, m model) model {
	t.Helper()

	next, _ := m.updateChats(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	updated, ok := next.(model)
	if !ok {
		t.Fatalf("want a panel model back, got %T", next)
	}

	return updated
}

// TestInfoTogglesLiveChatRecord confirms the info key stages a live chat's
// stored record in place of its running pane, and that pressing it again swaps
// the pane back.
func TestInfoTogglesLiveChatRecord(t *testing.T) {
	tmux := &stageTmux{}
	m := infoModel(tmux)

	m = pressInfo(t, m)
	if !m.infoPinned {
		t.Fatal("info key should pin the record")
	}
	if len(tmux.respawns) != 1 || !strings.Contains(tmux.respawns[0], "bat") {
		t.Fatalf("want the record rendered through bat, got %v", tmux.respawns)
	}
	if tmux.swaps != 0 {
		t.Fatalf("pinning info should not swap the chat pane in, got %d swaps", tmux.swaps)
	}

	m = pressInfo(t, m)
	if m.infoPinned {
		t.Fatal("second press should unpin the record")
	}
	if tmux.swaps != 1 {
		t.Fatalf("want the chat pane swapped back in, got %d swaps", tmux.swaps)
	}
}

// TestInfoUnpinsOnCursorMove confirms the pinned record does not outlive the row
// it describes.
func TestInfoUnpinsOnCursorMove(t *testing.T) {
	tmux := &stageTmux{}
	m := pressInfo(t, infoModel(tmux))

	next, _ := m.updateChats(tea.KeyMsg{Type: tea.KeyUp})
	moved, ok := next.(model)
	if !ok {
		t.Fatalf("want a panel model back, got %T", next)
	}
	if moved.infoPinned {
		t.Fatal("moving the cursor should unpin the record")
	}
}

// TestInfoIgnoredOnHeader confirms the info key does nothing on a workflow
// header, which has no record to show.
func TestInfoIgnoredOnHeader(t *testing.T) {
	tmux := &stageTmux{}
	m := infoModel(tmux)
	m.chatCursor = 0

	if pressInfo(t, m).infoPinned {
		t.Fatal("info key on a header should not pin anything")
	}
}

func TestChatMarkdown(t *testing.T) {
	agent := store.Agent{
		Name:            "auth",
		Pane:            "%9",
		Cwd:             "/repo",
		ClaudeSessionID: "abc",
		CreatedAt:       time.Date(2026, 7, 24, 15, 4, 0, 0, time.UTC),
	}

	live := chatMarkdown(agent, true)
	if !strings.Contains(live, "_running in %9_") {
		t.Fatalf("live record should say where it runs, got:\n%s", live)
	}
	if strings.Contains(live, "Press open to restore") {
		t.Fatalf("live record should not offer a restore, got:\n%s", live)
	}
	// Flags are unset here, so the record shows a dash rather than an empty field.
	if !strings.Contains(live, "- directory: /repo") || !strings.Contains(live, "- flags: —") {
		t.Fatalf("live record should carry the launch config, got:\n%s", live)
	}
	if !strings.Contains(live, "- resume: abc") {
		t.Fatalf("live record should carry the resume id, got:\n%s", live)
	}

	dormant := chatMarkdown(agent, false)
	if !strings.Contains(dormant, "_dormant — not running_") {
		t.Fatalf("dormant record should say it is not running, got:\n%s", dormant)
	}
	if !strings.Contains(dormant, "Press open to restore") {
		t.Fatalf("dormant record should offer a restore, got:\n%s", dormant)
	}
}
