package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"weave/config"
	"weave/internal/logging"
	"weave/internal/scripts"
	"weave/internal/store"
	"weave/internal/tmux"
	"weave/internal/tmuxtest"

	agentstore "weave/internal/store/agent"
	runstore "weave/internal/store/run"
	workflowstore "weave/internal/store/workflow"
	"weave/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListHooks pins that a file which cannot be run is still listed, since one written and
// forgotten should show up wanting a chmod rather than never appearing.
func TestListHooks(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "install-deps"), []byte("#!/bin/sh\n#WEAVE description=\"install it\"\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "unrunnable"), []byte("#!/bin/sh\n"), 0o644))

	listed, err := scriptService(t, dir, &recordingRunner{}).ListHooks()
	require.NoError(t, err)
	require.Len(t, listed, 2)

	assert.Equal(t, "install-deps", listed[0].Name)
	assert.True(t, listed[0].Executable)
	assert.Equal(t, "install it", listed[0].Header.Description)

	assert.Equal(t, "unrunnable", listed[1].Name)
	assert.False(t, listed[1].Executable)
}

// TestListHooksWithNoDirectory pins that never having written one is the ordinary case rather
// than a failure.
func TestListHooksWithNoDirectory(t *testing.T) {
	listed, err := scriptService(t, filepath.Join(t.TempDir(), "never-created"), &recordingRunner{}).ListHooks()

	require.NoError(t, err)
	assert.Empty(t, listed)
}

// TestOpenPullRequestRefusals covers everything that can go wrong before the process starts,
// since nothing waits for the built-in once it is running.
func TestOpenPullRequestRefusals(t *testing.T) {
	tests := []struct {
		name        string
		agentID     string
		mode        os.FileMode
		wantIs      error
		leaveUnmade bool
	}{
		{
			name:    "an agent that is not there",
			agentID: "missing",
			mode:    0o755,
			wantIs:  ErrAgentNotFound,
		},
		{
			name:        "a build shipping no such script",
			agentID:     "a1",
			leaveUnmade: true,
			wantIs:      scripts.ErrScriptNotFound,
		},
		{
			name:    "a script nobody made executable",
			agentID: "a1",
			mode:    0o644,
			wantIs:  scripts.ErrNotExecutable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if !tt.leaveUnmade {
				require.NoError(t, os.WriteFile(filepath.Join(dir, openPRScript), []byte("#!/bin/sh\n"), tt.mode))
			}

			runner := &recordingRunner{}
			err := scriptService(t, dir, runner).OpenPullRequest(context.Background(), tt.agentID)

			require.ErrorIs(t, err, tt.wantIs)
			assert.Empty(t, runner.ran, "nothing started")
		})
	}
}

// TestOpenPullRequestPayload covers what a script is handed, including the socket, without which keys
// sent to the pane would go to the user's own tmux server.
func TestOpenPullRequestPayload(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, openPRScript), []byte("#!/bin/sh\n"), 0o755))

	runner := &recordingRunner{}
	service := scriptService(t, dir, runner)

	require.NoError(t, service.OpenPullRequest(context.Background(), "a1"))

	require.Equal(t, []string{openPRScript}, runner.ran)
	assert.Equal(t, dir, runner.dir, "a script starts among its own siblings, not in the agent")

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
	assert.Equal(t, dir, values["WEAVE_SCRIPT_DIR"])
	assert.Equal(t, openPRScript, values["WEAVE_SCRIPT"], "its own name, for a script that wants to tag its own log lines")
	assert.Equal(t, "%3", values["WEAVE_INFO_PANE"], "the pane the panel laid out for a script to fill")
	assert.Equal(t, "/tmp/host-socket", values["WEAVE_HOST_TMUX_SOCKET"])
	assert.Equal(t, "/dev/ttys004", values["WEAVE_HOST_CLIENT"])
	assert.Equal(t, "nvim", values["EDITOR"], "the configured editor, not whatever the service inherited")
	assert.Equal(t, "/bin/zsh", values["WEAVE_SHELL"], "the shell tmux itself opens a pane with")
	assert.True(t, strings.HasPrefix(values["PATH"], "/opt/weave/bin"+string(os.PathListSeparator)),
		"a script that shells out to weave finds this build's own binary first")

	var payload scriptPayload
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

// TestWorktreeEnvOnlyWhenTheAgentIsProvisionedOne covers the environment the two built-in
// worktree scripts read their config from: present, and reflecting the workflow's own settings,
// when its worktree toggle is on and the agent is one the pair provisioned, and absent
// otherwise, since an agent handed somebody else's checkout has no worktree of its own.
func TestWorktreeEnvOnlyWhenTheAgentIsProvisionedOne(t *testing.T) {
	tests := []struct {
		name      string
		worktree  workflowstore.WorktreeConfig
		provision *bool
		wantKeys  []string
	}{
		{
			name: "disabled carries none of the worktree keys",
		},
		{
			name: "enabled carries none of them for an agent that provisions nothing",
			worktree: workflowstore.WorktreeConfig{
				Enabled:    true,
				BaseBranch: "develop",
			},
			provision: xptr.Ptr(false),
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
			builtinDir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(builtinDir, openPRScript), []byte("#!/bin/sh\n"), 0o755))
			worktreesDir := t.TempDir()

			runner := &recordingRunner{}
			service := NewScriptService(ScriptParams{
				Agents: fakeAgentReader{agent: &agentstore.Agent{
					ID:               "a1",
					Cwd:              "/worktrees/a1",
					WorkflowID:       "w1",
					ProvisionEnabled: tt.provision,
				}},
				Workflows: fakeWorkflowReader{workflow: &workflowstore.Workflow{
					ID: "w1", Name: "backend", Cwd: "/repo", Worktree: tt.worktree,
				}},
				Tmux:       fakePaneOwner{},
				Runner:     runner,
				BuiltinDir: builtinDir,
				Load: func() (*config.Config, error) {
					cfg := config.Defaults()
					cfg.Worktrees.Dir = worktreesDir

					return cfg, nil
				},
			})

			require.NoError(t, service.OpenPullRequest(context.Background(), "a1"))

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

// TestRunScriptNamesAnArchivedWorkflow pins an archived workflow still being named, since a
// script told nothing cannot tell that from a workflow with no name.
func TestRunScriptNamesAnArchivedWorkflow(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, openPRScript), []byte("#!/bin/sh\n"), 0o755))

	runner := &recordingRunner{}
	service := scriptService(t, dir, runner)
	service.workflows = fakeWorkflowReader{
		workflow: &workflowstore.Workflow{ID: "w1", Name: "backend"},
		archived: true,
	}

	require.NoError(t, service.OpenPullRequest(context.Background(), "a1"))

	var payload scriptPayload
	for _, entry := range runner.env {
		if value, found := strings.CutPrefix(entry, "WEAVE_AGENT="); found {
			require.NoError(t, json.Unmarshal([]byte(value), &payload))
		}
	}

	assert.Equal(t, "backend", payload.Workflow)
}

