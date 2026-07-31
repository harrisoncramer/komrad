package ui

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"weave/internal/actions"
	"weave/internal/appconfig"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestActionKeys covers what a keypress on the actions list does. Running does not move off the
// list, since firing an action is closer to a webhook than to opening something, so there is
// nowhere to navigate to and the cursor stays on the agent to fire a second one at.
func TestActionKeys(t *testing.T) {
	keys := appconfig.DefaultKeybindings()

	tests := []struct {
		name string
		// empty starts the screen with no scripts, for the keys that act on a row.
		empty      bool
		key        string
		wantScreen screen
		wantRan    []string
	}{
		{
			name:       "run on an empty list starts nothing",
			empty:      true,
			key:        "enter",
			wantScreen: screenActions,
		},
		{
			name:       "back returns to the agents list without running anything",
			key:        "esc",
			wantScreen: screenAgents,
		},
		{
			name:       "down moves to the next script",
			key:        "down",
			wantScreen: screenActions,
		},
		{
			name:       "run starts the script under the cursor and stays put",
			key:        "enter",
			wantScreen: screenActions,
			wantRan:    []string{"open-pr"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &recordingRunner{}
			m := actionsModel(t, runner, keys)
			if tt.empty {
				m.actions.items = nil
			}

			updated, _ := m.updateActions(keyMsg(tt.key))
			got, ok := updated.(model)
			require.True(t, ok)

			assert.Equal(t, tt.wantScreen, got.screen)
			assert.Equal(t, tt.wantRan, runner.ran)
			assert.Nil(t, got.banner, "nothing here was refused")
		})
	}
}

// TestActionsFireInSuccession covers what staying on the list is for. Two actions on the same
// agent is the ordinary case, opening a pull request and then the issue behind it, and leaving
// after the first would mean walking back to the same row to fire the second.
func TestActionsFireInSuccession(t *testing.T) {
	runner := &recordingRunner{}
	m := actionsModel(t, runner, appconfig.DefaultKeybindings())

	fired, _ := m.updateActions(keyMsg("enter"))
	m, ok := fired.(model)
	require.True(t, ok)

	moved, _ := m.updateActions(keyMsg("down"))
	m, ok = moved.(model)
	require.True(t, ok)

	fired, _ = m.updateActions(keyMsg("enter"))
	got, ok := fired.(model)
	require.True(t, ok)

	assert.Equal(t, screenActions, got.screen)
	assert.Equal(t, []string{"open-pr", "unrunnable"}, runner.ran, "both fired without leaving the list")
}

// TestActionRunIsRefusedWithoutTheExecutableBit covers the one failure the panel is the
// only thing that can report. The list stays up, since the row that needs chmod is on it.
func TestActionRunIsRefusedWithoutTheExecutableBit(t *testing.T) {
	runner := &recordingRunner{err: actions.ErrNotExecutable}
	m := actionsModel(t, runner, appconfig.DefaultKeybindings())

	updated, _ := m.updateActions(keyMsg("enter"))
	got, ok := updated.(model)
	require.True(t, ok)

	assert.Equal(t, screenActions, got.screen, "the list stays up so the row can be seen")
	require.NotNil(t, got.banner)
	assert.Contains(t, got.banner.Summary, "open-pr")
	assert.Contains(t, got.banner.Detail, "chmod")
}

// TestActionOpensOnTheHoveredAgent covers the list being opened from a row. It carries the
// agent it was opened on rather than reading the cursor back later, since the refresh keeps
// running while it is up and can move the rows under it.
func TestActionOpensOnTheHoveredAgent(t *testing.T) {
	dir := t.TempDir()
	writeScript(t, filepath.Join(dir, "open-pr"), 0o755)

	tmux := &recordingTmux{}
	m := infoModel(tmux)
	m.actionsDir = dir

	entered, _ := m.updateAgents(keyMsg("a"))
	got, ok := entered.(model)
	require.True(t, ok)

	require.Equal(t, screenActions, got.screen)
	assert.Equal(t, "auth", got.actions.agent.Name)
	require.Len(t, got.actions.items, 1)
	assert.Equal(t, "open-pr", got.actions.items[0].Name)
	assert.Contains(t, stagedDocument(t), "echo hello", "the script is what the stage shows")
}

