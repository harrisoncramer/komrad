package agents

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"weave/internal/actions"
	"weave/internal/appconfig"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListActions covers the listing the panel and a model both read. A file that cannot be run
// is still listed, since a script written and forgotten should show up wanting a chmod rather
// than never appearing, and each carries the start of its own text so a caller showing one needs
// no access to the file.
func TestListActions(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "open-pr"), []byte("#!/bin/sh\necho hello\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "unrunnable"), []byte("#!/bin/sh\n"), 0o644))

	listed, err := actionService(t, dir, &recordingRunner{}).ListActions()
	require.NoError(t, err)
	require.Len(t, listed, 2)

	assert.Equal(t, "open-pr", listed[0].Name)
	assert.True(t, listed[0].Executable)
	assert.Equal(t, "#!/bin/sh\necho hello\n", string(listed[0].Preview))
	assert.False(t, listed[0].Truncated)

	assert.Equal(t, "unrunnable", listed[1].Name)
	assert.False(t, listed[1].Executable)
}

// TestListActionsTruncatesALongFile covers the bound on the preview. An action can be a compiled
// program of any size, and a caller renders the whole document it is handed, so the head is cut
// back to its last complete line rather than sent whole.
func TestListActionsTruncatesALongFile(t *testing.T) {
	dir := t.TempDir()
	long := "#!/bin/sh\n"
	for len(long) < maxActionPreview*2 {
		long += "echo a line of a script that goes on for a while\n"
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "long"), []byte(long), 0o755))

	listed, err := actionService(t, dir, &recordingRunner{}).ListActions()
	require.NoError(t, err)
	require.Len(t, listed, 1)

	preview := listed[0].Preview
	assert.True(t, listed[0].Truncated)
	assert.LessOrEqual(t, len(preview), maxActionPreview)
	require.NotEmpty(t, preview)
	assert.Equal(t, long[:len(preview)], string(preview), "the head is the start of the file as it is")
	assert.Equal(t, byte('\n'), long[len(preview)], "cut at a line boundary, never mid-character")
}

// TestListActionsWithNoDirectory covers never having written one, which is the ordinary case
// rather than a failure. A caller says so on screen and needs nothing else from here.
func TestListActionsWithNoDirectory(t *testing.T) {
	listed, err := actionService(t, filepath.Join(t.TempDir(), "never-created"), &recordingRunner{}).ListActions()

	require.NoError(t, err)
	assert.Empty(t, listed)
}

// TestRunActionRefusals covers everything that can go wrong, which is all of it from before the
// process starts. Once an action is running nothing waits for it, so there is nothing else to
// report.
func TestRunActionRefusals(t *testing.T) {
	tests := []struct {
		name    string
		agentID string
		action  string
		wantIs  error
	}{
		{
			name:    "an agent that is not there",
			agentID: "missing",
			action:  "open-pr",
			wantIs:  ErrAgentNotFound,
		},
		{
			name:    "an action the directory does not hold",
			agentID: "a1",
			action:  "never-written",
			wantIs:  ErrActionNotFound,
		},
		{
			name:    "a name climbing out of the directory finds nothing",
			agentID: "a1",
			action:  "../../../../bin/echo",
			wantIs:  ErrActionNotFound,
		},
		{
			name:    "a file the user has not made executable",
			agentID: "a1",
			action:  "unrunnable",
			wantIs:  actions.ErrNotExecutable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "open-pr"), []byte("#!/bin/sh\n"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "unrunnable"), []byte("#!/bin/sh\n"), 0o644))

			runner := &recordingRunner{}
			err := actionService(t, dir, runner).RunAction(context.Background(), tt.agentID, tt.action)

			require.ErrorIs(t, err, tt.wantIs)
			assert.Empty(t, runner.ran, "nothing started")
		})
	}
}