// TestRunScriptBoundsALongPrompt pins a long prompt being cut, since everything an exec is
// handed shares a kernel limit that would fail the run instead.
func TestRunScriptBoundsALongPrompt(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, openPRScript), []byte("#!/bin/sh\n"), 0o755))

	runner := &recordingRunner{}
	service := scriptService(t, dir, runner)
	service.agents = fakeAgentReader{agent: &agentstore.Agent{
		ID:     "a1",
		Name:   "auth",
		Cwd:    "/repo",
		Prompt: strings.Repeat("prompt ", maxScriptPrompt),
	}}

	require.NoError(t, service.OpenPullRequest(context.Background(), "a1"))

	var encoded string
	for _, entry := range runner.env {
		if key, value, _ := strings.Cut(entry, "="); key == "WEAVE_AGENT" {
			encoded = value
		}
	}

	var payload scriptPayload
	require.NoError(t, json.Unmarshal([]byte(encoded), &payload))

	assert.True(t, payload.PromptTruncated, "a script showing the prompt can say it is a head of one")
	assert.LessOrEqual(t, len(payload.Prompt), maxScriptPrompt)
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

// TestRunScriptOnADormantAgent pins a script being told the agent is not running, rather than
// left to send keys into a pane somebody else has taken over.
func TestRunScriptOnADormantAgent(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, openPRScript), []byte("#!/bin/sh\n"), 0o755))

	runner := &recordingRunner{}
	service := scriptService(t, dir, runner)
	service.tmux = fakePaneOwner{owner: "somebody-else"}

	require.NoError(t, service.OpenPullRequest(context.Background(), "a1"))

	var payload scriptPayload
	for _, entry := range runner.env {
		if key, value, _ := strings.Cut(entry, "="); key == "WEAVE_AGENT" {
			require.NoError(t, json.Unmarshal([]byte(value), &payload))
		}
	}

	assert.False(t, payload.Alive)
}

// TestOpenPullRequestWithTheAgentDirectoryGone pins the agent's directory being a string in the
// payload, never somewhere the script has to be able to stand.
func TestOpenPullRequestWithTheAgentDirectoryGone(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, openPRScript), []byte("#!/bin/sh\n"), 0o755))

	gone := filepath.Join(t.TempDir(), "removed-worktree")
	runner := &recordingRunner{}
	service := scriptService(t, dir, runner)
	service.agents = fakeAgentReader{agent: &agentstore.Agent{
		ID:   "a1",
		Name: "auth",
		Cwd:  gone,
	}}

	require.NoError(t, service.OpenPullRequest(context.Background(), "a1"))

	assert.Equal(t, dir, runner.dir)

	var payload scriptPayload
	for _, entry := range runner.env {
		if key, value, _ := strings.Cut(entry, "="); key == "WEAVE_AGENT" {
			require.NoError(t, json.Unmarshal([]byte(value), &payload))
		}
	}

	assert.Equal(t, gone, payload.Cwd, "a script works out what to clean from the path itself")
}

// TestRunProvision covers the two things a workflow's provisioner decides about a create,
// which are whether it happens at all and where the agent ends up running.
func TestRunProvision(t *testing.T) {
	tests := []struct {
		name    string
		script  string
		result  runResult
		wantDir string
		wantRan []string
		wantErr bool
	}{
		{
			name:    "one the directory does not hold abandons the launch",
			script:  "was-renamed",
			wantErr: true,
		},
		{
			name:    "one that fails abandons the launch",
			script:  "install-deps",
			result:  runResult{stderr: "no lockfile", err: scripts.ErrFailed},
			wantRan: []string{"install-deps"},
			wantErr: true,
		},
		{
			name:    "one that writes too much to its dir file is refused rather than believed",
			script:  "loud",
			result:  runResult{dirFile: strings.Repeat("a", maxProvisionedDir+1)},
			wantRan: []string{"loud"},
			wantErr: true,
		},
		{
			name:    "one that writes right up to the cap is believed rather than cut short",
			script:  "loud",
			result:  runResult{dirFile: strings.Repeat("a", maxProvisionedDir)},
			wantDir: strings.Repeat("a", maxProvisionedDir),
			wantRan: []string{"loud"},
		},
		{
			name:    "a script that names nothing names no directory",
			script:  "install-deps",
			wantRan: []string{"install-deps"},
		},
		{
			name:    "what it writes to its dir file is where the agent runs",
			script:  "make-worktree",
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

			runner := &recordingRunner{results: map[string]runResult{tt.script: tt.result}}
			service := scriptService(t, dir, runner)

			got, err := service.RunProvision(t.Context(), agentstore.Agent{ID: "a1", Name: "auth"}, tt.script, "")

			if tt.wantErr {
				require.ErrorIs(t, err, ErrProvisionFailed)
				assert.Empty(t, got, "a refused setup names no directory")
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantDir, got)
			}
			assert.Equal(t, tt.wantRan, runner.ran)
		})
	}
}

// TestRunProvisionDoesNotHoldTheWholeDirFile pins that a setup writing a large log is
// streamed rather than held in memory.
func TestRunProvisionDoesNotHoldTheWholeDirFile(t *testing.T) {
	const wrote = 32 << 20

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "greedy"), []byte("#!/bin/sh\n"), 0o755))

	runner := &recordingRunner{results: map[string]runResult{"greedy": {dirFileSize: wrote}}}
	service := scriptService(t, dir, runner)

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)

	got, err := service.RunProvision(t.Context(), agentstore.Agent{ID: "a1", Name: "auth"}, "greedy", "")

	runtime.ReadMemStats(&after)

	require.ErrorIs(t, err, ErrProvisionFailed)
	assert.Empty(t, got, "a refused setup names no directory")
	assert.Contains(t, err.Error(), setupDirFileVar)
	assert.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(4<<20),
		"the refusal read the whole dir file to arrive at it")
}

// TestRunProvisionTellsTheScriptWhatSetItOff covers WEAVE_TRIGGER, which is how a script that
// only makes sense at one moment can refuse the others itself.
func TestRunProvisionTellsTheScriptWhatSetItOff(t *testing.T) {
	tests := []struct {
		name        string
		fire        func(*ScriptService, *recordingRunner) error
		wantTrigger string
	}{
		{
			name: "fired by hand",
			fire: func(s *ScriptService, _ *recordingRunner) error {
				return s.OpenPullRequest(t.Context(), "a1")
			},
			wantTrigger: TriggerOpenPR,
		},
		{
			name: "fired as a workflow's setup",
			fire: func(s *ScriptService, _ *recordingRunner) error {
				_, err := s.RunProvision(t.Context(), agentstore.Agent{ID: "a1"}, "make-worktree", "")
				return err
			},
			wantTrigger: TriggerProvision,
		},
		{
			name: "fired as a workflow's setup on the way back up",
			fire: func(s *ScriptService, _ *recordingRunner) error {
				_, err := s.RunResume(t.Context(), agentstore.Agent{ID: "a1"}, "make-worktree")
				return err
			},
			wantTrigger: TriggerResume,
		},
		{
			name: "fired as a workflow's cleanup",
			fire: func(s *ScriptService, _ *recordingRunner) error {
				return s.RunDeprovision(t.Context(), agentstore.Agent{ID: "a1"}, "make-worktree", nil)
			},
			wantTrigger: TriggerDeprovision,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "make-worktree"), []byte("#!/bin/sh\n"), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(dir, openPRScript), []byte("#!/bin/sh\n"), 0o755))

			runner := &recordingRunner{}
			require.NoError(t, tt.fire(scriptService(t, dir, runner), runner))

			assert.Contains(t, runner.env, "WEAVE_TRIGGER="+tt.wantTrigger)
		})
	}
}