// TestActionOnAWorkflowHeaderDoesNothing pins that the key needs a row. Every other
// per-row key behaves this way on a header, and there is no such thing as an action on a
// workflow.
func TestActionOnAWorkflowHeaderDoesNothing(t *testing.T) {
	m := infoModel(&recordingTmux{})
	m.actionsDir = t.TempDir()
	m.tree.cursor = 0

	updated, _ := m.updateAgents(keyMsg("a"))
	got, ok := updated.(model)
	require.True(t, ok)

	assert.Equal(t, screenAgents, got.screen)
}

// TestActionEnv covers what a script is handed. The agent goes in as one JSON value under one
// name, with its metadata as an object a script can index by key. The two values beside it are
// what it reaches back through, and without the socket, keys sent to the pane in the payload
// would go to the user's own tmux server instead.
func TestActionEnv(t *testing.T) {
	created := time.Date(2026, time.July, 30, 9, 0, 0, 0, time.UTC)
	mark := "✓"

	m := model{
		addr:       "127.0.0.1:8787",
		tmuxSocket: "weave",
		tree:       treeState{workflows: []workflowstore.Workflow{{ID: "w1", Name: "backend"}}},
		actions: actionState{
			alive: true,
			agent: agentstore.Agent{
				ID: "a1", Name: "auth", State: agentstore.StateWaiting, Cwd: "/repo",
				Harness: "claude", SessionID: "sess-1", Flags: "--model opus", Prompt: "fix auth",
				Pane: "%9", WorkflowID: "w1", Mark: &mark, CreatedAt: created,
				Metadata: []agentstore.Metadata{{Key: "pr", Value: "https://example.test/1"}},
			},
		},
	}

	env, err := m.actionEnv()
	require.NoError(t, err)

	values := map[string]string{}
	for _, entry := range env {
		key, value, found := strings.Cut(entry, "=")
		require.True(t, found, "every entry is a name and a value")
		values[key] = value
	}

	assert.Equal(t, "a1", values["WEAVE_AGENT_ID"])
	assert.Equal(t, "127.0.0.1:8787", values["WEAVE_ADDR"])
	assert.Equal(t, "weave", values["WEAVE_TMUX_SOCKET"])

	var payload actionPayload
	require.NoError(t, json.Unmarshal([]byte(values["WEAVE_AGENT"]), &payload))

	assert.Equal(t, "auth", payload.Name)
	assert.Equal(t, agentstore.StateWaiting, payload.State)
	assert.True(t, payload.Alive)
	assert.Equal(t, "%9", payload.Pane)
	assert.Equal(t, "backend", payload.Workflow, "the workflow reads as its name, not the id a script cannot use")
	assert.Equal(t, "✓", payload.Mark)
	assert.Equal(t, "2026-07-30T09:00:00Z", payload.CreatedAt)
	assert.Equal(t, map[string]string{"pr": "https://example.test/1"}, payload.Metadata)
	assert.NotContains(t, values["WEAVE_AGENT"], "transcript", "a transcript is far past what an environment holds")
}

// TestActionsViewNamesTheAgentAndDimsWhatCannotRun covers what the list says. The agent is named
// because the list acts on the row it was opened from rather than on whatever the agents list is
// hovering later, and the legend names the chmod, since a dim row cannot say why it is dim.
func TestActionsViewNamesTheAgentAndDimsWhatCannotRun(t *testing.T) {
	m := actionsModel(t, &recordingRunner{}, appconfig.DefaultKeybindings())
	m.actions.cursor = 1

	view := m.actionsView()

	assert.Contains(t, view, "Actions on auth")
	assert.Contains(t, view, "open-pr")
	assert.Contains(t, view, "unrunnable")
	assert.Contains(t, view, "chmod +x to run", "the legend says what the run key wants")
}