// TestRunActionPayload covers what a script is handed, which is the reason this lives in the
// service rather than in the panel. The agent goes in as one JSON value under one name, with its
// metadata as an object a script can index by key, and the two values beside it are what it
// reaches back through. Without the socket, keys sent to the pane in the payload would go to the
// user's own tmux server instead.
func TestRunActionPayload(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "open-pr"), []byte("#!/bin/sh\n"), 0o755))

	runner := &recordingRunner{}
	service := actionService(t, dir, runner)

	require.NoError(t, service.RunAction(context.Background(), "a1", "open-pr"))

	require.Equal(t, []string{"open-pr"}, runner.ran)
	assert.Equal(t, dir, runner.dir, "an action starts among its own siblings, not in the agent")

	values := map[string]string{}
	for _, entry := range runner.env {
		key, value, found := strings.Cut(entry, "=")
		require.True(t, found, "every entry is a name and a value")
		values[key] = value
	}

	assert.Equal(t, "a1", values["WEAVE_AGENT_ID"])
	assert.Equal(t, "127.0.0.1:8787", values["WEAVE_ADDR"])
	assert.Equal(t, "weave", values["WEAVE_TMUX_SOCKET"])
	assert.Equal(t, dir, values["WEAVE_ACTIONS_DIR"])

	var payload actionPayload
	require.NoError(t, json.Unmarshal([]byte(values["WEAVE_AGENT"]), &payload))

	assert.Equal(t, "auth", payload.Name)
	assert.Equal(t, agentstore.StateWaiting, payload.State)
	assert.True(t, payload.Alive, "the pane still belongs to this agent")
	assert.Equal(t, "%9", payload.Pane)
	assert.Equal(t, "backend", payload.Workflow, "the workflow reads as its name, not the id a script cannot use")
	assert.Equal(t, "✓", payload.Mark)
	assert.Equal(t, "2026-07-30T09:00:00Z", payload.CreatedAt)
	assert.Equal(t, map[string]string{"pr": "https://example.test/1"}, payload.Metadata)
	assert.Equal(t, "fix auth", payload.Prompt)
	assert.False(t, payload.PromptTruncated, "a prompt that fits arrives whole and says so")
	assert.NotContains(t, values["WEAVE_AGENT"], "transcript", "a transcript is far past what an environment holds")
}

// TestRunActionBoundsALongPrompt covers the one field in the payload with no bound of its own.
// Everything an exec is handed shares a kernel limit, and going past it fails the run with a
// message naming the script, so a long prompt is cut here instead and the payload says it was.
func TestRunActionBoundsALongPrompt(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "open-pr"), []byte("#!/bin/sh\n"), 0o755))

	runner := &recordingRunner{}
	service := actionService(t, dir, runner)
	service.agents = fakeAgentReader{agent: &agentstore.Agent{
		ID:     "a1",
		Name:   "auth",
		Cwd:    "/repo",
		Prompt: strings.Repeat("prompt ", maxActionPrompt),
	}}

	require.NoError(t, service.RunAction(context.Background(), "a1", "open-pr"))

	var encoded string
	for _, entry := range runner.env {
		if key, value, _ := strings.Cut(entry, "="); key == "WEAVE_AGENT" {
			encoded = value
		}
	}

	var payload actionPayload
	require.NoError(t, json.Unmarshal([]byte(encoded), &payload))

	assert.True(t, payload.PromptTruncated, "a script showing the prompt can say it is a head of one")
	assert.LessOrEqual(t, len(payload.Prompt), maxActionPrompt)
	assert.True(t, strings.HasPrefix(payload.Prompt, "prompt prompt "), "what is kept is the start of it")
	assert.Less(t, len(encoded), 128*1024, "the payload stays well inside what an exec carries")
}

// TestHead covers the cut itself, which has to leave text a script can decode rather than half of
// whatever character the limit landed inside.
func TestHead(t *testing.T) {
	tests := []struct {
		name          string
		s             string
		limit         int
		want          string
		wantTruncated bool
	}{
		{
			name:  "a string inside the limit is untouched",
			s:     "fix auth",
			limit: 8,
			want:  "fix auth",
		},
		{
			name:          "a longer string is cut to the limit",
			s:             "fix auth now",
			limit:         8,
			want:          "fix auth",
			wantTruncated: true,
		},
		{
			// The limit lands inside the third character, so the cut pulls back off it
			// rather than handing over one byte of it.
			name:          "a cut inside a character pulls back to the one before it",
			s:             "日本語",
			limit:         7,
			want:          "日本",
			wantTruncated: true,
		},
		{
			name:          "a limit inside the first character keeps nothing",
			s:             "日本語",
			limit:         2,
			want:          "",
			wantTruncated: true,
		},
		{
			name:  "an empty string is not truncated",
			s:     "",
			limit: 8,
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, truncated := head(tt.s, tt.limit)

			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantTruncated, truncated)
		})
	}
}