// TestRunProvisionCarriesTheReason pins the script's stderr travelling with the refusal,
// since it is the only thing that says why.
func TestRunProvisionCarriesTheReason(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "make-worktree"), []byte("#!/bin/sh\n"), 0o755))

	runner := &recordingRunner{results: map[string]runResult{
		"make-worktree": {stderr: "fatal: 'api-auth' is already checked out", err: scripts.ErrFailed},
	}}

	_, err := scriptService(t, dir, runner).RunProvision(t.Context(), agentstore.Agent{ID: "a1"}, "make-worktree", "")

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
	svc := NewScriptService(ScriptParams{
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

		result, err := svc.runInPane(t.Context(), scripts.Script{
			Name:       "ok",
			Path:       script,
			Executable: true,
		}, []string{"FOO=api-auth"}, pane, "", 2*time.Second)

		require.NoError(t, err)
		assert.Equal(t, 0, result.ExitCode)
		assert.Contains(t, string(result.Stderr), "working in api-auth", "captured alongside streaming live to the pane")
	})

	t.Run("a script that fails", func(t *testing.T) {
		pane := freshPane(t)
		script := filepath.Join(dir, "bad")
		require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\necho 'fatal: already checked out' >&2\nexit 7\n"), 0o755))

		result, err := svc.runInPane(t.Context(), scripts.Script{
			Name:       "bad",
			Path:       script,
			Executable: true,
		}, nil, pane, "", 2*time.Second)

		require.Error(t, err)
		require.ErrorIs(t, err, scripts.ErrFailed)
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

		result, err := svc.runInPane(t.Context(), scripts.Script{
			Name:       "stdout-only",
			Path:       script,
			Executable: true,
		}, nil, pane, logPath, 2*time.Second)

		require.Error(t, err)
		require.ErrorIs(t, err, scripts.ErrFailed)
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

		result, err := svc.runInPane(t.Context(), scripts.Script{
			Name:       "slow",
			Path:       script,
			Executable: true,
		}, nil, pane, logPath, 200*time.Millisecond)

		require.Error(t, err)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Contains(t, string(result.Stderr), "gave up after 200ms", "the reason is not silently lost")
		persisted, readErr := os.ReadFile(logPath)
		require.NoError(t, readErr)
		assert.Contains(t, string(persisted), "gave up after 200ms", "and it lands in the durable log a person cycles to later")
	})

	t.Run("a pane that is already gone", func(t *testing.T) {
		pane := freshPane(t)
		server.Run("kill-pane", "-t", pane)

		script := filepath.Join(dir, "ok")
		result, err := svc.runInPane(t.Context(), scripts.Script{
			Name:       "ok",
			Path:       script,
			Executable: true,
		}, nil, pane, "", time.Second)

		require.ErrorIs(t, err, scripts.ErrNotStartable)
		assert.Equal(t, -1, result.ExitCode, "a process that never ran has no status of its own")
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
		result, err := svc.runInPane(t.Context(), scripts.Script{
			Name:       "slow",
			Path:       script,
			Executable: true,
		}, nil, pane, logPath, 30*time.Second)
		elapsed := time.Since(began)

		require.Error(t, err)
		require.ErrorIs(t, err, errPaneDied)
		assert.Less(t, elapsed, 5*time.Second, "noticed the pane dying rather than waiting out the full timeout")
		assert.Contains(t, string(result.Stderr), "pane closed before the script finished")
		persisted, readErr := os.ReadFile(logPath)
		require.NoError(t, readErr)
		assert.Contains(t, string(persisted), "pane closed before the script finished", "left for a person cycling to the setup log to read")
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

// TestRunDeprovisionRefusesOneThatIsGone pins a renamed script being reported rather than
// pretended over, leaving the caller to decide the agent is still removable.
func TestRunDeprovisionRefusesOneThatIsGone(t *testing.T) {
	err := scriptService(t, t.TempDir(), &recordingRunner{}).
		RunDeprovision(t.Context(), agentstore.Agent{ID: "a1"}, "was-renamed", nil)

	require.ErrorIs(t, err, scripts.ErrScriptNotFound)
}

// TestOpenPullRequestLogsWhatItDid covers the only account there is of a script nothing waits on.
func TestOpenPullRequestLogsWhatItDid(t *testing.T) {
	tests := []struct {
		name       string
		result     runResult
		wantLevel  string
		wantBody   string
		wantFields map[string]any
	}{
		{
			name:      "one that worked is still an account of a run",
			wantLevel: "INFO",
			wantBody:  "script finished",
			wantFields: map[string]any{
				"process.exit.code":     float64(0),
				"weave.script.stderr":   nil,
				"weave.script.name":     openPRScript,
				"weave.run.trigger":     TriggerOpenPR,
				"weave.script.agent.id": "a1",
			},
		},
		{
			name: "one that failed carries the status and what it said",
			result: runResult{
				stderr:   "fatal: '/repo' is dirty\n",
				exitCode: 3,
				err:      scripts.ErrFailed,
			},
			wantLevel: "ERROR",
			wantBody:  "script failed",
			wantFields: map[string]any{
				"process.exit.code":      float64(3),
				"weave.script.stderr":    "fatal: '/repo' is dirty",
				"weave.script.agent.cwd": "/repo",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, openPRScript), []byte("#!/bin/sh\n"), 0o755))

			runner := &recordingRunner{results: map[string]runResult{openPRScript: tt.result}}
			ctx, records := loggedContext(t)

			require.NoError(t, scriptService(t, dir, runner).OpenPullRequest(ctx, "a1"))

			written := records()

			began := recordFor(t, written, "script started")
			assert.Equal(t, "INFO", began["severity_text"])
			assert.Equal(t, openPRScript, began["weave.script.name"])

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

// TestProvisionerLogsBothEnds pins the log saying a run happened and what came of it, which is
// the half nothing else records.
func TestProvisionerLogsBothEnds(t *testing.T) {
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
				err:      scripts.ErrFailed,
			},
			wantFields: map[string]any{
				"process.exit.code":   float64(2),
				"weave.script.stderr": "fatal: 'api-auth' is already checked out",
				"weave.run.trigger":   TriggerProvision,
			},
		},
		{
			name:   "one that worked carries what it said about itself",
			result: runResult{dirFile: "/repo/.worktrees/auth\n", stderr: "cloning\n"},
			wantFields: map[string]any{
				"process.exit.code":     float64(0),
				"weave.script.stderr":   "cloning",
				"weave.script.name":     "make-worktree",
				"weave.script.agent.id": "a1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "make-worktree"), []byte("#!/bin/sh\n"), 0o755))

			runner := &recordingRunner{results: map[string]runResult{"make-worktree": tt.result}}
			ctx, records := loggedContext(t)

			_, err := scriptService(t, dir, runner).
				RunProvision(ctx, agentstore.Agent{ID: "a1", Cwd: "/repo"}, "make-worktree", "")
			if tt.result.err != nil {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			written := records()

			began := recordFor(t, written, "script started")
			assert.Equal(t, "INFO", began["severity_text"])
			assert.Equal(t, TriggerProvision, began["weave.run.trigger"])

			got := recordFor(t, written, "script finished")
			assert.Equal(t, "INFO", got["severity_text"], "the caller reports the refusal, this says a run happened")
			assert.Contains(t, got, "duration_ms")
			for field, want := range tt.wantFields {
				assert.Equal(t, want, got[field], field)
			}
		})
	}
}

// TestRunDeprovisionLogsUnderItsOwnTrigger covers the field that separates the two
// fire-and-forget paths in a log.
func TestRunDeprovisionLogsUnderItsOwnTrigger(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cleanup"), []byte("#!/bin/sh\n"), 0o755))

	runner := &recordingRunner{results: map[string]runResult{"cleanup": {exitCode: 1, err: scripts.ErrFailed}}}
	ctx, records := loggedContext(t)

	gone := "/repo/.worktrees/removed"
	var doneErr error
	var doneCalled bool
	require.NoError(t, scriptService(t, dir, runner).
		RunDeprovision(ctx, agentstore.Agent{ID: "a2", Cwd: gone}, "cleanup", func(err error) {
			doneCalled = true
			doneErr = err
		}))

	got := recordFor(t, records(), "script failed")
	assert.Equal(t, TriggerDeprovision, got["weave.run.trigger"])
	assert.Equal(t, "a2", got["weave.script.agent.id"])
	assert.Equal(t, gone, got["weave.script.agent.cwd"], "the row is gone, so the record is what names the directory")
	assert.True(t, doneCalled, "the caller learns the run finished, not just that it logged")
	assert.ErrorIs(t, doneErr, scripts.ErrFailed)
}

// TestRunDeprovisionLogsWhatAZeroExitSaid pins stderr being kept from a zero exit, since the
// worktree cleanup declines that way and would otherwise leave a tree on disk with no trace.
func TestRunDeprovisionLogsWhatAZeroExitSaid(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cleanup"), []byte("#!/bin/sh\n"), 0o755))

	declined := "worktree cleanup: /repo/wt is on HEAD rather than agent/a3, so it is not this agent's to remove"
	runner := &recordingRunner{results: map[string]runResult{"cleanup": {stderr: declined}}}
	ctx, records := loggedContext(t)

	var doneErr error
	var doneCalled bool
	require.NoError(t, scriptService(t, dir, runner).
		RunDeprovision(ctx, agentstore.Agent{ID: "a3", Cwd: "/repo/wt"}, "cleanup", func(err error) {
			doneCalled = true
			doneErr = err
		}))

	got := recordFor(t, records(), "script finished")
	assert.EqualValues(t, 0, got["process.exit.code"], "the script succeeded")
	assert.Equal(t, declined, got["weave.script.stderr"], "what it declined to do is kept")
	assert.True(t, doneCalled)
	assert.NoError(t, doneErr)
}

// TestRunDeprovisionPersistsStdoutOnAFailure pins stdout being captured too, since a script
// that fails through a plain echo and a bare exit would otherwise leave the log empty.
func TestRunDeprovisionPersistsStdoutOnAFailure(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "teardown"), []byte(
		"#!/bin/sh\necho tearing down 1\necho tearing down 2\nexit 1\n",
	), 0o755))

	logsDir := t.TempDir()
	svc := scriptServiceWithLogsDir(t, dir, scripts.NewRunner(), logsDir)

	done := make(chan error, 1)
	require.NoError(t, svc.RunDeprovision(t.Context(), agentstore.Agent{ID: "a1", Cwd: dir}, "teardown", func(err error) {
		done <- err
	}))

	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("the deprovisioner never reported finishing")
	}

	persisted, err := os.ReadFile(onlyRunLog(t, logsDir))
	require.NoError(t, err)
	assert.Contains(t, string(persisted), "tearing down 1")
	assert.Contains(t, string(persisted), "tearing down 2")
}