// TestNoActionsSaysWhereToPutThem covers the empty case. The directory is not created for
// the user, so the pane beside an empty list has to name the path rather than sit blank.
func TestNoActionsSaysWhereToPutThem(t *testing.T) {
	got := noActionsMarkdown("/home/someone/.config/weave/actions")

	assert.Contains(t, got, "/home/someone/.config/weave/actions")
	assert.Contains(t, got, "WEAVE_AGENT")
}

// TestFencedBlock covers the shared block any screen wraps somebody else's text in. The fence has
// to beat whatever is inside it, or text carrying a fence of its own closes the block early and
// leaves the rest of itself rendering as prose.
func TestFencedBlock(t *testing.T) {
	tests := []struct {
		name        string
		text        []byte
		wantContain []string
		wantAbsent  []string
	}{
		{
			name:        "a compiled program has nothing to read",
			text:        []byte{0x7f, 'E', 'L', 'F', 0x00, 0x01},
			wantContain: []string{"not text"},
			wantAbsent:  []string{"```", "ELF"},
		},
		{
			name:        "nothing at all says so rather than drawing an empty block",
			text:        []byte("\n"),
			wantContain: []string{"_empty_"},
			wantAbsent:  []string{"```"},
		},
		{
			name:        "text carrying a fence is wrapped in a longer one",
			text:        []byte("echo '```go'\n"),
			wantContain: []string{"````\necho '```go'\n````"},
		},
		{
			name:        "text carrying a longer fence is wrapped in a longer one still",
			text:        []byte("````\n"),
			wantContain: []string{"`````\n````\n`````"},
		},
		{
			name:        "plain text goes in as it was written",
			text:        []byte("#!/bin/sh\nopen \"$url\"\n"),
			wantContain: []string{"```\n#!/bin/sh\nopen \"$url\"\n```"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fencedBlock(tt.text)

			for _, want := range tt.wantContain {
				assert.Contains(t, got, want)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, got, absent)
			}
		})
	}
}

// TestActionMarkdown covers what the action document adds around the shared block, which is the
// name, the chmod a file without the bit wants, and whether the file was read whole.
func TestActionMarkdown(t *testing.T) {
	tests := []struct {
		name        string
		action      actions.Action
		contents    []byte
		truncated   bool
		wantContain []string
		wantAbsent  []string
	}{
		{
			name:        "a file without the executable bit says what it wants",
			action:      actions.Action{Name: "unrunnable"},
			contents:    []byte("#!/bin/sh\n"),
			wantContain: []string{"# unrunnable", "chmod +x"},
		},
		{
			name:        "a binary read to the limit does not claim to be a partial script",
			action:      actions.Action{Name: "compiled", Executable: true},
			contents:    []byte{0x7f, 'E', 'L', 'F', 0x00},
			truncated:   true,
			wantContain: []string{"not text"},
			wantAbsent:  []string{"only the start"},
		},
		{
			name:        "a head of a longer file says it is only the start",
			action:      actions.Action{Name: "long", Executable: true},
			contents:    []byte("#!/bin/sh\necho one\n"),
			truncated:   true,
			wantContain: []string{"only the start of the file is shown"},
		},
		{
			name:        "a runnable script is its name over its own text",
			action:      actions.Action{Name: "open-pr", Executable: true},
			contents:    []byte("#!/bin/sh\nopen \"$url\"\n"),
			wantContain: []string{"# open-pr", "```\n#!/bin/sh"},
			wantAbsent:  []string{"chmod", "only the start"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := actionMarkdown(tt.action, tt.contents, tt.truncated)

			for _, want := range tt.wantContain {
				assert.Contains(t, got, want)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, got, absent)
			}
		})
	}
}

// TestActionPreviewRedrawsOnlyOnAChange covers the stage leaving its pane alone while what it
// holds is unchanged, which is what keeps the refresh from restarting the pager every tick. The
// document carries the file's text, so an edit is a different document rather than the same
// path staged twice.
func TestActionPreviewRedrawsOnlyOnAChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "open-pr")
	writeScript(t, path, 0o755)

	tmux := &recordingTmux{}
	m := actionsModel(t, &recordingRunner{}, appconfig.DefaultKeybindings())
	m.stage = NewStage(tmux, appconfig.DefaultSplit)
	m.stage.Init()

	found, err := actions.Discover(dir)
	require.NoError(t, err)
	m.actions.items = found

	m.stageAction()
	m.stageAction()
	require.Len(t, tmux.respawns, 1, "showing the same file twice draws once")

	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\necho edited\n"), 0o755))
	m.stageAction()

	require.Len(t, tmux.respawns, 2, "an edit is a different document")
	assert.Contains(t, stagedDocument(t), "echo edited")
}

