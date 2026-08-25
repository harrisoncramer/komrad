package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"weave/config"
	"weave/internal/actions"
	"weave/internal/logging"
	"weave/internal/tmux"
	"weave/internal/tmuxtest"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListActions covers the listing the panel and a model both read. A file that cannot be run
// is still listed, since a script written and forgotten should show up wanting a chmod rather
// than never appearing, and each carries what its own header declares so a caller showing one
// needs no access to the file.
func TestListActions(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "open-pr"), []byte("#!/bin/sh\n#WEAVE description=\"open it\"\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "unrunnable"), []byte("#!/bin/sh\n"), 0o644))

	listed, err := actionService(t, dir, &recordingRunner{}).ListActions()
	require.NoError(t, err)
	require.Len(t, listed, 2)

	assert.Equal(t, "open-pr", listed[0].Name)
	assert.True(t, listed[0].Executable)
	assert.Equal(t, "open it", listed[0].Header.Description)

	assert.Equal(t, "unrunnable", listed[1].Name)
	assert.False(t, listed[1].Executable)
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
			wantIs:  actions.ErrActionNotFound,
		},
		{
			name:    "a name climbing out of the directory finds nothing",
			agentID: "a1",
			action:  "../../../../bin/echo",
			wantIs:  actions.ErrActionNotFound,
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
	assert.Equal(t, "/repo", values["WEAVE_AGENT_CWD"])
	assert.Equal(t, "127.0.0.1:8787", values["WEAVE_ADDR"])
	assert.Equal(t, "weave", values["WEAVE_TMUX_SOCKET"])
	assert.Equal(t, dir, values["WEAVE_ACTIONS_DIR"])
	assert.Equal(t, "open-pr", values["WEAVE_ACTION"], "its own name, for a script that wants to tag its own log lines")
	assert.Equal(t, "%3", values["WEAVE_INFO_PANE"], "the pane the panel laid out for a script to fill")
	assert.Equal(t, "/tmp/host-socket", values["WEAVE_HOST_TMUX_SOCKET"])
	assert.Equal(t, "/dev/ttys004", values["WEAVE_HOST_CLIENT"])
	assert.Equal(t, "nvim", values["EDITOR"], "the configured editor, not whatever the service inherited")
	assert.Equal(t, "/bin/zsh", values["WEAVE_SHELL"], "the shell tmux itself opens a pane with")
	assert.True(t, strings.HasPrefix(values["PATH"], "/opt/weave/bin"+string(os.PathListSeparator)),
		"a script that shells out to weave finds this build's own binary first")

	var payload actionPayload
	require.NoError(t, json.Unmarshal([]byte(values["WEAVE_AGENT"]), &payload))

	assert.Equal(t, "auth", payload.Name)
	assert.Equal(t, agentstore.StateWaiting, payload.State)
	assert.True(t, payload.Alive, "the pane still belongs to this agent")
	assert.Equal(t, "%9", payload.Pane)
	assert.Equal(t, "backend", payload.Workflow, "the workflow reads as its name, not the id a script cannot use")
	assert.Equal(t, "✓", payload.Tag)
	assert.Equal(t, "2026-07-30T09:00:00Z", payload.CreatedAt)
	assert.Equal(t, map[string]string{"pr": "https://example.test/1"}, payload.Metadata)
	assert.Equal(t, "fix auth", payload.Prompt)
	assert.False(t, payload.PromptTruncated, "a prompt that fits arrives whole and says so")
	assert.NotContains(t, values["WEAVE_AGENT"], "transcript", "a transcript is far past what an environment holds")
}

// TestRunActionCarriesWorktreeEnvOnlyWhenEnabled covers the environment the two built-in
// worktree actions read their config from: present, and reflecting the workflow's own
// settings, when its worktree toggle is on, and absent entirely otherwise, since most
// workflows are not worktrees and should not carry them.
func TestRunActionCarriesWorktreeEnvOnlyWhenEnabled(t *testing.T) {
	tests := []struct {
		name     string
		worktree workflowstore.WorktreeConfig
		wantKeys []string
	}{
		{
			name: "disabled carries none of the worktree keys",
		},
		{
			name: "enabled carries every worktree key, reflecting the workflow's own config",
			worktree: workflowstore.WorktreeConfig{
				Enabled:      true,
				BaseBranch:   "develop",
				CopyPaths:    []string{".env", "config/local.json"},
				SymlinkPaths: []string{"node_modules"},
			},
			wantKeys: []string{
				"WEAVE_WORKTREE_REPO",
				"WEAVE_WORKTREES_DIR",
				"WEAVE_WORKTREE_BASE_BRANCH",
				"WEAVE_WORKTREE_COPY_PATHS",
				"WEAVE_WORKTREE_SYMLINK_PATHS",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actionsDir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(actionsDir, "open-pr"), []byte("#!/bin/sh\n"), 0o755))
			worktreesDir := t.TempDir()

			runner := &recordingRunner{}
			service := NewActionService(ActionParams{
				Agents: fakeAgentReader{agent: &agentstore.Agent{ID: "a1", Cwd: "/worktrees/a1", WorkflowID: "w1"}},
				Workflows: fakeWorkflowReader{workflow: &workflowstore.Workflow{
					ID: "w1", Name: "backend", Cwd: "/repo", Worktree: tt.worktree,
				}},
				Tmux:   fakePaneOwner{},
				Runner: runner,
				Load: func() (*config.Config, error) {
					cfg := config.Defaults()
					cfg.Actions.Dir = actionsDir
					cfg.Worktrees.Dir = worktreesDir

					return cfg, nil
				},
			})

			require.NoError(t, service.RunAction(context.Background(), "a1", "open-pr"))

			values := map[string]string{}
			for _, entry := range runner.env {
				key, value, found := strings.Cut(entry, "=")
				require.True(t, found)
				values[key] = value
			}

			if len(tt.wantKeys) == 0 {
				for _, key := range []string{"WEAVE_WORKTREE_REPO", "WEAVE_WORKTREES_DIR", "WEAVE_WORKTREE_BASE_BRANCH", "WEAVE_WORKTREE_COPY_PATHS", "WEAVE_WORKTREE_SYMLINK_PATHS"} {
					_, ok := values[key]
					assert.False(t, ok, "%s is not set for a workflow with worktree support off", key)
				}

				return
			}

			for _, key := range tt.wantKeys {
				_, ok := values[key]
				assert.True(t, ok, "%s is missing", key)
			}

			assert.Equal(t, "/repo", values["WEAVE_WORKTREE_REPO"], "the repo is the workflow's cwd, not the worktree the agent's cwd already names")
			assert.Equal(t, worktreesDir, values["WEAVE_WORKTREES_DIR"])
			assert.Equal(t, "develop", values["WEAVE_WORKTREE_BASE_BRANCH"])
			assert.Equal(t, ".env\nconfig/local.json", values["WEAVE_WORKTREE_COPY_PATHS"])
			assert.Equal(t, "node_modules", values["WEAVE_WORKTREE_SYMLINK_PATHS"])
		})
	}
}