// TestRunProvisionPersistsStdoutOnAFailure covers a provisioner that fails through a
// plain echo and a bare exit, never touching stderr, and checks the per-agent log, the
// structured log, and the refusal a create call returns all carry what it said.
func TestRunProvisionPersistsStdoutOnAFailure(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "setup"), []byte(
		"#!/bin/sh\necho diagnosing 1\necho diagnosing 2\nexit 1\n",
	), 0o755))

	logsDir := t.TempDir()
	svc := scriptServiceWithLogsDir(t, dir, scripts.NewRunner(), logsDir)
	ctx, records := loggedContext(t)

	_, err := svc.RunProvision(ctx, agentstore.Agent{ID: "a1"}, "setup", "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "diagnosing 1", "the refusal a create call sees carries what the script said")

	persisted, readErr := os.ReadFile(onlyRunLog(t, logsDir))
	require.NoError(t, readErr)
	assert.Contains(t, string(persisted), "diagnosing 1")
	assert.Contains(t, string(persisted), "diagnosing 2")

	got := recordFor(t, records(), "script finished")
	assert.Contains(t, got, "weave.script.stdout", "the structured log carries the stream that actually failed")
}

// TestScriptDetailCapsOutputAndNamesTheLogPath covers the string that becomes a panel
// banner, capped to a size a banner can hold with the cap allocated to stderr first so a
// chatty stdout cannot push the line that actually says what went wrong out of what
// survives.
func TestScriptDetailCapsOutputAndNamesTheLogPath(t *testing.T) {
	long := strings.Repeat("x", scriptDetailCap+50)

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
			got := scriptDetail([]byte(tt.stdout), []byte(tt.stderr), tt.logPath)

			if !tt.wantCut {
				assert.Equal(t, tt.want, got)

				return
			}

			kept, _, found := strings.Cut(strings.TrimPrefix(got, ": ..."), " (")
			require.True(t, found, "the cut carries a parenthetical saying what happened to the rest")
			assert.LessOrEqual(t, len(kept), scriptDetailCap+1, "one byte of slack for the newline joining both streams")
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

// TestScriptEnvGuardsAgainstInteractivePrompts pins that a script is run with prompts turned
// off, while an askpass the user configured is left as it was.
func TestScriptEnvGuardsAgainstInteractivePrompts(t *testing.T) {
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
			svc := scriptServiceWithLogsDir(t, dir, scripts.NewRunner(), logsDir)

			done := make(chan error, 1)
			require.NoError(t, svc.RunDeprovision(t.Context(), agentstore.Agent{ID: "a1", Cwd: dir}, "teardown", func(err error) {
				done <- err
			}))

			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(5 * time.Second):
				t.Fatal("the deprovisioner never reported finishing")
			}

			persisted, readErr := os.ReadFile(onlyRunLog(t, logsDir))
			require.NoError(t, readErr)
			output := string(persisted)

			assert.Contains(t, output, "GIT_TERMINAL_PROMPT=0")
			assert.Contains(t, output, "GCM_INTERACTIVE=never")
			assert.Contains(t, output, "GIT_ASKPASS="+tt.gitAskpass)
			assert.Contains(t, output, "SSH_ASKPASS="+tt.sshAskpass)
		})
	}
}