// TestRunActionOnADormantAgent covers the one field the payload works out rather than reads. A
// pane that has been taken over or has died is not this agent's, so a script is told the agent is
// not running rather than left to send keys into somebody else's pane.
func TestRunActionOnADormantAgent(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "open-pr"), []byte("#!/bin/sh\n"), 0o755))

	runner := &recordingRunner{}
	service := actionService(t, dir, runner)
	service.tmux = fakePaneOwner{owner: "somebody-else"}

	require.NoError(t, service.RunAction(context.Background(), "a1", "open-pr"))

	var payload actionPayload
	for _, entry := range runner.env {
		if key, value, _ := strings.Cut(entry, "="); key == "WEAVE_AGENT" {
			require.NoError(t, json.Unmarshal([]byte(value), &payload))
		}
	}

	assert.False(t, payload.Alive)
}

// TestRunActionWithTheAgentDirectoryGone covers the agent an action is most wanted on. A worktree
// removed out from under an agent leaves the row behind, and that row is the one a script is fired
// on to tidy it, so where the agent used to be is a string in the payload and never somewhere the
// action has to be able to stand.
func TestRunActionWithTheAgentDirectoryGone(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cleanup"), []byte("#!/bin/sh\n"), 0o755))

	gone := filepath.Join(t.TempDir(), "removed-worktree")
	runner := &recordingRunner{}
	service := actionService(t, dir, runner)
	service.agents = fakeAgentReader{agent: &agentstore.Agent{ID: "a1", Name: "auth", Cwd: gone}}

	require.NoError(t, service.RunAction(context.Background(), "a1", "cleanup"))

	assert.Equal(t, dir, runner.dir)

	var payload actionPayload
	for _, entry := range runner.env {
		if key, value, _ := strings.Cut(entry, "="); key == "WEAVE_AGENT" {
			require.NoError(t, json.Unmarshal([]byte(value), &payload))
		}
	}

	assert.Equal(t, gone, payload.Cwd, "a script works out what to clean from the path itself")
}

// actionService builds the service over a temp actions directory, one agent, and one workflow.
func actionService(t *testing.T, dir string, runner actionRunner) *ActionService {
	t.Helper()

	mark := "✓"

	return NewActionService(ActionParams{
		Agents: fakeAgentReader{agent: &agentstore.Agent{
			ID: "a1", Name: "auth", State: agentstore.StateWaiting, Cwd: "/repo",
			Harness: "claude", SessionID: "sess-1", Flags: "--model opus", Prompt: "fix auth",
			Pane: "%9", WorkflowID: "w1", Mark: &mark,
			CreatedAt: time.Date(2026, time.July, 30, 9, 0, 0, 0, time.UTC),
			Metadata:  []agentstore.Metadata{{Key: "pr", Value: "https://example.test/1"}},
		}},
		Workflows: fakeWorkflowReader{workflow: &workflowstore.Workflow{ID: "w1", Name: "backend"}},
		Tmux:      fakePaneOwner{owner: "a1"},
		Runner:    runner,
		Load: func() (*appconfig.Config, error) {
			cfg := appconfig.Defaults()
			cfg.Actions.Dir = dir

			return cfg, nil
		},
		Addr:   "127.0.0.1:8787",
		Socket: "weave",
	})
}

// fakeAgentReader answers with one agent, and with nothing for any other id.
type fakeAgentReader struct {
	agent *agentstore.Agent
}

func (f fakeAgentReader) GetByID(_ context.Context, id string) (*agentstore.Agent, error) {
	if f.agent != nil && f.agent.ID == id {
		return f.agent, nil
	}

	return nil, nil
}

// fakeWorkflowReader answers with one workflow, which is where the payload's workflow name comes
// from.
type fakeWorkflowReader struct {
	workflow *workflowstore.Workflow
}

func (f fakeWorkflowReader) Get(_ context.Context, id string) (*workflowstore.Workflow, error) {
	if f.workflow != nil && f.workflow.ID == id {
		return f.workflow, nil
	}

	return nil, nil
}

// fakePaneOwner reports one owner for every pane, which is what decides whether the payload says
// the agent is running.
type fakePaneOwner struct {
	owner string
}

func (f fakePaneOwner) PaneOwner(string) string { return f.owner }

// recordingRunner stands in for starting a process, so the service's tests drive the whole path
// without spawning anything. Only the last run is kept, since no case fires twice.
type recordingRunner struct {
	ran []string
	dir string
	env []string
}

func (r *recordingRunner) Run(action actions.Action, dir string, env []string) error {
	if !action.Executable {
		return actions.ErrNotExecutable
	}

	r.ran = append(r.ran, action.Name)
	r.dir, r.env = dir, env

	return nil
}