// TestRunActionNamesAnArchivedWorkflow covers the label on an agent whose workflow has been
// archived. The agent still belongs to it, and a script told nothing cannot tell that from a
// workflow with no name.
func TestRunActionNamesAnArchivedWorkflow(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "open-pr"), []byte("#!/bin/sh\n"), 0o755))

	runner := &recordingRunner{}
	service := actionService(t, dir, runner)
	service.workflows = fakeWorkflowReader{
		workflow: &workflowstore.Workflow{ID: "w1", Name: "backend"},
		archived: true,
	}

	require.NoError(t, service.RunAction(context.Background(), "a1", "open-pr"))

	var payload actionPayload
	for _, entry := range runner.env {
		if value, found := strings.CutPrefix(entry, "WEAVE_AGENT="); found {
			require.NoError(t, json.Unmarshal([]byte(value), &payload))
		}
	}

	assert.Equal(t, "backend", payload.Workflow)
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

// TestRunSetupAction covers the two things a workflow's setup action decides about a create,
// which are whether it happens at all and where the agent ends up running.
func TestRunSetupAction(t *testing.T) {
	tests := []struct {
		name string

		action  string
		result  runResult
		wantDir string
		wantRan []string
		wantErr bool
	}{
		{
			name:    "one the directory does not hold abandons the launch",
			action:  "was-renamed",
			wantErr: true,
		},
		{
			name:    "one that fails abandons the launch",
			action:  "install-deps",
			result:  runResult{stderr: "no lockfile", err: actions.ErrFailed},
			wantRan: []string{"install-deps"},
			wantErr: true,
		},
		{
			name:    "one that writes too much to its dir file is refused rather than believed",
			action:  "loud",
			result:  runResult{dirFile: strings.Repeat("a", maxSetupDir+1)},
			wantRan: []string{"loud"},
			wantErr: true,
		},
		{
			name:    "one that writes right up to the cap is believed rather than cut short",
			action:  "loud",
			result:  runResult{dirFile: strings.Repeat("a", maxSetupDir)},
			wantDir: strings.Repeat("a", maxSetupDir),
			wantRan: []string{"loud"},
		},
		{
			name:    "an action that names nothing names no directory",
			action:  "install-deps",
			wantRan: []string{"install-deps"},
		},
		{
			name:    "what it writes to its dir file is where the agent runs",
			action:  "make-worktree",
			result:  runResult{dirFile: "/work/api-auth\n"},
			wantDir: "/work/api-auth",
			wantRan: []string{"make-worktree"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, name := range []string{"make-worktree", "install-deps", "loud"} {
				require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0o755))
			}

			runner := &recordingRunner{results: map[string]runResult{tt.action: tt.result}}
			service := actionService(t, dir, runner)

			got, err := service.RunSetupAction(t.Context(), agentstore.Agent{ID: "a1", Name: "auth"}, tt.action, "")

			if tt.wantErr {
				require.ErrorIs(t, err, ErrSetupFailed)
				assert.Empty(t, got, "a refused setup names no directory")
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantDir, got)
			}
			assert.Equal(t, tt.wantRan, runner.ran)
		})
	}
}

func TestRunSetupActionDoesNotHoldTheWholeDirFile(t *testing.T) {
	const wrote = 32 << 20

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "greedy"), []byte("#!/bin/sh\n"), 0o755))

	runner := &recordingRunner{results: map[string]runResult{"greedy": {dirFileSize: wrote}}}
	service := actionService(t, dir, runner)

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)

	got, err := service.RunSetupAction(t.Context(), agentstore.Agent{ID: "a1", Name: "auth"}, "greedy", "")

	runtime.ReadMemStats(&after)

	require.ErrorIs(t, err, ErrSetupFailed)
	assert.Empty(t, got, "a refused setup names no directory")
	assert.Contains(t, err.Error(), setupDirFileVar)
	assert.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(4<<20),
		"the refusal read the whole dir file to arrive at it")
}

// TestRunSetupActionTellsTheScriptWhatSetItOff covers WEAVE_TRIGGER, which is how a script that
// only makes sense at one moment can refuse the others itself.
func TestRunSetupActionTellsTheScriptWhatSetItOff(t *testing.T) {
	tests := []struct {
		name        string
		fire        func(*ActionService, *recordingRunner) error
		wantTrigger string
	}{
		{
			name: "fired by hand",
			fire: func(s *ActionService, _ *recordingRunner) error {
				return s.RunAction(t.Context(), "a1", "make-worktree")
			},
			wantTrigger: TriggerManual,
		},
		{
			name: "fired as a workflow's setup",
			fire: func(s *ActionService, _ *recordingRunner) error {
				_, err := s.RunSetupAction(t.Context(), agentstore.Agent{ID: "a1"}, "make-worktree", "")
				return err
			},
			wantTrigger: TriggerSetup,
		},
		{
			name: "fired as a workflow's setup on the way back up",
			fire: func(s *ActionService, _ *recordingRunner) error {
				_, err := s.RunResumeAction(t.Context(), agentstore.Agent{ID: "a1"}, "make-worktree")
				return err
			},
			wantTrigger: TriggerResume,
		},
		{
			name: "fired as a workflow's cleanup",
			fire: func(s *ActionService, _ *recordingRunner) error {
				return s.RunCleanupAction(t.Context(), agentstore.Agent{ID: "a1"}, "make-worktree", nil)
			},
			wantTrigger: TriggerCleanup,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "make-worktree"), []byte("#!/bin/sh\n"), 0o755))

			runner := &recordingRunner{}
			require.NoError(t, tt.fire(actionService(t, dir, runner), runner))

			assert.Contains(t, runner.env, "WEAVE_TRIGGER="+tt.wantTrigger)
		})
	}
}