// TestRunHookRunsInTheAgentsDirectory drives a real script through the runner, since
// where a hook runs is the whole point of one: it is there to work on the checkout rather than
// on the directory the script itself lives in.
func TestRunHookRunsInTheAgentsDirectory(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "install-deps"), []byte(
		"#!/bin/sh\nset -eu\necho installing in \"$PWD\"\npwd >installed\nprintf '%s' \"$WEAVE_TRIGGER\" >>installed\n",
	), 0o755))

	logsDir := t.TempDir()
	worktree := t.TempDir()
	svc := scriptServiceWithLogsDir(t, dir, scripts.NewRunner(), logsDir)

	err := svc.RunHook(t.Context(), agentstore.Agent{ID: "a1", Cwd: worktree}, "install-deps", workflowstore.HookPreLaunch)
	require.NoError(t, err)

	landed, readErr := os.ReadFile(filepath.Join(worktree, "installed"))
	require.NoError(t, readErr, "the hook ran in the agent's own directory rather than the script's")
	assert.Contains(t, string(landed), workflowstore.HookPreLaunch, "and read the moment out of WEAVE_TRIGGER")

	runs := config.ListRunLogs(logsDir, "a1")
	require.Len(t, runs, 1)
	assert.Equal(t, workflowstore.HookPreLaunch, runs[0].Trigger)
	assert.Equal(t, "install-deps", runs[0].Script)

	logged, logErr := os.ReadFile(runs[0].Path)
	require.NoError(t, logErr)
	assert.Contains(t, string(logged), "installing in", "and its output is kept under the run that made it")
}

// TestRunHookCarriesWhatTheScriptSaid pins the refusal a failing hook hands back, since
// that string is what abandons a create and is the only place a person sees why.
func TestRunHookCarriesWhatTheScriptSaid(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "install-deps"), []byte(
		"#!/bin/sh\necho no lockfile here >&2\nexit 3\n",
	), 0o755))

	svc := scriptServiceWithLogsDir(t, dir, scripts.NewRunner(), t.TempDir())

	err := svc.RunHook(t.Context(), agentstore.Agent{ID: "a1", Cwd: t.TempDir()}, "install-deps", workflowstore.HookPreLaunch)

	require.ErrorIs(t, err, ErrHookFailed)
	assert.Contains(t, err.Error(), "install-deps")
	assert.Contains(t, err.Error(), "no lockfile here")
}

// TestRunHookRefusesADirectoryThatIsGone pins a hook with nowhere to run failing rather
// than relocating, since a script that installs into the scripts directory instead of the
// worktree is worse than one that says it could not run.
func TestRunHookRefusesADirectoryThatIsGone(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "install-deps"), []byte("#!/bin/sh\nexit 0\n"), 0o755))

	svc := scriptServiceWithLogsDir(t, dir, scripts.NewRunner(), t.TempDir())

	err := svc.RunHook(t.Context(), agentstore.Agent{ID: "a1", Cwd: "/definitely/not/here"}, "install-deps", workflowstore.HookPreLaunch)

	require.ErrorIs(t, err, ErrHookFailed)
	assert.Contains(t, err.Error(), "/definitely/not/here")
}

// TestARefusedRunLeavesNoRunBehind pins a refusal spending no number, since a
// file claimed by a run that never happened would push a real one out of the fifty kept.
func TestARefusedRunLeavesNoRunBehind(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, openPRScript), []byte("#!/bin/sh\nexit 0\n"), 0o644))

	logsDir := t.TempDir()
	svc := scriptServiceWithLogsDir(t, dir, scripts.NewRunner(), logsDir)

	err := svc.OpenPullRequest(t.Context(), "a1")

	require.Error(t, err, "a file nobody made executable is not startable")
	assert.Empty(t, config.ListRunLogs(logsDir, "a1"), "and it leaves no run behind")
}

// fakeRunRecorder stands in for the run table, holding what a test wants to read back about
// the rows the service opened and closed.
type fakeRunRecorder struct {
	mu        sync.Mutex
	begun     []runstore.BeginParams
	finished  []runstore.FinishParams
	abandoned []string
	pruned    []string
}

func (f *fakeRunRecorder) Begin(_ context.Context, params runstore.BeginParams) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.begun = append(f.begun, params)

	return fmt.Sprintf("run-%d", len(f.begun)), nil
}

func (f *fakeRunRecorder) Finish(_ context.Context, params runstore.FinishParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.finished = append(f.finished, params)

	return nil
}

func (f *fakeRunRecorder) Abandon(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.abandoned = append(f.abandoned, id)

	return nil
}

func (f *fakeRunRecorder) Prune(_ context.Context, agentID string, _ int) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.pruned = append(f.pruned, agentID)

	return nil
}

func (f *fakeRunRecorder) rows() ([]runstore.BeginParams, []runstore.FinishParams) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.begun, f.finished
}

func scriptServiceRecording(t *testing.T, dir string, runner scriptRunner, logsDir string) (*ScriptService, *fakeRunRecorder) {
	t.Helper()

	recorder := &fakeRunRecorder{}
	svc := scriptServiceWithLogsDir(t, dir, runner, logsDir)
	svc.runs = recorder

	return svc, recorder
}

// TestRunHookRecordsTheRun covers the row a hook leaves behind, which is what puts a failure on
// the agent's own line instead of only in the service log.
func TestRunHookRecordsTheRun(t *testing.T) {
	tests := []struct {
		name         string
		script       string
		cwd          func(t *testing.T) string
		wantExitCode *int32
		wantFailure  string
	}{
		{
			name:         "a hook that succeeded",
			script:       "#!/bin/sh\necho installing\n",
			cwd:          func(t *testing.T) string { t.Helper(); return t.TempDir() },
			wantExitCode: xptr.Ptr(int32(0)),
		},
		{
			name:         "a hook that exited badly",
			script:       "#!/bin/sh\necho no lockfile here >&2\nexit 3\n",
			cwd:          func(t *testing.T) string { t.Helper(); return t.TempDir() },
			wantExitCode: xptr.Ptr(int32(3)),
			wantFailure:  "no lockfile here",
		},
		{
			name:        "a hook refused before it started has no status of its own",
			script:      "#!/bin/sh\nexit 0\n",
			cwd:         func(*testing.T) string { return "/definitely/not/here" },
			wantFailure: "unusable working directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "install-deps"), []byte(tt.script), 0o755))

			logsDir := t.TempDir()
			svc, recorder := scriptServiceRecording(t, dir, scripts.NewRunner(), logsDir)

			err := svc.RunHook(t.Context(), agentstore.Agent{ID: "a1", Cwd: tt.cwd(t)}, "install-deps", workflowstore.HookPostLaunch)
			if tt.wantFailure == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}

			begun, finished := recorder.rows()

			require.Len(t, begun, 1)
			assert.Equal(t, "a1", begun[0].AgentID)
			assert.Equal(t, workflowstore.HookPostLaunch, begun[0].Trigger)
			assert.Equal(t, "install-deps", begun[0].Script)
			require.NotNil(t, begun[0].LogNumber, "the row points at the log the output went to")
			assert.Equal(t, int32(1), *begun[0].LogNumber)

			require.Len(t, finished, 1)
			assert.Equal(t, "run-1", finished[0].ID)
			assert.Equal(t, tt.wantExitCode, finished[0].ExitCode)
			if tt.wantFailure == "" {
				assert.Empty(t, finished[0].Failure)

				return
			}
			assert.Contains(t, finished[0].Failure, "install-deps", "the row says which script it was")
			assert.Contains(t, finished[0].Failure, tt.wantFailure)
		})
	}
}