// TestActionPreviewOfAnUnreadableFile covers a file listed but not readable. The stage says why
// rather than drawing an empty preview, since the row is on screen either way.
func TestActionPreviewOfAnUnreadableFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "locked")
	writeScript(t, path, 0o755)
	require.NoError(t, os.Chmod(path, 0o100))

	m := actionsModel(t, &recordingRunner{}, appconfig.DefaultKeybindings())
	found, err := actions.Discover(dir)
	require.NoError(t, err)
	m.actions.items = found

	m.stageAction()

	assert.Contains(t, stagedDocument(t), "locked")
}

// TestActionRunsInTheAgentsDirectory covers the one thing about the run the panel decides
// rather than the script: it starts where the agent is, so git and gh in a script land in
// that checkout without the script working out where it is.
func TestActionRunsInTheAgentsDirectory(t *testing.T) {
	runner := &recordingRunner{}
	m := actionsModel(t, runner, appconfig.DefaultKeybindings())

	_, _ = m.updateActions(keyMsg("enter"))

	require.Equal(t, []string{"/repo"}, runner.dirs)
	require.Len(t, runner.envs, 1)
	assert.Contains(t, strings.Join(runner.envs[0], " "), `"name":"auth"`)
}

// TestActionRunsARealScript drives the whole path with nothing faked: a script on disk, the
// runner that starts processes, and a payload that crosses into one. It is the only test
// here that proves the contract a script is actually written against, since every other one
// stops at the panel's own edge.
//
// The prompt carries a quote and a shell command on purpose. An action is executed rather
// than typed, so what is in the payload reaches the script as bytes and is never parsed as
// a command line, which is the difference between this and the launcher.
func TestActionRunsARealScript(t *testing.T) {
	dir := t.TempDir()
	work := t.TempDir()
	out := filepath.Join(work, "payload.json")
	marker := filepath.Join(work, "pwned")

	// Both files are written to relative paths, so arriving in work at all is the script
	// having started in the agent's own directory.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "record"), []byte(
		"#!/bin/sh\nprintf '%s' \"$WEAVE_AGENT\" > payload.json\n"+
			"printf '%s %s %s' \"$WEAVE_AGENT_ID\" \"$WEAVE_ADDR\" \"$WEAVE_TMUX_SOCKET\" > reached-back\n",
	), 0o755))

	found, err := actions.Discover(dir)
	require.NoError(t, err)

	tmux := &recordingTmux{}
	stage := NewStage(tmux, appconfig.DefaultSplit)
	stage.Init()

	m := model{
		tmux:       tmux,
		popup:      NewPopup(tmux, "test-session"),
		stage:      stage,
		keys:       appconfig.DefaultKeybindings(),
		screen:     screenActions,
		actionsDir: dir,
		runner:     actions.Runner{},
		addr:       "127.0.0.1:8787",
		tmuxSocket: "weave",
		actions: actionState{
			items: found,
			agent: agentstore.Agent{
				ID: "a1", Name: "auth", Cwd: work, Pane: "%9",
				Prompt:   `fix "auth"; echo pwned > ` + marker,
				Metadata: []agentstore.Metadata{{Key: "pr", Value: "https://example.test/1"}},
			},
			alive: true,
		},
	}

	updated, _ := m.updateActions(keyMsg("enter"))
	got, ok := updated.(model)
	require.True(t, ok)
	require.Nil(t, got.banner, "the script started")
	assert.Equal(t, screenActions, got.screen)

	var payload actionPayload
	require.NoError(t, json.Unmarshal([]byte(waitForFile(t, out)), &payload))

	assert.Equal(t, "auth", payload.Name)
	assert.Equal(t, "%9", payload.Pane)
	assert.Equal(t, `fix "auth"; echo pwned > `+marker, payload.Prompt, "the payload arrives as it was written")
	assert.Equal(t, map[string]string{"pr": "https://example.test/1"}, payload.Metadata)

	assert.Equal(t, "a1 127.0.0.1:8787 weave", waitForFile(t, filepath.Join(work, "reached-back")))
	assert.NoFileExists(t, marker, "the payload is never parsed as a command line")
}