// TestRunSetupActionCarriesTheReason covers what a person sees when a setup action refuses. The
// script's own stderr is the only thing that says why, so it travels with the refusal rather than
// sitting somewhere only a log would show.
func TestRunSetupActionCarriesTheReason(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "make-worktree"), []byte("#!/bin/sh\n"), 0o755))

	runner := &recordingRunner{results: map[string]runResult{
		"make-worktree": {stderr: "fatal: 'api-auth' is already checked out", err: actions.ErrFailed},
	}}

	_, err := actionService(t, dir, runner).RunSetupAction(t.Context(), agentstore.Agent{ID: "a1"}, "make-worktree", "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "already checked out")
}

// TestRunInPaneStreamsToARealPane drives runInPane against a real, throwaway tmux server
// rather than a fake, since the wrapper script it respawns into a pane is shell rather than
// Go, and a fake tmux would only ever prove the Go side calls out correctly, not that what
// it hands tmux actually runs.
func TestRunInPaneStreamsToARealPane(t *testing.T) {
	server := tmuxtest.New(t)
	server.NewSession("test")
	server.Run("new-window", "-d", "-t", "test:")

	dir := t.TempDir()
	svc := NewActionService(ActionParams{
		Tmux:   realPaneRunner{server.Client()},
		Socket: server.Socket(),
	})

	freshPane := func(t *testing.T) string {
		t.Helper()

		return server.Run("split-window", "-d", "-P", "-F", "#{pane_id}", "-t", "test:1")
	}

	t.Run("a script that succeeds", func(t *testing.T) {
		pane := freshPane(t)
		script := filepath.Join(dir, "ok")
		require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\necho \"working in $FOO\" >&2\n"), 0o755))

		result, err := svc.runInPane(t.Context(), actions.Action{Name: "ok", Path: script, Executable: true}, []string{"FOO=api-auth"}, pane, "", 2*time.Second)

		require.NoError(t, err)
		assert.Equal(t, 0, result.ExitCode)
		assert.Contains(t, string(result.Stderr), "working in api-auth", "captured alongside streaming live to the pane")
	})

	t.Run("a script that fails", func(t *testing.T) {
		pane := freshPane(t)
		script := filepath.Join(dir, "bad")
		require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\necho 'fatal: already checked out' >&2\nexit 7\n"), 0o755))

		result, err := svc.runInPane(t.Context(), actions.Action{Name: "bad", Path: script, Executable: true}, nil, pane, "", 2*time.Second)

		require.Error(t, err)
		assert.ErrorIs(t, err, actions.ErrFailed)
		assert.Equal(t, 7, result.ExitCode)
		assert.Contains(t, string(result.Stderr), "fatal: already checked out")
		captured := server.Run("capture-pane", "-p", "-t", pane)
		assert.Contains(t, captured, "fatal: already checked out", "left up to read rather than only in the error")
		assert.Contains(t, captured, "exited 7", "held open on a prompt rather than left to vanish")

		server.Run("send-keys", "-t", pane, "Enter")
	})

	t.Run("a script that only ever writes to stdout", func(t *testing.T) {
		pane := freshPane(t)
		script := filepath.Join(dir, "stdout-only")
		require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\necho 'hello 1'\necho 'hello 2'\nexit 1\n"), 0o755))
		logPath := filepath.Join(t.TempDir(), "setup.log")

		result, err := svc.runInPane(t.Context(), actions.Action{Name: "stdout-only", Path: script, Executable: true}, nil, pane, logPath, 2*time.Second)

		require.Error(t, err)
		assert.ErrorIs(t, err, actions.ErrFailed)
		assert.Contains(t, string(result.Stderr), "hello 1")
		assert.Contains(t, string(result.Stderr), "hello 2")
		persisted, readErr := os.ReadFile(logPath)
		require.NoError(t, readErr)
		assert.Contains(t, string(persisted), "hello 1", "stdout is what most scripts fail through, not just stderr")
		assert.Contains(t, string(persisted), "hello 2")
	})

	t.Run("a script that outlives its timeout", func(t *testing.T) {
		pane := freshPane(t)
		script := filepath.Join(dir, "slow")
		require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\nsleep 30\n"), 0o755))
		logPath := filepath.Join(t.TempDir(), "setup.log")

		result, err := svc.runInPane(t.Context(), actions.Action{Name: "slow", Path: script, Executable: true}, nil, pane, logPath, 200*time.Millisecond)

		require.Error(t, err)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Contains(t, string(result.Stderr), "gave up after 200ms", "the reason is not silently lost")
		persisted, readErr := os.ReadFile(logPath)
		require.NoError(t, readErr)
		assert.Contains(t, string(persisted), "gave up after 200ms", "and it lands in the durable log a person cycles to later")
	})

	t.Run("a pane that is already gone", func(t *testing.T) {
		pane := freshPane(t)
		server.Run("kill-pane", "-t", pane)

		script := filepath.Join(dir, "ok")
		_, err := svc.runInPane(t.Context(), actions.Action{Name: "ok", Path: script, Executable: true}, nil, pane, "", time.Second)

		require.Error(t, err)
		assert.ErrorIs(t, err, actions.ErrNotStartable)
	})

	t.Run("a pane killed while it is running", func(t *testing.T) {
		pane := freshPane(t)
		script := filepath.Join(dir, "slow")
		require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\nsleep 30\n"), 0o755))
		logPath := filepath.Join(t.TempDir(), "setup.log")

		go func() {
			time.Sleep(300 * time.Millisecond)
			server.Run("kill-pane", "-t", pane)
		}()

		began := time.Now()
		result, err := svc.runInPane(t.Context(), actions.Action{Name: "slow", Path: script, Executable: true}, nil, pane, logPath, 30*time.Second)
		elapsed := time.Since(began)

		require.Error(t, err)
		assert.ErrorIs(t, err, errPaneDied)
		assert.Less(t, elapsed, 5*time.Second, "noticed the pane dying rather than waiting out the full timeout")
		assert.Contains(t, string(result.Stderr), "pane closed before the action finished")
		persisted, readErr := os.ReadFile(logPath)
		require.NoError(t, readErr)
		assert.Contains(t, string(persisted), "pane closed before the action finished", "left for a person cycling to the setup log to read")
	})
}

type realPaneRunner struct {
	*tmux.Client
}

func (realPaneRunner) GetPaneOwner(string) string { return "" }
func (realPaneRunner) GetHostSocket() string      { return "" }
func (realPaneRunner) GetInfoPane() string        { return "" }
func (realPaneRunner) ShowGlobal(string) string   { return "" }
func (realPaneRunner) DefaultShell() string       { return "" }