// TestARefusedRunDropsItsRow pins the row going with the empty log file, since a refusal is not
// a run and one recorded as such would spend a number and stand as a failure to be read.
func TestARefusedRunDropsItsRow(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, openPRScript), []byte("#!/bin/sh\nexit 0\n"), 0o644))

	svc, recorder := scriptServiceRecording(t, dir, scripts.NewRunner(), t.TempDir())

	require.Error(t, svc.OpenPullRequest(t.Context(), "a1"), "a file nobody made executable is not startable")

	begun, finished := recorder.rows()
	require.Len(t, begun, 1)
	assert.Empty(t, finished)
	assert.Equal(t, []string{"run-1"}, recorder.abandoned)
}

// TestAProvisionerRefusedInItsPaneSaysSo covers the pane path's own refusals, where the script
// never runs and so writes nothing: the row must not read as a clean exit and the number claimed
// for it must not open an empty pager.
func TestAProvisionerRefusedInItsPaneSaysSo(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "setup"), []byte("#!/bin/sh\nexit 0\n"), 0o644))

	logsDir := t.TempDir()
	svc, recorder := scriptServiceRecording(t, dir, scripts.NewRunner(), logsDir)

	_, err := svc.RunProvision(t.Context(), agentstore.Agent{ID: "a1"}, "setup", "%3")

	require.ErrorIs(t, err, ErrProvisionFailed)

	_, finished := recorder.rows()
	require.Len(t, finished, 1)
	assert.Nil(t, finished[0].ExitCode, "a script nobody made executable never exited at all")
	assert.Contains(t, finished[0].Failure, "setup")

	logged, readErr := os.ReadFile(onlyRunLog(t, logsDir))
	require.NoError(t, readErr)
	assert.Contains(t, string(logged), "setup", "and the run the mark points at says why rather than opening empty")
}

// TestClaimRunLogNumbersEveryRun pins each run getting a file of its own rather than the
// two fixed names that came before, since a resume used to overwrite the log of the create.
func TestClaimRunLogNumbersEveryRun(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "setup"), []byte("#!/bin/sh\necho made it\n"), 0o755))

	logsDir := t.TempDir()
	svc := scriptServiceWithLogsDir(t, dir, scripts.NewRunner(), logsDir)

	for range 2 {
		_, err := svc.RunProvision(t.Context(), agentstore.Agent{ID: "a1"}, "setup", "")
		require.NoError(t, err)
	}

	runs := config.ListRunLogs(logsDir, "a1")

	require.Len(t, runs, 2, "the second run does not overwrite the first")
	assert.Equal(t, 2, runs[0].Number)
	assert.Equal(t, 1, runs[1].Number)
	assert.Equal(t, config.TriggerProvision, runs[0].Trigger)
	assert.Equal(t, "setup", runs[0].Script)

	for _, run := range runs {
		content, readErr := os.ReadFile(run.Path)
		require.NoError(t, readErr)
		assert.Contains(t, string(content), "made it", "each run holds its own output")
	}
}

// TestClaimRunLogPrunesTheOldestRuns pins the pruning, since an agent resumed daily for a
// year would otherwise keep a file for every one of them.
func TestClaimRunLogPrunesTheOldestRuns(t *testing.T) {
	dir := t.TempDir()
	logsDir := t.TempDir()
	svc := scriptServiceWithLogsDir(t, dir, scripts.NewRunner(), logsDir)

	logDir := config.RunLogDir(logsDir, "a1")
	require.NoError(t, os.MkdirAll(logDir, 0o755))
	for number := 1; number <= config.MaxRunLogs; number++ {
		name := config.RunLogName(number, config.TriggerOpenPR, openPRScript)
		require.NoError(t, os.WriteFile(filepath.Join(logDir, name), []byte("out\n"), 0o644))
	}

	path, number := svc.claimRunLog(t.Context(), "a1", config.TriggerOpenPR, openPRScript)
	require.NotEmpty(t, path)
	assert.Equal(t, config.MaxRunLogs+1, number)

	runs := config.ListRunLogs(logsDir, "a1")

	require.Len(t, runs, config.MaxRunLogs)
	assert.Equal(t, config.MaxRunLogs+1, runs[0].Number, "the newest run is the one just claimed")
	assert.Equal(t, 2, runs[len(runs)-1].Number, "the oldest went to make room for it")
}

// TestClaimRunLogStepsPastANameAlreadyTaken pins the exclusive create, since a manual
// script and a launch can begin at the same moment and neither may write over the other.
func TestClaimRunLogStepsPastANameAlreadyTaken(t *testing.T) {
	logsDir := t.TempDir()
	svc := scriptServiceWithLogsDir(t, t.TempDir(), scripts.NewRunner(), logsDir)

	logDir := config.RunLogDir(logsDir, "a1")
	require.NoError(t, os.MkdirAll(logDir, 0o755))
	taken := filepath.Join(logDir, config.RunLogName(1, config.TriggerOpenPR, openPRScript))
	require.NoError(t, os.WriteFile(taken, []byte("someone else\n"), 0o644))

	path, number := svc.claimRunLog(t.Context(), "a1", config.TriggerOpenPR, openPRScript)

	assert.NotEqual(t, taken, path, "the name already there is left as it stands")
	assert.Equal(t, 2, number, "it steps past the number that was taken")
	content, err := os.ReadFile(taken)
	require.NoError(t, err)
	assert.Contains(t, string(content), "someone else")
}

// fixtureAgentID is the agent every script test in this file runs against.
const fixtureAgentID = "a1"