// TestActionStartFailureIsReported covers the other refusal. Nothing else in the panel
// would ever say so, since no output is captured and no exit status is read.
func TestActionStartFailureIsReported(t *testing.T) {
	runner := &recordingRunner{err: errRunnerFailed}
	m := actionsModel(t, runner, appconfig.DefaultKeybindings())

	updated, _ := m.updateActions(keyMsg("enter"))
	got, ok := updated.(model)
	require.True(t, ok)

	require.NotNil(t, got.banner)
	assert.Contains(t, got.banner.Detail, "exec format error")
}

// actionsModel builds the panel sitting on the actions list of one live agent, with a
// runnable script and one that is not executable.
func actionsModel(t *testing.T, runner actionRunner, keys appconfig.Keybindings) model {
	t.Helper()

	dir := t.TempDir()
	writeScript(t, filepath.Join(dir, "open-pr"), 0o755)
	writeScript(t, filepath.Join(dir, "unrunnable"), 0o644)

	found, err := actions.Discover(dir)
	require.NoError(t, err)
	require.Len(t, found, 2)

	tmux := &recordingTmux{}
	stage := NewStage(tmux, appconfig.DefaultSplit)
	stage.Init()

	return model{
		tmux:       tmux,
		popup:      NewPopup(tmux, "test-session"),
		stage:      stage,
		keys:       keys,
		screen:     screenActions,
		actionsDir: dir,
		runner:     runner,
		actions: actionState{
			items: found,
			agent: agentstore.Agent{ID: "a1", Name: "auth", Cwd: "/repo"},
			alive: true,
		},
	}
}

// stagedDocument is the text of what the stage last rendered, read back off the file the pager
// in its pane is showing. The colours around a word sit between it and the next, so they are
// stripped to read it.
func stagedDocument(t *testing.T) string {
	t.Helper()

	shown, err := os.ReadFile(filepath.Join(os.TempDir(), "weave-stage.ansi"))
	require.NoError(t, err)

	return ansi.Strip(string(shown))
}

// writeScript puts a runnable shell script at path with the given mode.
func writeScript(t *testing.T, path string, mode os.FileMode) {
	t.Helper()

	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\necho hello\n"), mode))
}

// recordingRunner stands in for running a script, so a test drives the screen without
// spawning anything. err is what it refuses with, which is how the chmod case is covered
// without a file whose mode the test would have to arrange.
type recordingRunner struct {
	err error
	ran []string
	// dirs are the working directories it was asked to run in, one per run.
	dirs []string
	// envs are the environments it was handed, one per run.
	envs [][]string
}

func (r *recordingRunner) Run(action actions.Action, dir string, env []string) error {
	if r.err != nil {
		return r.err
	}

	r.ran = append(r.ran, action.Name)
	r.dirs = append(r.dirs, dir)
	r.envs = append(r.envs, env)

	return nil
}

// waitForFile reads what a started script is on its way to writing. Running one returns as
// soon as the process exists rather than once it has finished, so waiting for what it left
// behind is the only thing a caller can do.
func waitForFile(t *testing.T, path string) string {
	t.Helper()

	var content []byte
	require.Eventually(t, func() bool {
		read, err := os.ReadFile(path)
		content = read

		return err == nil && len(read) > 0
	}, 5*time.Second, 10*time.Millisecond, "the script never wrote %s", path)

	return string(content)
}

// errRunnerFailed stands in for a script that could not be started at all, which is a
// failure of the panel's rather than of anything the script did.
var errRunnerFailed = errors.New("fork/exec: exec format error")