// TestRunCleanupActionRefusesOneThatIsGone covers a cleanup naming a script that has been
// renamed. It reports rather than pretending, and the caller is the one that decides an agent
// is still removable.
func TestRunCleanupActionRefusesOneThatIsGone(t *testing.T) {
	err := actionService(t, t.TempDir(), &recordingRunner{}).
		RunCleanupAction(t.Context(), agentstore.Agent{ID: "a1"}, "was-renamed", nil)

	require.ErrorIs(t, err, actions.ErrActionNotFound)
}

// TestRunActionLogsWhatItDid covers the only account there is of an action nothing waits on.
func TestRunActionLogsWhatItDid(t *testing.T) {
	tests := []struct {
		name string

		result    runResult
		wantLevel string
		wantBody  string

		wantFields map[string]any
	}{
		{
			name:      "one that worked is still an account of a run",
			wantLevel: "INFO",
			wantBody:  "action finished",
			wantFields: map[string]any{
				"process.exit.code":     float64(0),
				"weave.action.stderr":   nil,
				"weave.action.name":     "cleanup",
				"weave.action.trigger":  TriggerManual,
				"weave.action.agent.id": "a1",
			},
		},
		{
			name: "one that failed carries the status and what it said",
			result: runResult{
				stderr:   "fatal: '/repo' is dirty\n",
				exitCode: 3,
				err:      actions.ErrFailed,
			},
			wantLevel: "ERROR",
			wantBody:  "action failed",
			wantFields: map[string]any{
				"process.exit.code":      float64(3),
				"weave.action.stderr":    "fatal: '/repo' is dirty",
				"weave.action.agent.cwd": "/repo",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "cleanup"), []byte("#!/bin/sh\n"), 0o755))

			runner := &recordingRunner{results: map[string]runResult{"cleanup": tt.result}}
			ctx, records := loggedContext(t)

			require.NoError(t, actionService(t, dir, runner).RunAction(ctx, "a1", "cleanup"))

			written := records()

			began := recordFor(t, written, "action started")
			assert.Equal(t, "INFO", began["severity_text"])
			assert.Equal(t, "cleanup", began["weave.action.name"])

			got := recordFor(t, written, tt.wantBody)
			assert.Equal(t, tt.wantLevel, got["severity_text"])
			assert.Contains(t, got, "duration_ms", "how long it took is on the end of every run")
			for field, want := range tt.wantFields {
				if want == nil {
					assert.NotContains(t, got, field)

					continue
				}

				assert.Equal(t, want, got[field], field)
			}
		})
	}
}

// TestSetupActionLogsBothEnds covers the account of an action something is waiting on. The
// refusal travels back to whoever asked for the launch, so the log's job here is saying a run
// happened and what came of it, which is the half nothing else records.
func TestSetupActionLogsBothEnds(t *testing.T) {
	tests := []struct {
		name       string
		result     runResult
		wantFields map[string]any
	}{
		{
			name: "one that refused is recorded as the end of the run",
			result: runResult{
				stderr:   "fatal: 'api-auth' is already checked out\n",
				exitCode: 2,
				err:      actions.ErrFailed,
			},
			wantFields: map[string]any{
				"process.exit.code":    float64(2),
				"weave.action.stderr":  "fatal: 'api-auth' is already checked out",
				"weave.action.trigger": TriggerSetup,
			},
		},
		{
			name:   "one that worked carries what it said about itself",
			result: runResult{dirFile: "/repo/.worktrees/auth\n", stderr: "cloning\n"},
			wantFields: map[string]any{
				"process.exit.code":     float64(0),
				"weave.action.stderr":   "cloning",
				"weave.action.name":     "make-worktree",
				"weave.action.agent.id": "a1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "make-worktree"), []byte("#!/bin/sh\n"), 0o755))

			runner := &recordingRunner{results: map[string]runResult{"make-worktree": tt.result}}
			ctx, records := loggedContext(t)

			_, err := actionService(t, dir, runner).
				RunSetupAction(ctx, agentstore.Agent{ID: "a1", Cwd: "/repo"}, "make-worktree", "")
			if tt.result.err != nil {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			written := records()

			began := recordFor(t, written, "action started")
			assert.Equal(t, "INFO", began["severity_text"])
			assert.Equal(t, TriggerSetup, began["weave.action.trigger"])

			got := recordFor(t, written, "action finished")
			assert.Equal(t, "INFO", got["severity_text"], "the caller reports the refusal, this says a run happened")
			assert.Contains(t, got, "duration_ms")
			for field, want := range tt.wantFields {
				assert.Equal(t, want, got[field], field)
			}
		})
	}
}

// TestRunCleanupActionLogsUnderItsOwnTrigger covers the field that separates the two
// fire-and-forget paths in a log.
func TestRunCleanupActionLogsUnderItsOwnTrigger(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cleanup"), []byte("#!/bin/sh\n"), 0o755))

	runner := &recordingRunner{results: map[string]runResult{"cleanup": {exitCode: 1, err: actions.ErrFailed}}}
	ctx, records := loggedContext(t)

	gone := "/repo/.worktrees/removed"
	var doneErr error
	var doneCalled bool
	require.NoError(t, actionService(t, dir, runner).
		RunCleanupAction(ctx, agentstore.Agent{ID: "a2", Cwd: gone}, "cleanup", func(err error) {
			doneCalled = true
			doneErr = err
		}))

	got := recordFor(t, records(), "action failed")
	assert.Equal(t, TriggerCleanup, got["weave.action.trigger"])
	assert.Equal(t, "a2", got["weave.action.agent.id"])
	assert.Equal(t, gone, got["weave.action.agent.cwd"], "the row is gone, so the record is what names the directory")
	assert.True(t, doneCalled, "the caller learns the run finished, not just that it logged")
	assert.ErrorIs(t, doneErr, actions.ErrFailed)
}

// TestRunCleanupActionLogsWhatAZeroExitSaid covers the only record a refusal leaves. The
// built-in worktree cleanup exits zero when it declines to remove something, since a delete
// must not be blocked, and nothing waits on it to read what it said; a success that dropped
// stderr left the tree on disk with no trace of why.
func TestRunCleanupActionLogsWhatAZeroExitSaid(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cleanup"), []byte("#!/bin/sh\n"), 0o755))

	declined := "worktree cleanup: /repo/wt is on HEAD rather than agent/a3, so it is not this agent's to remove"
	runner := &recordingRunner{results: map[string]runResult{"cleanup": {stderr: declined}}}
	ctx, records := loggedContext(t)

	var doneErr error
	var doneCalled bool
	require.NoError(t, actionService(t, dir, runner).
		RunCleanupAction(ctx, agentstore.Agent{ID: "a3", Cwd: "/repo/wt"}, "cleanup", func(err error) {
			doneCalled = true
			doneErr = err
		}))

	got := recordFor(t, records(), "action finished")
	assert.Equal(t, float64(0), got["process.exit.code"], "the action succeeded")
	assert.Equal(t, declined, got["weave.action.stderr"], "what it declined to do is kept")
	assert.True(t, doneCalled)
	assert.NoError(t, doneErr)
}

// TestRunCleanupActionPersistsStdoutOnAFailure is the real-runner regression for the durable
// teardown log: a cleanup action runs headless, with nothing attached to read its output live,
// so the only account of a failure is what gets captured into the durable log. Most scripts —
// this one included — fail through a plain echo on stdout and a bare exit, never touching
// stderr at all; before Run() captured stdout too, that failure left the log on disk empty.
func TestRunCleanupActionPersistsStdoutOnAFailure(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "teardown"), []byte(
		"#!/bin/sh\necho tearing down 1\necho tearing down 2\nexit 1\n",
	), 0o755))

	logsDir := t.TempDir()
	svc := actionServiceWithLogsDir(t, dir, actions.NewRunner(), logsDir)

	done := make(chan error, 1)
	require.NoError(t, svc.RunCleanupAction(t.Context(), agentstore.Agent{ID: "a1", Cwd: dir}, "teardown", func(err error) {
		done <- err
	}))

	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("the cleanup action never reported finishing")
	}

	persisted, err := os.ReadFile(config.ActionLogPath(logsDir, "a1", config.ActionLogTeardown))
	require.NoError(t, err)
	assert.Contains(t, string(persisted), "tearing down 1")
	assert.Contains(t, string(persisted), "tearing down 2")
}