// onlyRunLog is the one run the fixture's agent has recorded, since a test that ran a single
// script has no reason to know which number it was given.
func onlyRunLog(t *testing.T, logsDir string) string {
	t.Helper()

	runs := config.ListRunLogs(logsDir, fixtureAgentID)
	require.Len(t, runs, 1)

	return runs[0].Path
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

// TestScriptTimeouts covers how long a run gets, where the file's own declaration wins and
// only a launch blocked on the script falls back to a ceiling.
func TestScriptTimeouts(t *testing.T) {
	agent := agentstore.Agent{ID: "a1", Cwd: t.TempDir()}

	tests := []struct {
		name   string
		script string
		fire   func(*ScriptService, string) error
		want   time.Duration
	}{
		{
			name:   "the built-in behind the panel's own key is unbounded",
			script: openPRScript,
			fire: func(s *ScriptService, _ string) error {
				return s.OpenPullRequest(context.Background(), "a1")
			},
			want: 0,
		},
		{
			name:   "a cleanup that declares nothing gets the default ceiling",
			script: "bare",
			fire: func(s *ScriptService, name string) error {
				return s.RunDeprovision(context.Background(), agent, name, nil)
			},
			want: 2 * time.Minute,
		},
		{
			name:   "a cleanup that declares no limit stays unbounded",
			script: "unlimited",
			fire: func(s *ScriptService, name string) error {
				return s.RunDeprovision(context.Background(), agent, name, nil)
			},
			want: 0,
		},
		{
			name:   "a provisioner that declares nothing keeps the ceiling a launch needs",
			script: "bare",
			fire: func(s *ScriptService, name string) error {
				_, err := s.RunProvision(context.Background(), agent, name, "")

				return err
			},
			want: provisionTimeout,
		},
		{
			name:   "a provisioner that declares one is held to what it declared",
			script: "declared",
			fire: func(s *ScriptService, name string) error {
				_, err := s.RunProvision(context.Background(), agent, name, "")

				return err
			},
			want: 30 * time.Second,
		},
		{
			name:   "a resume runs the same provisioner under the same declaration",
			script: "declared",
			fire: func(s *ScriptService, name string) error {
				_, err := s.RunResume(context.Background(), agent, name)

				return err
			},
			want: 30 * time.Second,
		},
		{
			name:   "a hook that declares one is bounded by it",
			script: "declared",
			fire: func(s *ScriptService, name string) error {
				return s.RunHook(context.Background(), agent, name, workflowstore.HookPreLaunch)
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
			require.NoError(t, os.WriteFile(filepath.Join(dir, openPRScript), []byte("#!/bin/sh\n"), 0o755))

			runner := &recordingRunner{}
			require.NoError(t, tt.fire(scriptService(t, dir, runner), tt.script))

			require.Equal(t, []string{tt.script}, runner.ran)
			assert.Equal(t, tt.want, runner.timeout)
		})
	}
}

// TestOpenPullRequestRefusesASecondConcurrentRun covers holding the panel's own key down, which is
// the one way a person can ask for the same script on the same agent twice at once.
func TestOpenPullRequestRefusesASecondConcurrentRun(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, openPRScript), []byte("#!/bin/sh\nsleep 0.3\n"), 0o755))

	svc := scriptService(t, dir, scripts.NewRunner())

	require.NoError(t, svc.OpenPullRequest(context.Background(), "a1"))

	err := svc.OpenPullRequest(context.Background(), "a1")
	require.ErrorIs(t, err, ErrScriptAlreadyRunning)

	require.Eventually(t, func() bool {
		return svc.OpenPullRequest(context.Background(), "a1") == nil
	}, 5*time.Second, 20*time.Millisecond, "the claim is released once the first run finishes")

	time.Sleep(500 * time.Millisecond)
}

// TestOpenPullRequestReleasesTheClaimWhenItNeverStarts covers the path where Run refuses
// synchronously and its Done callback never fires at all, which is the one place a claim
// could be stranded forever with nothing left to release it.
func TestOpenPullRequestReleasesTheClaimWhenItNeverStarts(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, openPRScript), []byte("#!/bin/sh\n"), 0o644))

	svc := scriptService(t, dir, scripts.NewRunner())

	require.ErrorIs(t, svc.OpenPullRequest(context.Background(), "a1"), scripts.ErrNotExecutable)

	err := svc.OpenPullRequest(context.Background(), "a1")
	require.ErrorIs(t, err, scripts.ErrNotExecutable)
	assert.NotErrorIs(t, err, ErrScriptAlreadyRunning, "a refusal that never started must not strand the claim")
}

// TestConcurrencyGuardIsScopedNarrowly covers what the guard must not catch: the
// same script against a different agent, a hook, and a provisioner, all of which are
// serialized by the thing that asked for them and must never be refused by this guard.
func TestConcurrencyGuardIsScopedNarrowly(t *testing.T) {
	tests := []struct {
		name string
		fire func(svc *ScriptService) error
	}{
		{
			name: "the same script for a different agent runs alongside it",
			fire: func(svc *ScriptService) error {
				return svc.OpenPullRequest(context.Background(), "a2")
			},
		},
		{
			name: "a hook is never guarded by the in-flight set",
			fire: func(svc *ScriptService) error {
				return svc.RunHook(context.Background(), agentstore.Agent{ID: "a1", Cwd: t.TempDir()}, openPRScript, workflowstore.HookPreLaunch)
			},
		},
		{
			name: "a provisioner is never guarded by the in-flight set",
			fire: func(svc *ScriptService) error {
				_, err := svc.RunProvision(context.Background(), agentstore.Agent{ID: "a1"}, openPRScript, "")

				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, openPRScript), []byte("#!/bin/sh\nsleep 0.3\n"), 0o755))

			svc := scriptService(t, dir, scripts.NewRunner())
			svc.agents = fakeAgentReaderFunc(func(_ context.Context, id string) (*agentstore.Agent, error) {
				return &agentstore.Agent{ID: id}, nil
			})

			require.NoError(t, svc.OpenPullRequest(context.Background(), "a1"))

			assert.NoError(t, tt.fire(svc))

			time.Sleep(500 * time.Millisecond)
		})
	}
}

type fakeAgentReaderFunc func(ctx context.Context, id string) (*agentstore.Agent, error)

func (f fakeAgentReaderFunc) GetByID(ctx context.Context, id string) (*agentstore.Agent, error) {
	return f(ctx, id)
}

func scriptService(t *testing.T, dir string, runner scriptRunner) *ScriptService {
	t.Helper()

	return scriptServiceWithLogsDir(t, dir, runner, "")
}

func scriptServiceWithLogsDir(t *testing.T, dir string, runner scriptRunner, logsDir string) *ScriptService {
	t.Helper()

	tag := "✓"

	return NewScriptService(ScriptParams{
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
			cfg.Hooks.Dir = dir

			return cfg, nil
		},
		BuiltinDir: dir,
		Addr:       "127.0.0.1:8787",
		Socket:     "weave",
		Editor:     "nvim",
		BinPath:    "/opt/weave/bin/weave",
		LogsDir:    logsDir,
	})
}

type fakeAgentReader struct {
	agent *agentstore.Agent
}

func (f fakeAgentReader) GetByID(_ context.Context, id string) (*agentstore.Agent, error) {
	if f.agent != nil && f.agent.ID == id {
		return f.agent, nil
	}

	return nil, fmt.Errorf("%w: agent %q", store.ErrNotFound, id)
}