// TestRunSetupActionPersistsStdoutOnAFailure covers a setup script that fails through a
// plain echo and a bare exit, never touching stderr, and checks the per-agent log, the
// structured log, and the refusal a create call returns all carry what it said.
func TestRunSetupActionPersistsStdoutOnAFailure(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "setup"), []byte(
		"#!/bin/sh\necho diagnosing 1\necho diagnosing 2\nexit 1\n",
	), 0o755))

	logsDir := t.TempDir()
	svc := actionServiceWithLogsDir(t, dir, actions.NewRunner(), logsDir)
	ctx, records := loggedContext(t)

	_, err := svc.RunSetupAction(ctx, agentstore.Agent{ID: "a1"}, "setup", "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "diagnosing 1", "the refusal a create call sees carries what the script said")

	persisted, readErr := os.ReadFile(config.ActionLogPath(logsDir, "a1", config.ActionLogSetup))
	require.NoError(t, readErr)
	assert.Contains(t, string(persisted), "diagnosing 1")
	assert.Contains(t, string(persisted), "diagnosing 2")

	got := recordFor(t, records(), "action finished")
	assert.Contains(t, got, "weave.action.stdout", "the structured log carries the stream that actually failed")
}

// TestActionDetailCapsOutputAndNamesTheLogPath covers the string that becomes a panel
// banner, capped to a size a banner can hold with the cap allocated to stderr first so a
// chatty stdout cannot push the line that actually says what went wrong out of what
// survives.
func TestActionDetailCapsOutputAndNamesTheLogPath(t *testing.T) {
	long := strings.Repeat("x", actionDetailCap+50)

	tests := []struct {
		name         string
		stdout       string
		stderr       string
		logPath      string
		want         string
		wantCut      bool
		wantSurvives string
	}{
		{
			name:         "a short stderr survives a stdout too chatty to fit alongside it",
			stdout:       long,
			stderr:       "fatal: dirty worktree",
			logPath:      "/logs/a1-setup.log",
			wantCut:      true,
			wantSurvives: "fatal: dirty worktree",
		},
		{
			name:    "stdout alone past the cap names where the rest went",
			stdout:  long,
			logPath: "/logs/a1-setup.log",
			wantCut: true,
		},
		{
			name:    "stdout alone past the cap with nowhere to point says so plainly",
			stdout:  long,
			wantCut: true,
		},
		{
			name:   "output inside the cap passes through untouched",
			stderr: "fatal: dirty worktree",
			want:   ": fatal: dirty worktree",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := actionDetail([]byte(tt.stdout), []byte(tt.stderr), tt.logPath)

			if !tt.wantCut {
				assert.Equal(t, tt.want, got)

				return
			}

			kept, _, found := strings.Cut(strings.TrimPrefix(got, ": ..."), " (")
			require.True(t, found, "the cut carries a parenthetical saying what happened to the rest")
			assert.LessOrEqual(t, len(kept), actionDetailCap+1, "one byte of slack for the newline joining both streams")
			if tt.wantSurvives != "" {
				assert.Contains(t, got, tt.wantSurvives, "stderr keeps its own room rather than losing it to stdout")
			} else {
				assert.NotContains(t, got, tt.stdout, "the full tail does not reach the banner")
			}
			if tt.logPath != "" {
				assert.Contains(t, got, tt.logPath, "says where the rest went rather than only that it was cut")
			} else {
				assert.Contains(t, got, "not kept", "says why nothing is named rather than silently capping")
			}
		})
	}
}

func TestActionEnvGuardsAgainstInteractivePrompts(t *testing.T) {
	tests := []struct {
		name       string
		gitAskpass string
		sshAskpass string
	}{
		{name: "nothing configured gets the full guard"},
		{
			name:       "a user-configured askpass survives untouched",
			gitAskpass: "/usr/local/bin/my-askpass",
			sshAskpass: "/usr/local/bin/my-ssh-askpass",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.gitAskpass != "" {
				t.Setenv("GIT_ASKPASS", tt.gitAskpass)
			} else {
				t.Setenv("GIT_ASKPASS", "")
				require.NoError(t, os.Unsetenv("GIT_ASKPASS"))
			}
			if tt.sshAskpass != "" {
				t.Setenv("SSH_ASKPASS", tt.sshAskpass)
			} else {
				t.Setenv("SSH_ASKPASS", "")
				require.NoError(t, os.Unsetenv("SSH_ASKPASS"))
			}

			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "teardown"), []byte(
				"#!/bin/sh\n"+
					"echo \"GIT_TERMINAL_PROMPT=$GIT_TERMINAL_PROMPT\"\n"+
					"echo \"GCM_INTERACTIVE=$GCM_INTERACTIVE\"\n"+
					"echo \"GIT_ASKPASS=$GIT_ASKPASS\"\n"+
					"echo \"SSH_ASKPASS=$SSH_ASKPASS\"\n",
			), 0o755))

			logsDir := t.TempDir()
			svc := actionServiceWithLogsDir(t, dir, actions.NewRunner(), logsDir)

			done := make(chan error, 1)
			require.NoError(t, svc.RunCleanupAction(t.Context(), agentstore.Agent{ID: "a1", Cwd: dir}, "teardown", func(err error) {
				done <- err
			}))

			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(5 * time.Second):
				t.Fatal("the cleanup action never reported finishing")
			}

			persisted, readErr := os.ReadFile(config.ActionLogPath(logsDir, "a1", config.ActionLogTeardown))
			require.NoError(t, readErr)
			output := string(persisted)

			assert.Contains(t, output, "GIT_TERMINAL_PROMPT=0")
			assert.Contains(t, output, "GCM_INTERACTIVE=never")
			assert.Contains(t, output, "GIT_ASKPASS="+tt.gitAskpass)
			assert.Contains(t, output, "SSH_ASKPASS="+tt.sshAskpass)
		})
	}
}

func loggedContext(t *testing.T) (context.Context, func() []map[string]any) {
	t.Helper()

	var written bytes.Buffer

	ctx := logging.WithLogger(context.Background(), logging.New(&written, slog.LevelDebug))

	return ctx, func() []map[string]any {
		records := make([]map[string]any, 0, 2)
		for _, line := range strings.Split(strings.TrimSpace(written.String()), "\n") {
			if line == "" {
				continue
			}

			var record map[string]any
			require.NoError(t, json.Unmarshal([]byte(line), &record))
			records = append(records, record)
		}

		return records
	}
}

func recordFor(t *testing.T, records []map[string]any, body string) map[string]any {
	t.Helper()

	for _, record := range records {
		if record["body"] == body {
			return record
		}
	}

	require.FailNowf(t, "no record was written", "wanted %q, got %v", body, records)

	return nil
}

// TestListActionsReadsTheUserOverTheBuiltins covers the two directories behind the list. A
// file the user writes stands in for the one weave ships of the same name, which is how a
// key is given different behaviour, and the rest of both sets is offered alongside.
func TestListActionsReadsTheUserOverTheBuiltins(t *testing.T) {
	userDir, builtinDir := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(userDir, "_editor"), []byte("#!/bin/sh\n# mine\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(userDir, "open-pr"), []byte("#!/bin/sh\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(builtinDir, "_editor"), []byte("#!/bin/sh\n# shipped\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(builtinDir, "_terminal"), []byte("#!/bin/sh\n"), 0o755))

	service := actionService(t, userDir, &recordingRunner{})
	service.builtinDir = builtinDir

	listed, err := service.ListActions()
	require.NoError(t, err)
	require.Len(t, listed, 3, "one name is claimed twice and counted once")

	names := make([]string, 0, len(listed))
	for _, action := range listed {
		names = append(names, action.Name)
	}
	assert.Equal(t, []string{"_editor", "_terminal", "open-pr"}, names, "in name order across both")

	assert.Equal(t, filepath.Join(userDir, "_editor"), listed[0].Path, "the user's file is the one on offer")
}

// TestRunActionRunsABuiltinAmongItsOwnSiblings covers firing a shipped action. It runs in the
// directory it came out of rather than the user's, since that is where the scripts it sits
// beside are.
func TestRunActionRunsABuiltinAmongItsOwnSiblings(t *testing.T) {
	userDir, builtinDir := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(builtinDir, "_terminal"), []byte("#!/bin/sh\n"), 0o755))

	runner := &recordingRunner{}
	service := actionService(t, userDir, runner)
	service.builtinDir = builtinDir

	require.NoError(t, service.RunAction(context.Background(), "a1", "_terminal"))

	require.Equal(t, []string{"_terminal"}, runner.ran)
	assert.Equal(t, builtinDir, runner.dir)
	assert.Contains(t, runner.env, "WEAVE_ACTIONS_DIR="+builtinDir)
}

// TestEditAction covers opening an action's own file. The editing action is what runs, since
// the panel opens the pane and a script fills it, and the file it opens is named as a path,
// which is the one part of this the panel cannot work out for itself.
func TestEditAction(t *testing.T) {
	userDir, builtinDir := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(userDir, "open-pr"), []byte("#!/bin/sh\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(builtinDir, editActionName), []byte("#!/bin/sh\n"), 0o755))

	runner := &recordingRunner{}
	service := actionService(t, userDir, runner)
	service.builtinDir = builtinDir

	require.NoError(t, service.EditAction(context.Background(), "a1", "open-pr"))

	require.Equal(t, []string{editActionName}, runner.ran)
	assert.Equal(t, builtinDir, runner.dir, "the editing action runs among its own siblings")
	assert.Contains(t, runner.env, editPathVar+"="+filepath.Join(userDir, "open-pr"))
	assert.Contains(t, runner.env, "EDITOR=nvim", "the editor is the one the server was started with")
}

// TestEditActionRefusals covers what will not open. A compiled program is refused rather than
// handed to an editor, which would show a screen of bytes and write back something that is no
// longer a program.
func TestEditActionRefusals(t *testing.T) {
	tests := []struct {
		name    string
		agentID string
		action  string
		wantErr error
	}{
		{
			name:    "an agent that is not there",
			agentID: "gone",
			action:  "open-pr",
			wantErr: ErrAgentNotFound,
		},
		{
			name:    "an action nobody wrote",
			agentID: "a1",
			action:  "never-written",
			wantErr: actions.ErrActionNotFound,
		},
		{
			name:    "a compiled program",
			agentID: "a1",
			action:  "compiled",
			wantErr: ErrNotEditable,
		},
	}

	userDir, builtinDir := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(userDir, "open-pr"), []byte("#!/bin/sh\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(userDir, "compiled"), []byte{0x7f, 'E', 'L', 'F', 0x00}, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(builtinDir, editActionName), []byte("#!/bin/sh\n"), 0o755))

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &recordingRunner{}
			service := actionService(t, userDir, runner)
			service.builtinDir = builtinDir

			err := service.EditAction(context.Background(), tt.agentID, tt.action)

			require.ErrorIs(t, err, tt.wantErr)
			assert.Empty(t, runner.ran, "nothing is started for a refusal")
		})
	}
}