type fakeWorkflowReader struct {
	workflow *workflowstore.Workflow
	archived bool
}

func (f fakeWorkflowReader) Get(ctx context.Context, id string) (*workflowstore.Workflow, error) {
	if f.archived {
		return nil, fmt.Errorf("%w: workflow %q", store.ErrNotFound, id)
	}

	return f.GetIncludingArchived(ctx, id)
}

func (f fakeWorkflowReader) GetIncludingArchived(_ context.Context, id string) (*workflowstore.Workflow, error) {
	if f.workflow != nil && f.workflow.ID == id {
		return f.workflow, nil
	}

	return nil, fmt.Errorf("%w: workflow %q", store.ErrNotFound, id)
}

type fakePaneOwner struct {
	owner      string
	globals    map[string]string
	hostSocket string
	shell      string
	infoPane   string
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
func (r *recordingRunner) Run(p scripts.RunParams) error {
	if !p.Script.Executable {
		return scripts.ErrNotExecutable
	}

	r.ran = append(r.ran, p.Script.Name)
	r.dir, r.env, r.timeout = p.Dir, p.Env, p.Timeout

	if p.Done != nil {
		answer := r.results[p.Script.Name]
		p.Done(scripts.Result{Stderr: []byte(answer.stderr), ExitCode: answer.exitCode}, answer.err)
	}

	return nil
}

func (r *recordingRunner) RunAndWait(_ context.Context, p scripts.RunParams) (scripts.Result, error) {
	if !p.Script.Executable {
		return scripts.Result{}, scripts.ErrNotExecutable
	}

	r.ran = append(r.ran, p.Script.Name)
	r.dir, r.env, r.timeout = p.Dir, p.Env, p.Timeout

	answer := r.results[p.Script.Name]

	if path, ok := lookupEnv(p.Env, setupDirFileVar); ok {
		if err := answer.writeDirFile(path); err != nil {
			return scripts.Result{}, err
		}
	}

	return scripts.Result{
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

// TestOpenPullRequestPrefersTheBuiltinOverAHookOfTheSameName pins where the scripts weave ships
// come from now that a person's own directory is only ever read for hooks: a file of theirs
// called _open_pr is a hook they may hang on a moment, not a replacement for weave's own.
func TestOpenPullRequestPrefersTheBuiltinOverAHookOfTheSameName(t *testing.T) {
	hooksDir, builtinDir := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(hooksDir, openPRScript), []byte("#!/bin/sh\n# theirs\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(builtinDir, openPRScript), []byte("#!/bin/sh\n# ours\n"), 0o755))

	runner := &recordingRunner{}
	service := scriptService(t, hooksDir, runner)
	service.builtinDir = builtinDir

	require.NoError(t, service.OpenPullRequest(context.Background(), "a1"))

	assert.Equal(t, builtinDir, runner.dir, "the one weave ships is the one that ran")
}

// TestWorktreeEnvPrefersTheWorkflowsOwnDirectory covers which parent a worktree is made in:
// the workflow's own when it names one, expanded from a home-relative path, and the configured
// directory every workflow shares when it does not.
func TestWorktreeEnvPrefersTheWorkflowsOwnDirectory(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	tests := []struct {
		name string
		dir  string
		want func(configured string) string
	}{
		{
			name: "blank falls back to the configured directory every workflow shares",
			want: func(configured string) string { return configured },
		},
		{
			name: "an absolute directory is where this workflow's worktrees go",
			dir:  "/repo/.worktrees",
			want: func(string) string { return "/repo/.worktrees" },
		},
		{
			name: "a home-relative directory is expanded, since the scripts get a path rather than a shell",
			dir:  "~/repo/.worktrees",
			want: func(string) string { return filepath.Join(home, "repo/.worktrees") },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builtinDir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(builtinDir, openPRScript), []byte("#!/bin/sh\n"), 0o755))
			configured := t.TempDir()

			runner := &recordingRunner{}
			service := NewScriptService(ScriptParams{
				Agents: fakeAgentReader{agent: &agentstore.Agent{
					ID: "a1", Cwd: "/worktrees/a1", WorkflowID: "w1",
				}},
				Workflows: fakeWorkflowReader{workflow: &workflowstore.Workflow{
					ID: "w1", Name: "backend", Cwd: "/repo",
					Worktree: workflowstore.WorktreeConfig{Enabled: true, Dir: tt.dir},
				}},
				Tmux:       fakePaneOwner{},
				Runner:     runner,
				BuiltinDir: builtinDir,
				Load: func() (*config.Config, error) {
					cfg := config.Defaults()
					cfg.Worktrees.Dir = configured

					return cfg, nil
				},
			})

			require.NoError(t, service.OpenPullRequest(context.Background(), "a1"))

			values := map[string]string{}
			for _, entry := range runner.env {
				key, value, found := strings.Cut(entry, "=")
				require.True(t, found)
				values[key] = value
			}

			assert.Equal(t, tt.want(configured), values["WEAVE_WORKTREES_DIR"])
		})
	}
}

// TestARefusedHookLeavesARunLogSayingWhy pins a hook with nowhere to run writing the log key
// something to open, since a refusal that never reaches the runner used to leave the panel
// nothing at all to show.
func TestARefusedHookLeavesARunLogSayingWhy(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "install-deps"), []byte("#!/bin/sh\nexit 0\n"), 0o755))

	logsDir := t.TempDir()
	svc := scriptServiceWithLogsDir(t, dir, scripts.NewRunner(), logsDir)

	err := svc.RunHook(t.Context(), agentstore.Agent{ID: "a1", Cwd: "/definitely/not/here"}, "install-deps", workflowstore.HookPostRestore)
	require.ErrorIs(t, err, ErrHookFailed)

	runs := config.ListRunLogs(logsDir, "a1")
	require.Len(t, runs, 1)
	assert.Equal(t, workflowstore.HookPostRestore, runs[0].Trigger)
	assert.Equal(t, "install-deps", runs[0].Script)

	logged, readErr := os.ReadFile(runs[0].Path)
	require.NoError(t, readErr)
	assert.Contains(t, string(logged), "/definitely/not/here")
}

// TestAHookThatCannotStartLeavesARunLogSayingWhy pins the other refusal, since a hook deleted
// out from under a workflow reads on screen exactly like one that ran and did nothing.
func TestAHookThatCannotStartLeavesARunLogSayingWhy(t *testing.T) {
	logsDir := t.TempDir()
	svc := scriptServiceWithLogsDir(t, t.TempDir(), scripts.NewRunner(), logsDir)

	err := svc.RunHook(t.Context(), agentstore.Agent{ID: "a1", Cwd: t.TempDir()}, "install-deps", workflowstore.HookPostLaunch)
	require.ErrorIs(t, err, ErrHookFailed)

	runs := config.ListRunLogs(logsDir, "a1")
	require.Len(t, runs, 1)

	logged, readErr := os.ReadFile(runs[0].Path)
	require.NoError(t, readErr)
	assert.Contains(t, string(logged), "install-deps")
}