// TestEditActionWithoutTheEditingAction covers a build whose shipped actions are not installed
// and a user who has written no _edit_action either. There is nothing to open the file with, so
// the name that answers to nothing is the one reported.
func TestEditActionWithoutTheEditingAction(t *testing.T) {
	userDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(userDir, "open-pr"), []byte("#!/bin/sh\n"), 0o755))

	runner := &recordingRunner{}
	err := actionService(t, userDir, runner).EditAction(context.Background(), "a1", "open-pr")

	require.ErrorIs(t, err, actions.ErrActionNotFound)
	assert.Contains(t, err.Error(), editActionName)
	assert.Empty(t, runner.ran)
}

// TestActionTimeouts covers how long a run gets. What the file declares wins wherever it was
// fired from, and an action that declares nothing falls back to a ceiling only where a launch
// is blocked on it.
func TestActionTimeouts(t *testing.T) {
	agent := agentstore.Agent{ID: "a1", Cwd: "/repo"}

	tests := []struct {
		name   string
		action string
		fire   func(*ActionService, string) error
		want   time.Duration
	}{
		{
			name:   "a manual run of an action that declares nothing is unbounded",
			action: "bare",
			fire: func(s *ActionService, name string) error {
				return s.RunAction(context.Background(), "a1", name)
			},
			want: 0,
		},
		{
			name:   "a cleanup that declares nothing gets the default ceiling",
			action: "bare",
			fire: func(s *ActionService, name string) error {
				return s.RunCleanupAction(context.Background(), agent, name, nil)
			},
			want: 2 * time.Minute,
		},
		{
			name:   "a cleanup that declares no limit stays unbounded",
			action: "unlimited",
			fire: func(s *ActionService, name string) error {
				return s.RunCleanupAction(context.Background(), agent, name, nil)
			},
			want: 0,
		},
		{
			name:   "a setup action that declares nothing keeps the ceiling a launch needs",
			action: "bare",
			fire: func(s *ActionService, name string) error {
				_, err := s.RunSetupAction(context.Background(), agent, name, "")

				return err
			},
			want: setupTimeout,
		},
		{
			name:   "a setup action that declares one is held to what it declared",
			action: "declared",
			fire: func(s *ActionService, name string) error {
				_, err := s.RunSetupAction(context.Background(), agent, name, "")

				return err
			},
			want: 30 * time.Second,
		},
		{
			name:   "a resume runs the same setup action under the same declaration",
			action: "declared",
			fire: func(s *ActionService, name string) error {
				_, err := s.RunResumeAction(context.Background(), agent, name)

				return err
			},
			want: 30 * time.Second,
		},
		{
			name:   "a manual run of an action that declares one is bounded by it",
			action: "declared",
			fire: func(s *ActionService, name string) error {
				return s.RunAction(context.Background(), "a1", name)
			},
			want: 30 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "bare"), []byte("#!/bin/sh\n"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "declared"), []byte(
				"#!/bin/sh\n#WEAVE timeout=\"30s\"\n",
			), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "unlimited"), []byte(
				"#!/bin/sh\n#WEAVE timeout=\"none\"\n",
			), 0o755))

			runner := &recordingRunner{}
			require.NoError(t, tt.fire(actionService(t, dir, runner), tt.action))

			require.Equal(t, []string{tt.action}, runner.ran)
			assert.Equal(t, tt.want, runner.timeout)
		})
	}
}

// TestRunActionRefusesASecondConcurrentRun covers holding enter on the actions list, or an
// agent calling run_action in a loop, against the same action on the same agent.
func TestRunActionRefusesASecondConcurrentRun(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "slow"), []byte("#!/bin/sh\nsleep 0.3\n"), 0o755))

	svc := actionService(t, dir, actions.NewRunner())

	require.NoError(t, svc.RunAction(context.Background(), "a1", "slow"))

	err := svc.RunAction(context.Background(), "a1", "slow")
	require.ErrorIs(t, err, ErrActionAlreadyRunning)

	require.Eventually(t, func() bool {
		return svc.RunAction(context.Background(), "a1", "slow") == nil
	}, 5*time.Second, 20*time.Millisecond, "the claim is released once the first run finishes")

	time.Sleep(500 * time.Millisecond)
}

// TestRunActionReleasesTheClaimWhenItNeverStarts covers the path where Run refuses
// synchronously and its Done callback never fires at all, which is the one place a claim
// could be stranded forever with nothing left to release it.
func TestRunActionReleasesTheClaimWhenItNeverStarts(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "unrunnable"), []byte("#!/bin/sh\n"), 0o644))

	svc := actionService(t, dir, actions.NewRunner())

	require.ErrorIs(t, svc.RunAction(context.Background(), "a1", "unrunnable"), actions.ErrNotExecutable)

	err := svc.RunAction(context.Background(), "a1", "unrunnable")
	require.ErrorIs(t, err, actions.ErrNotExecutable)
	assert.NotErrorIs(t, err, ErrActionAlreadyRunning, "a refusal that never started must not strand the claim")
}

// TestRunActionConcurrencyGuardIsScopedNarrowly covers what the guard must not catch: a
// different action, a different agent, and a setup action, which is already serialized by
// the create it blocks and must never be refused by this guard on top of that.
func TestRunActionConcurrencyGuardIsScopedNarrowly(t *testing.T) {
	tests := []struct {
		name string
		fire func(t *testing.T, svc *ActionService) error
	}{
		{
			name: "a different action for the same agent runs alongside it",
			fire: func(_ *testing.T, svc *ActionService) error {
				return svc.RunAction(context.Background(), "a1", "other")
			},
		},
		{
			name: "the same action for a different agent runs alongside it",
			fire: func(_ *testing.T, svc *ActionService) error {
				return svc.RunAction(context.Background(), "a2", "slow")
			},
		},
		{
			name: "a setup action is never guarded by the in-flight set",
			fire: func(_ *testing.T, svc *ActionService) error {
				_, err := svc.RunSetupAction(context.Background(), agentstore.Agent{ID: "a1"}, "slow", "")

				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "slow"), []byte("#!/bin/sh\nsleep 0.3\n"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(dir, "other"), []byte("#!/bin/sh\nsleep 0.3\n"), 0o755))

			svc := actionService(t, dir, actions.NewRunner())
			svc.agents = fakeAgentReaderFunc(func(_ context.Context, id string) (*agentstore.Agent, error) {
				return &agentstore.Agent{ID: id}, nil
			})

			require.NoError(t, svc.RunAction(context.Background(), "a1", "slow"))

			assert.NoError(t, tt.fire(t, svc))

			time.Sleep(500 * time.Millisecond)
		})
	}
}

type fakeAgentReaderFunc func(ctx context.Context, id string) (*agentstore.Agent, error)

func (f fakeAgentReaderFunc) GetByID(ctx context.Context, id string) (*agentstore.Agent, error) {
	return f(ctx, id)
}

func actionService(t *testing.T, dir string, runner actionRunner) *ActionService {
	t.Helper()

	return actionServiceWithLogsDir(t, dir, runner, "")
}

func actionServiceWithLogsDir(t *testing.T, dir string, runner actionRunner, logsDir string) *ActionService {
	t.Helper()

	tag := "✓"

	return NewActionService(ActionParams{
		Agents: fakeAgentReader{agent: &agentstore.Agent{
			ID: "a1", Name: "auth", State: agentstore.StateWaiting, Cwd: "/repo",
			Harness: "claude", SessionID: "sess-1", Flags: "--model opus", Prompt: "fix auth",
			Pane: "%9", WorkflowID: "w1", Tag: &tag,
			CreatedAt: time.Date(2026, time.July, 30, 9, 0, 0, 0, time.UTC),
			Metadata:  []agentstore.Metadata{{Key: "pr", Value: "https://example.test/1"}},
		}},
		Workflows: fakeWorkflowReader{workflow: &workflowstore.Workflow{ID: "w1", Name: "backend"}},
		Tmux: fakePaneOwner{
			owner:      "a1",
			hostSocket: "/tmp/host-socket",
			shell:      "/bin/zsh",
			infoPane:   "%3",
			globals: map[string]string{
				tmux.InvokingClientOption: "/dev/ttys004",
			},
		},
		Runner: runner,
		Load: func() (*config.Config, error) {
			cfg := config.Defaults()
			cfg.Actions.Dir = dir

			return cfg, nil
		},
		Addr:    "127.0.0.1:8787",
		Socket:  "weave",
		Editor:  "nvim",
		BinPath: "/opt/weave/bin/weave",
		LogsDir: logsDir,
	})
}

type fakeAgentReader struct {
	agent *agentstore.Agent
}

func (f fakeAgentReader) GetByID(_ context.Context, id string) (*agentstore.Agent, error) {
	if f.agent != nil && f.agent.ID == id {
		return f.agent, nil
	}

	return nil, nil
}

type fakeWorkflowReader struct {
	workflow *workflowstore.Workflow
	archived bool
}

func (f fakeWorkflowReader) Get(ctx context.Context, id string) (*workflowstore.Workflow, error) {
	if f.archived {
		return nil, nil
	}

	return f.GetIncludingArchived(ctx, id)
}

func (f fakeWorkflowReader) GetIncludingArchived(_ context.Context, id string) (*workflowstore.Workflow, error) {
	if f.workflow != nil && f.workflow.ID == id {
		return f.workflow, nil
	}

	return nil, nil
}

type fakePaneOwner struct {
	owner string

	globals map[string]string

	hostSocket string

	shell string

	infoPane string
}

func (f fakePaneOwner) GetPaneOwner(string) string            { return f.owner }
func (f fakePaneOwner) GetHostSocket() string                 { return f.hostSocket }
func (f fakePaneOwner) ShowGlobal(name string) string         { return f.globals[name] }
func (f fakePaneOwner) GetInfoPane() string                   { return f.infoPane }
func (f fakePaneOwner) DefaultShell() string                  { return f.shell }
func (f fakePaneOwner) IsPaneAlive(string) bool               { return true }
func (f fakePaneOwner) RespawnPane(string, string, ...string) {}
func (f fakePaneOwner) WaitFor(context.Context, string) error { return nil }

type recordingRunner struct {
	ran     []string
	dir     string
	env     []string
	timeout time.Duration
	results map[string]runResult
}

type runResult struct {
	dirFile     string
	dirFileSize int
	stderr      string
	exitCode    int
	err         error
}

// Run answers the outcome inline, so a case asserting what reached the log has nothing to
// wait for.
func (r *recordingRunner) Run(p actions.RunParams) error {
	if !p.Action.Executable {
		return actions.ErrNotExecutable
	}

	r.ran = append(r.ran, p.Action.Name)
	r.dir, r.env, r.timeout = p.Dir, p.Env, p.Timeout

	if p.Done != nil {
		answer := r.results[p.Action.Name]
		p.Done(actions.Result{Stderr: []byte(answer.stderr), ExitCode: answer.exitCode}, answer.err)
	}

	return nil
}

func (r *recordingRunner) RunAndWait(_ context.Context, p actions.RunParams) (actions.Result, error) {
	if !p.Action.Executable {
		return actions.Result{}, actions.ErrNotExecutable
	}

	r.ran = append(r.ran, p.Action.Name)
	r.dir, r.env, r.timeout = p.Dir, p.Env, p.Timeout

	answer := r.results[p.Action.Name]

	if path, ok := lookupEnv(p.Env, setupDirFileVar); ok {
		if err := answer.writeDirFile(path); err != nil {
			return actions.Result{}, err
		}
	}

	return actions.Result{
		Stderr:   []byte(answer.stderr),
		ExitCode: answer.exitCode,
	}, answer.err
}

func (r runResult) writeDirFile(path string) error {
	if r.dirFile != "" {
		return os.WriteFile(path, []byte(r.dirFile), 0o644)
	}
	if r.dirFileSize == 0 {
		return nil
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}

	chunk := bytes.Repeat([]byte("a"), 64<<10)
	for left := r.dirFileSize; left > 0; left -= len(chunk) {
		if left < len(chunk) {
			chunk = chunk[:left]
		}
		if _, err := file.Write(chunk); err != nil {
			_ = file.Close()

			return err
		}
	}

	return file.Close()
}

func lookupEnv(env []string, key string) (string, bool) {
	for _, entry := range env {
		if name, value, found := strings.Cut(entry, "="); found && name == key {
			return value, true
		}
	}

	return "", false
}
