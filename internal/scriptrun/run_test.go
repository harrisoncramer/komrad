package scriptrun

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
	"weave/db/testmodels"
	"weave/internal/logging"
	"weave/internal/scripts"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/storetest"
	"weave/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListHooks pins that a file which cannot be run is still listed, wanting a chmod rather
// than never appearing.
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

// TestFiredScriptRefusals covers everything that can go wrong before the process starts.
func TestFiredScriptRefusals(t *testing.T) {
	tests := []struct {
		name        string
		mode        os.FileMode
		wantIs      error
		leaveUnmade bool
	}{
		{
			name:        "a build shipping no such script",
			leaveUnmade: true,
			wantIs:      scripts.ErrScriptNotFound,
		},
		{
			name:   "a script nobody made executable",
			mode:   0o644,
			wantIs: scripts.ErrNotExecutable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if !tt.leaveUnmade {
				require.NoError(t, os.WriteFile(filepath.Join(dir, firedScript), []byte("#!/bin/sh\n"), tt.mode))
			}

			runner := &recordingRunner{}
			err := fireScript(t.Context(), t, scriptService(t, dir, runner))

			require.ErrorIs(t, err, tt.wantIs)
			assert.Empty(t, runner.ran, "nothing started")
		})
	}
}

// TestScriptPayload covers what a script is handed, including the socket, without which keys
// sent to the pane would go to the user's own tmux server.
func TestScriptPayload(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, firedScript), []byte("#!/bin/sh\n"), 0o755))

	runner := &recordingRunner{}
	service := scriptService(t, dir, runner)
	seeded, err := service.agents.GetByID(t.Context(), fixtureAgentID)
	require.NoError(t, err)

	require.NoError(t, fireScript(t.Context(), t, service))

	require.Equal(t, []string{firedScript}, runner.ran)
	assert.Equal(t, dir, runner.dir, "a script starts among its own siblings, not in the agent")

	values := map[string]string{}
	for _, entry := range runner.env {
		key, value, found := strings.Cut(entry, "=")
		require.True(t, found, "every entry is a name and a value")
		values[key] = value
	}

	assert.Equal(t, fixtureAgentID, values["WEAVE_AGENT_ID"])
	assert.Equal(t, "/repo", values["WEAVE_AGENT_CWD"])
	assert.Equal(t, "127.0.0.1:8787", values["WEAVE_ADDR"])
	assert.Equal(t, "weave", values["WEAVE_TMUX_SOCKET"])
	assert.Equal(t, dir, values["WEAVE_SCRIPT_DIR"])
	assert.Equal(t, firedScript, values["WEAVE_SCRIPT"], "its own name, for a script that wants to tag its own log lines")
	assert.Equal(t, "%3", values["WEAVE_INFO_PANE"], "the pane the panel laid out for a script to fill")
	assert.Equal(t, "/tmp/host-socket", values["WEAVE_HOST_TMUX_SOCKET"])
	assert.Equal(t, "nvim", values["EDITOR"], "the configured editor, not whatever the service inherited")
	assert.Equal(t, "/bin/zsh", values["WEAVE_SHELL"], "the shell tmux itself opens a pane with")
	assert.True(t, strings.HasPrefix(values["PATH"], "/opt/weave/bin"+string(os.PathListSeparator)),
		"a script that shells out to weave finds this build's own binary first")

	var payload scriptPayload
	require.NoError(t, json.Unmarshal([]byte(values["WEAVE_AGENT"]), &payload))

	assert.Equal(t, "auth", payload.Name)
	assert.Equal(t, string(agentstore.StateWaiting), payload.State)
	assert.True(t, payload.Alive, "the pane still belongs to this agent")
	assert.Equal(t, "%9", payload.Pane)
	assert.Equal(t, "backend", payload.Workflow, "the workflow reads as its name, not the id a script cannot use")
	created, err := time.Parse(time.RFC3339, payload.CreatedAt)
	require.NoError(t, err, "the creation time is RFC 3339")
	assert.True(t, created.Equal(seeded.CreatedAt.Truncate(time.Second)), "and names the instant the row was written")
	assert.Equal(t, map[string]string{"pr": "https://example.test/1"}, payload.Metadata)
	assert.Equal(t, "fix auth", payload.Prompt)
	assert.False(t, payload.PromptTruncated, "a prompt that fits arrives whole and says so")
	assert.NotContains(t, values["WEAVE_AGENT"], "transcript", "a transcript is far past what an environment holds")
}

// TestWorktreeEnvOnlyWhenTheAgentIsProvisionedOne covers the environment the two built-in
// worktree scripts read their config from: present and reflecting the workflow's own settings for
// an agent the pair provisioned, and absent otherwise.
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
			require.NoError(t, os.WriteFile(filepath.Join(builtinDir, firedScript), []byte("#!/bin/sh\n"), 0o755))
			worktreesDir := t.TempDir()

			st := storetest.Open(t)
			seedFixture(t, st, agentstore.Agent{
				ID:               fixtureAgentID,
				Name:             "auth",
				Cwd:              "/worktrees/a1",
				ProvisionEnabled: tt.provision,
			}, workflowstore.Config{Name: "backend", Cwd: "/repo", Worktree: tt.worktree})

			runner := &recordingRunner{}
			service := NewScriptService(NewScriptServiceParams{
				Agents:     st.Agents,
				Workflows:  st.Workflows,
				Tmux:       fakePaneOwner{},
				Runner:     runner,
				BuiltinDir: builtinDir,
				Load: func() (*config.Config, error) {
					cfg := config.Defaults()
					cfg.Worktrees.Dir = worktreesDir

					return cfg, nil
				},
			})

			require.NoError(t, fireScript(t.Context(), t, service))

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

// TestRunScriptNamesAnArchivedWorkflow pins an archived workflow still being named.
func TestRunScriptNamesAnArchivedWorkflow(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, firedScript), []byte("#!/bin/sh\n"), 0o755))

	runner := &recordingRunner{}
	st := storetest.Open(t)
	agent := seedFixture(t, st, fixtureAgent(), workflowstore.Config{Name: "backend"})
	require.NoError(t, st.Workflows.Archive(t.Context(), agent.WorkflowID))
	service := scriptServiceOver(t, scriptServiceOverParams{Stores: st, Dir: dir, Runner: runner})

	require.NoError(t, fireScript(t.Context(), t, service))

	var payload scriptPayload
	for _, entry := range runner.env {
		if value, found := strings.CutPrefix(entry, "WEAVE_AGENT="); found {
			require.NoError(t, json.Unmarshal([]byte(value), &payload))
		}
	}

	assert.Equal(t, "backend", payload.Workflow)
}

// TestRunScriptBoundsALongPrompt pins a long prompt being cut, which the kernel limit on what an
// exec is handed would otherwise fail the run over.
func TestRunScriptBoundsALongPrompt(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, firedScript), []byte("#!/bin/sh\n"), 0o755))

	runner := &recordingRunner{}
	st := storetest.Open(t)
	seedFixture(t, st, agentstore.Agent{
		ID:     fixtureAgentID,
		Name:   "auth",
		Cwd:    "/repo",
		Prompt: strings.Repeat("prompt ", maxScriptPrompt),
	}, workflowstore.Config{Name: "backend"})
	service := scriptServiceOver(t, scriptServiceOverParams{Stores: st, Dir: dir, Runner: runner})

	require.NoError(t, fireScript(t.Context(), t, service))

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

// TestRunScriptOnADormantAgent pins a script being told the agent is not running.
func TestRunScriptOnADormantAgent(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, firedScript), []byte("#!/bin/sh\n"), 0o755))

	runner := &recordingRunner{}
	service := scriptService(t, dir, runner)
	service.tmux = fakePaneOwner{owner: "somebody-else"}

	require.NoError(t, fireScript(t.Context(), t, service))

	var payload scriptPayload
	for _, entry := range runner.env {
		if key, value, _ := strings.Cut(entry, "="); key == "WEAVE_AGENT" {
			require.NoError(t, json.Unmarshal([]byte(value), &payload))
		}
	}

	assert.False(t, payload.Alive)
}

// TestFiredScriptWithTheAgentDirectoryGone pins the agent's directory being a string in the
// payload, never somewhere the script has to be able to stand.
func TestFiredScriptWithTheAgentDirectoryGone(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, firedScript), []byte("#!/bin/sh\n"), 0o755))

	gone := filepath.Join(t.TempDir(), "removed-worktree")
	runner := &recordingRunner{}
	st := storetest.Open(t)
	seedFixture(t, st, agentstore.Agent{
		ID:   fixtureAgentID,
		Name: "auth",
		Cwd:  gone,
	}, workflowstore.Config{Name: "backend"})
	service := scriptServiceOver(t, scriptServiceOverParams{Stores: st, Dir: dir, Runner: runner})

	require.NoError(t, fireScript(t.Context(), t, service))

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

			got, err := service.RunProvision(t.Context(), agentstore.Agent{ID: fixtureAgentID, Name: "auth"}, tt.script)

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

	got, err := service.RunProvision(t.Context(), agentstore.Agent{ID: fixtureAgentID, Name: "auth"}, "greedy")

	runtime.ReadMemStats(&after)

	require.ErrorIs(t, err, ErrProvisionFailed)
	assert.Empty(t, got, "a refused setup names no directory")
	assert.Contains(t, err.Error(), setupDirFileVar)
	assert.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(4<<20),
		"the refusal read the whole dir file to arrive at it")
}

// TestRunProvisionTellsTheScriptWhatSetItOff covers WEAVE_TRIGGER.
func TestRunProvisionTellsTheScriptWhatSetItOff(t *testing.T) {
	tests := []struct {
		name        string
		fire        func(*ScriptService, *recordingRunner) error
		wantTrigger string
	}{
		{
			name: "fired as a workflow's setup",
			fire: func(s *ScriptService, _ *recordingRunner) error {
				_, err := s.RunProvision(t.Context(), agentstore.Agent{ID: fixtureAgentID}, "make-worktree")
				return err
			},
			wantTrigger: TriggerProvision,
		},
		{
			name: "fired as a workflow's setup on the way back up",
			fire: func(s *ScriptService, _ *recordingRunner) error {
				_, err := s.RunResume(t.Context(), agentstore.Agent{ID: fixtureAgentID}, "make-worktree")
				return err
			},
			wantTrigger: TriggerResume,
		},
		{
			name: "fired as a workflow's cleanup",
			fire: func(s *ScriptService, _ *recordingRunner) error {
				return s.RunDeprovision(t.Context(), agentstore.Agent{ID: fixtureAgentID}, "make-worktree", nil)
			},
			wantTrigger: TriggerDeprovision,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "make-worktree"), []byte("#!/bin/sh\n"), 0o755))

			runner := &recordingRunner{}
			require.NoError(t, tt.fire(scriptService(t, dir, runner), runner))

			assert.Contains(t, runner.env, "WEAVE_TRIGGER="+tt.wantTrigger)
		})
	}
}

// TestRunProvisionCarriesTheReason pins the script's stderr travelling with the refusal.
func TestRunProvisionCarriesTheReason(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "make-worktree"), []byte("#!/bin/sh\n"), 0o755))

	runner := &recordingRunner{results: map[string]runResult{
		"make-worktree": {stderr: "fatal: 'api-auth' is already checked out", err: scripts.ErrFailed},
	}}

	_, err := scriptService(t, dir, runner).RunProvision(t.Context(), agentstore.Agent{ID: fixtureAgentID}, "make-worktree")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "already checked out")
}

// TestRunDeprovisionRefusesOneThatIsGone pins a renamed script being reported rather than
// pretended over, leaving the caller to decide the agent is still removable.
func TestRunDeprovisionRefusesOneThatIsGone(t *testing.T) {
	err := scriptService(t, t.TempDir(), &recordingRunner{}).
		RunDeprovision(t.Context(), agentstore.Agent{ID: fixtureAgentID}, "was-renamed", nil)

	require.ErrorIs(t, err, scripts.ErrScriptNotFound)
}

// TestFiredScriptLogsWhatItDid covers the only account there is of a script nothing waits on.
func TestFiredScriptLogsWhatItDid(t *testing.T) {
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
				"weave.script.name":     firedScript,
				"weave.run.trigger":     TriggerDeprovision,
				"weave.script.agent.id": fixtureAgentID,
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
			require.NoError(t, os.WriteFile(filepath.Join(dir, firedScript), []byte("#!/bin/sh\n"), 0o755))

			runner := &recordingRunner{results: map[string]runResult{firedScript: tt.result}}
			ctx, records := loggedContext(t)

			require.NoError(t, fireScript(ctx, t, scriptService(t, dir, runner)))

			written := records()

			began := recordFor(t, written, "script started")
			assert.Equal(t, "INFO", began["severity_text"])
			assert.Equal(t, firedScript, began["weave.script.name"])

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
				"weave.script.agent.id": fixtureAgentID,
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
				RunProvision(ctx, agentstore.Agent{ID: fixtureAgentID, Cwd: "/repo"}, "make-worktree")
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
	require.ErrorIs(t, doneErr, scripts.ErrFailed)
}

// TestRunDeprovisionLogsWhatAZeroExitSaid pins stderr being kept from a zero exit.
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
	require.NoError(t, doneErr)
}

// TestRunDeprovisionPersistsStdoutOnAFailure pins stdout being captured too, for a script that
// fails through a plain echo and a bare exit.
func TestRunDeprovisionPersistsStdoutOnAFailure(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "teardown"), []byte(
		"#!/bin/sh\necho tearing down 1\necho tearing down 2\nexit 1\n",
	), 0o755))

	logsDir := t.TempDir()
	svc := scriptServiceWithLogsDir(t, dir, scripts.NewRunner(), logsDir)

	done := make(chan error, 1)
	require.NoError(t, svc.RunDeprovision(t.Context(), agentstore.Agent{ID: fixtureAgentID, Cwd: dir}, "teardown", func(err error) {
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

	_, err := svc.RunProvision(ctx, agentstore.Agent{ID: fixtureAgentID}, "setup")

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
			require.NoError(t, svc.RunDeprovision(t.Context(), agentstore.Agent{ID: fixtureAgentID, Cwd: dir}, "teardown", func(err error) {
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

// TestRunHookRunsInTheAgentsDirectory drives a real script through the runner, a hook being there
// to work on the checkout and not on the directory the script itself lives in.
func TestRunHookRunsInTheAgentsDirectory(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "install-deps"), []byte(
		"#!/bin/sh\nset -eu\necho installing in \"$PWD\"\npwd >installed\nprintf '%s' \"$WEAVE_TRIGGER\" >>installed\n",
	), 0o755))

	logsDir := t.TempDir()
	worktree := t.TempDir()
	svc := scriptServiceWithLogsDir(t, dir, scripts.NewRunner(), logsDir)

	err := svc.RunHook(t.Context(), agentstore.Agent{ID: fixtureAgentID, Cwd: worktree}, "install-deps", workflowstore.HookPreLaunch)
	require.NoError(t, err)

	landed, readErr := os.ReadFile(filepath.Join(worktree, "installed"))
	require.NoError(t, readErr, "the hook ran in the agent's own directory rather than the script's")
	assert.Contains(t, string(landed), workflowstore.HookPreLaunch, "and read the moment out of WEAVE_TRIGGER")

	runs := config.ListRunLogs(logsDir, fixtureAgentID)
	require.Len(t, runs, 1)
	assert.Equal(t, workflowstore.HookPreLaunch, runs[0].Trigger)
	assert.Equal(t, "install-deps", runs[0].Script)

	logged, logErr := os.ReadFile(runs[0].Path)
	require.NoError(t, logErr)
	assert.Contains(t, string(logged), "installing in", "and its output is kept under the run that made it")
}

// TestRunHookCarriesWhatTheScriptSaid pins the refusal a failing hook hands back.
func TestRunHookCarriesWhatTheScriptSaid(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "install-deps"), []byte(
		"#!/bin/sh\necho no lockfile here >&2\nexit 3\n",
	), 0o755))

	svc := scriptServiceWithLogsDir(t, dir, scripts.NewRunner(), t.TempDir())

	err := svc.RunHook(t.Context(), agentstore.Agent{ID: fixtureAgentID, Cwd: t.TempDir()}, "install-deps", workflowstore.HookPreLaunch)

	require.ErrorIs(t, err, ErrHookFailed)
	assert.Contains(t, err.Error(), "install-deps")
	assert.Contains(t, err.Error(), "no lockfile here")
}

// TestRunHookRefusesADirectoryThatIsGone pins a hook with nowhere to run failing rather than
// relocating.
func TestRunHookRefusesADirectoryThatIsGone(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "install-deps"), []byte("#!/bin/sh\nexit 0\n"), 0o755))

	svc := scriptServiceWithLogsDir(t, dir, scripts.NewRunner(), t.TempDir())

	err := svc.RunHook(t.Context(), agentstore.Agent{ID: fixtureAgentID, Cwd: "/definitely/not/here"}, "install-deps", workflowstore.HookPreLaunch)

	require.ErrorIs(t, err, ErrHookFailed)
	assert.Contains(t, err.Error(), "/definitely/not/here")
}

// TestChmodMakesAHookRunnable pins the one action the listing tells a person to take: a hook
// listed as unrunnable is refused, and a chmod is enough to make the next run go through
// without weave being restarted.
func TestChmodMakesAHookRunnable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "install-deps")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o644))

	svc := scriptServiceWithLogsDir(t, dir, scripts.NewRunner(), t.TempDir())

	listed, err := svc.ListHooks()
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.False(t, listed[0].Executable)

	err = svc.RunHook(t.Context(), agentstore.Agent{ID: fixtureAgentID, Cwd: t.TempDir()}, "install-deps", workflowstore.HookPreLaunch)
	require.ErrorIs(t, err, scripts.ErrNotExecutable)

	require.NoError(t, os.Chmod(path, 0o755))

	listed, err = svc.ListHooks()
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.True(t, listed[0].Executable)

	require.NoError(t, svc.RunHook(t.Context(), agentstore.Agent{ID: fixtureAgentID, Cwd: t.TempDir()}, "install-deps", workflowstore.HookPreLaunch))
}

// TestARefusedRunLeavesNoRunBehind pins a refusal spending no number.
func TestARefusedRunLeavesNoRunBehind(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, firedScript), []byte("#!/bin/sh\nexit 0\n"), 0o644))

	logsDir := t.TempDir()
	svc := scriptServiceWithLogsDir(t, dir, scripts.NewRunner(), logsDir)

	err := fireScript(t.Context(), t, svc)

	require.Error(t, err, "a file nobody made executable is not startable")
	assert.Empty(t, config.ListRunLogs(logsDir, fixtureAgentID), "and it leaves no run behind")
}

func scriptServiceRecording(t *testing.T, dir string, runner scriptRunner, logsDir string) (*ScriptService, *storetest.Stores) {
	t.Helper()

	st := storetest.Open(t)
	seedFixture(t, st, fixtureAgent(), workflowstore.Config{Name: "backend"})
	svc := scriptServiceOver(t, scriptServiceOverParams{
		Stores:  st,
		Dir:     dir,
		Runner:  runner,
		LogsDir: logsDir,
	})
	svc.runs = st.Runs

	return svc, st
}

func listRuns(t *testing.T, st *storetest.Stores) []*testmodels.ListAgentRunsRow {
	t.Helper()

	runs, err := testmodels.New(st.Pool.GetDB()).ListAgentRuns(t.Context(), fixtureAgentID)
	require.NoError(t, err)

	return runs
}

// TestRunHookRecordsTheRun covers the row a hook leaves behind.
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
			svc, st := scriptServiceRecording(t, dir, scripts.NewRunner(), logsDir)

			err := svc.RunHook(t.Context(), agentstore.Agent{ID: fixtureAgentID, Cwd: tt.cwd(t)}, "install-deps", workflowstore.HookPostLaunch)
			if tt.wantFailure == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}

			runs := listRuns(t, st)

			require.Len(t, runs, 1)
			assert.Equal(t, workflowstore.HookPostLaunch, runs[0].Trigger)
			assert.Equal(t, "install-deps", runs[0].Script)
			require.NotNil(t, runs[0].LogNumber, "the row points at the log the output went to")
			assert.Equal(t, int32(1), *runs[0].LogNumber)

			assert.Equal(t, tt.wantExitCode, runs[0].ExitCode)
			if tt.wantFailure == "" {
				assert.Empty(t, runs[0].Failure)

				return
			}
			assert.Contains(t, runs[0].Failure, "install-deps", "the row says which script it was")
			assert.Contains(t, runs[0].Failure, tt.wantFailure)
		})
	}
}

// TestAHookThatFailedSilentlySaysSoInItsLog covers a hook exiting non-zero having printed
// nothing, where the mark would otherwise point the person at a zero-byte pager.
func TestAHookThatFailedSilentlySaysSoInItsLog(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "install-deps"), []byte("#!/bin/sh\nexit 4\n"), 0o755))

	logsDir := t.TempDir()
	svc, st := scriptServiceRecording(t, dir, scripts.NewRunner(), logsDir)

	err := svc.RunHook(t.Context(), agentstore.Agent{ID: fixtureAgentID, Cwd: t.TempDir()}, "install-deps", workflowstore.HookPostLaunch)
	require.ErrorIs(t, err, ErrHookFailed)

	runs := listRuns(t, st)
	require.Len(t, runs, 1)
	require.NotNil(t, runs[0].ExitCode)
	assert.Equal(t, int32(4), *runs[0].ExitCode)

	logged, readErr := os.ReadFile(onlyRunLog(t, logsDir))
	require.NoError(t, readErr)
	assert.Contains(t, string(logged), "install-deps", "the run the mark points at says why rather than opening empty")
}

// TestARefusedRunDropsItsRow pins the row going with the empty log file.
func TestARefusedRunDropsItsRow(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, firedScript), []byte("#!/bin/sh\nexit 0\n"), 0o644))

	svc, st := scriptServiceRecording(t, dir, scripts.NewRunner(), t.TempDir())

	require.Error(t, fireScript(t.Context(), t, svc), "a file nobody made executable is not startable")

	assert.Empty(t, listRuns(t, st))
}

// TestARefusedProvisionerSaysSo covers a provisioner that never runs and so writes nothing:
// the row must not read as a clean exit and the number claimed for it must not open an empty
// pager.
func TestARefusedProvisionerSaysSo(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "setup"), []byte("#!/bin/sh\nexit 0\n"), 0o644))

	logsDir := t.TempDir()
	svc, st := scriptServiceRecording(t, dir, scripts.NewRunner(), logsDir)

	_, err := svc.RunProvision(t.Context(), agentstore.Agent{ID: fixtureAgentID}, "setup")

	require.ErrorIs(t, err, ErrProvisionFailed)

	runs := listRuns(t, st)
	require.Len(t, runs, 1)
	assert.Nil(t, runs[0].ExitCode, "a script nobody made executable never exited at all")
	assert.Contains(t, runs[0].Failure, "setup")

	logged, readErr := os.ReadFile(onlyRunLog(t, logsDir))
	require.NoError(t, readErr)
	assert.Contains(t, string(logged), "setup", "and the run the mark points at says why rather than opening empty")
}

// TestClaimRunLogNumbersEveryRun pins each run getting a file of its own.
func TestClaimRunLogNumbersEveryRun(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "setup"), []byte("#!/bin/sh\necho made it\n"), 0o755))

	logsDir := t.TempDir()
	svc := scriptServiceWithLogsDir(t, dir, scripts.NewRunner(), logsDir)

	for range 2 {
		_, err := svc.RunProvision(t.Context(), agentstore.Agent{ID: fixtureAgentID}, "setup")
		require.NoError(t, err)
	}

	runs := config.ListRunLogs(logsDir, fixtureAgentID)

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

// TestClaimRunLogPrunesTheOldestRuns pins the pruning.
func TestClaimRunLogPrunesTheOldestRuns(t *testing.T) {
	dir := t.TempDir()
	logsDir := t.TempDir()
	svc := scriptServiceWithLogsDir(t, dir, scripts.NewRunner(), logsDir)

	logDir := config.RunLogDir(logsDir, fixtureAgentID)
	require.NoError(t, os.MkdirAll(logDir, 0o755))
	for number := 1; number <= config.MaxRunLogs; number++ {
		name := config.RunLogName(number, config.TriggerDeprovision, firedScript)
		require.NoError(t, os.WriteFile(filepath.Join(logDir, name), []byte("out\n"), 0o644))
	}

	path, number := svc.claimRunLog(t.Context(), fixtureAgentID, config.TriggerDeprovision, firedScript)
	require.NotEmpty(t, path)
	assert.Equal(t, config.MaxRunLogs+1, number)

	runs := config.ListRunLogs(logsDir, fixtureAgentID)

	require.Len(t, runs, config.MaxRunLogs)
	assert.Equal(t, config.MaxRunLogs+1, runs[0].Number, "the newest run is the one just claimed")
	assert.Equal(t, 2, runs[len(runs)-1].Number, "the oldest went to make room for it")
}

// TestClaimRunLogStepsPastANameAlreadyTaken pins the exclusive create, which a manual script and
// a launch beginning together both go through.
func TestClaimRunLogStepsPastANameAlreadyTaken(t *testing.T) {
	logsDir := t.TempDir()
	svc := scriptServiceWithLogsDir(t, t.TempDir(), scripts.NewRunner(), logsDir)

	logDir := config.RunLogDir(logsDir, fixtureAgentID)
	require.NoError(t, os.MkdirAll(logDir, 0o755))
	taken := filepath.Join(logDir, config.RunLogName(1, config.TriggerDeprovision, firedScript))
	require.NoError(t, os.WriteFile(taken, []byte("someone else\n"), 0o644))

	path, number := svc.claimRunLog(t.Context(), fixtureAgentID, config.TriggerDeprovision, firedScript)

	assert.NotEqual(t, taken, path, "the name already there is left as it stands")
	assert.Equal(t, 2, number, "it steps past the number that was taken")
	content, err := os.ReadFile(taken)
	require.NoError(t, err)
	assert.Contains(t, string(content), "someone else")
}

const (
	// fixtureAgentID is the agent every script test in this file runs against.
	fixtureAgentID = "01990000-0000-7000-8000-0000000000a1"
	otherAgentID   = "01990000-0000-7000-8000-0000000000a2"
)

// firedScript is the built-in the tests use when they cover the machinery every script goes
// through rather than anything the script itself does.
const firedScript = "_worktree_cleanup"

// fireScript starts a script the way weave starts one it will not wait for.
func fireScript(ctx context.Context, t *testing.T, svc *ScriptService) error {
	t.Helper()

	return fireScriptFor(ctx, t, svc, fixtureAgentID)
}

func fireScriptFor(ctx context.Context, t *testing.T, svc *ScriptService, agentID string) error {
	t.Helper()

	agent, err := svc.agents.GetByID(ctx, agentID)
	require.NoError(t, err)

	return svc.RunDeprovision(ctx, *agent, firedScript, nil)
}

// onlyRunLog is the one run the fixture's agent has recorded.
func onlyRunLog(t *testing.T, logsDir string) string {
	t.Helper()

	runs := config.ListRunLogs(logsDir, fixtureAgentID)
	require.Len(t, runs, 1)

	return runs[0].Path
}

func loggedContext(t *testing.T) (context.Context, func() []map[string]any) {
	t.Helper()

	var written bytes.Buffer

	ctx := logging.WithLogger(t.Context(), logging.New(&written, slog.LevelDebug))

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
	agent := agentstore.Agent{ID: fixtureAgentID, Cwd: t.TempDir()}

	tests := []struct {
		name   string
		script string
		fire   func(*ScriptService, string) error
		want   time.Duration
	}{
		{
			name:   "a cleanup that declares nothing gets the default ceiling",
			script: "bare",
			fire: func(s *ScriptService, name string) error {
				return s.RunDeprovision(t.Context(), agent, name, nil)
			},
			want: 2 * time.Minute,
		},
		{
			name:   "a cleanup that declares no limit stays unbounded",
			script: "unlimited",
			fire: func(s *ScriptService, name string) error {
				return s.RunDeprovision(t.Context(), agent, name, nil)
			},
			want: 0,
		},
		{
			name:   "a provisioner that declares nothing keeps the ceiling a launch needs",
			script: "bare",
			fire: func(s *ScriptService, name string) error {
				_, err := s.RunProvision(t.Context(), agent, name)

				return err
			},
			want: provisionTimeout,
		},
		{
			name:   "a provisioner that declares one is held to what it declared",
			script: "declared",
			fire: func(s *ScriptService, name string) error {
				_, err := s.RunProvision(t.Context(), agent, name)

				return err
			},
			want: 30 * time.Second,
		},
		{
			name:   "a resume runs the same provisioner under the same declaration",
			script: "declared",
			fire: func(s *ScriptService, name string) error {
				_, err := s.RunResume(t.Context(), agent, name)

				return err
			},
			want: 30 * time.Second,
		},
		{
			name:   "a hook that declares one is bounded by it",
			script: "declared",
			fire: func(s *ScriptService, name string) error {
				return s.RunHook(t.Context(), agent, name, workflowstore.HookPreLaunch)
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
			require.NoError(t, os.WriteFile(filepath.Join(dir, firedScript), []byte("#!/bin/sh\n"), 0o755))

			runner := &recordingRunner{}
			require.NoError(t, tt.fire(scriptService(t, dir, runner), tt.script))

			require.Equal(t, []string{tt.script}, runner.ran)
			assert.Equal(t, tt.want, runner.timeout)
		})
	}
}

// TestFiredScriptRefusesASecondConcurrentRun covers the same script being asked for twice on
// one agent before the first run has finished.
func TestFiredScriptRefusesASecondConcurrentRun(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, firedScript), []byte("#!/bin/sh\nsleep 0.3\n"), 0o755))

	svc := scriptService(t, dir, scripts.NewRunner())

	require.NoError(t, fireScript(t.Context(), t, svc))

	err := fireScript(t.Context(), t, svc)
	require.ErrorIs(t, err, ErrScriptAlreadyRunning)

	require.Eventually(t, func() bool {
		return fireScript(t.Context(), t, svc) == nil
	}, 5*time.Second, 20*time.Millisecond, "the claim is released once the first run finishes")

	time.Sleep(500 * time.Millisecond)
}

// TestFiredScriptReleasesTheClaimWhenItNeverStarts covers the path where Run refuses synchronously
// and its Done callback never fires at all.
func TestFiredScriptReleasesTheClaimWhenItNeverStarts(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, firedScript), []byte("#!/bin/sh\n"), 0o644))

	svc := scriptService(t, dir, scripts.NewRunner())

	require.ErrorIs(t, fireScript(t.Context(), t, svc), scripts.ErrNotExecutable)

	err := fireScript(t.Context(), t, svc)
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
				return fireScriptFor(t.Context(), t, svc, otherAgentID)
			},
		},
		{
			name: "a hook is never guarded by the in-flight set",
			fire: func(svc *ScriptService) error {
				return svc.RunHook(t.Context(), agentstore.Agent{ID: fixtureAgentID, Cwd: t.TempDir()}, firedScript, workflowstore.HookPreLaunch)
			},
		},
		{
			name: "a provisioner is never guarded by the in-flight set",
			fire: func(svc *ScriptService) error {
				_, err := svc.RunProvision(t.Context(), agentstore.Agent{ID: fixtureAgentID}, firedScript)

				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, firedScript), []byte("#!/bin/sh\nsleep 0.3\n"), 0o755))

			st := storetest.Open(t)
			first := seedFixture(t, st, fixtureAgent(), workflowstore.Config{Name: "backend"})
			st.SeedAgent(t, agentstore.Agent{ID: otherAgentID, Name: "api", WorkflowID: first.WorkflowID})
			svc := scriptServiceOver(t, scriptServiceOverParams{Stores: st, Dir: dir, Runner: scripts.NewRunner()})

			require.NoError(t, fireScript(t.Context(), t, svc))

			require.NoError(t, tt.fire(svc))

			time.Sleep(500 * time.Millisecond)
		})
	}
}

func scriptService(t *testing.T, dir string, runner scriptRunner) *ScriptService {
	t.Helper()

	return scriptServiceWithLogsDir(t, dir, runner, "")
}

func scriptServiceWithLogsDir(t *testing.T, dir string, runner scriptRunner, logsDir string) *ScriptService {
	t.Helper()

	st := storetest.Open(t)
	seedFixture(t, st, fixtureAgent(), workflowstore.Config{Name: "backend"})

	return scriptServiceOver(t, scriptServiceOverParams{
		Stores:  st,
		Dir:     dir,
		Runner:  runner,
		LogsDir: logsDir,
	})
}

type scriptServiceOverParams struct {
	Stores  *storetest.Stores
	Dir     string
	Runner  scriptRunner
	LogsDir string
}

func scriptServiceOver(t *testing.T, params scriptServiceOverParams) *ScriptService {
	t.Helper()

	return NewScriptService(NewScriptServiceParams{
		Agents:    params.Stores.Agents,
		Workflows: params.Stores.Workflows,
		Tmux: fakePaneOwner{
			owner:      fixtureAgentID,
			hostSocket: "/tmp/host-socket",
			shell:      "/bin/zsh",
			infoPane:   "%3",
		},
		Runner: params.Runner,
		Load: func() (*config.Config, error) {
			cfg := config.Defaults()
			cfg.Hooks.Dir = params.Dir

			return cfg, nil
		},
		BuiltinDir: params.Dir,
		Addr:       "127.0.0.1:8787",
		Socket:     "weave",
		Editor:     "nvim",
		BinPath:    "/opt/weave/bin/weave",
		LogsDir:    params.LogsDir,
	})
}

func fixtureAgent() agentstore.Agent {
	return agentstore.Agent{
		ID:        fixtureAgentID,
		Name:      "auth",
		State:     agentstore.StateWaiting,
		Cwd:       "/repo",
		Harness:   "claude",
		SessionID: "sess-1",
		Flags:     "--model opus",
		Prompt:    "fix auth",
		Pane:      "%9",
	}
}

func seedFixture(t *testing.T, st *storetest.Stores, agent agentstore.Agent, workflow workflowstore.Config) *agentstore.Agent {
	t.Helper()

	agent.WorkflowID = st.SeedWorkflow(t, workflow).ID
	seeded := st.SeedAgent(t, agent)

	_, err := st.Agents.SetMetadata(t.Context(), agentstore.SetMetadataParams{
		ID:    seeded.ID,
		Key:   "pr",
		Value: xptr.Ptr("https://example.test/1"),
	})
	require.NoError(t, err)

	return seeded
}

type fakePaneOwner struct {
	owner      string
	hostSocket string
	shell      string
	infoPane   string
}

func (f fakePaneOwner) GetPaneOwner(string) string { return f.owner }
func (f fakePaneOwner) GetHostSocket() string      { return f.hostSocket }
func (f fakePaneOwner) GetInfoPane() string        { return f.infoPane }
func (f fakePaneOwner) GetDefaultShell() string    { return f.shell }

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

// TestFiredScriptPrefersTheBuiltinOverAHookOfTheSameName pins where the scripts weave ships
// come from now that a person's own directory is only ever read for hooks: a file of theirs
// called _worktree_cleanup is a hook they may hang on a moment, not a replacement for weave's own.
func TestFiredScriptPrefersTheBuiltinOverAHookOfTheSameName(t *testing.T) {
	hooksDir, builtinDir := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(hooksDir, firedScript), []byte("#!/bin/sh\n# theirs\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(builtinDir, firedScript), []byte("#!/bin/sh\n# ours\n"), 0o755))

	runner := &recordingRunner{}
	service := scriptService(t, hooksDir, runner)
	service.builtinDir = builtinDir

	require.NoError(t, fireScript(t.Context(), t, service))

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
			require.NoError(t, os.WriteFile(filepath.Join(builtinDir, firedScript), []byte("#!/bin/sh\n"), 0o755))
			configured := t.TempDir()

			st := storetest.Open(t)
			seedFixture(t, st, agentstore.Agent{
				ID:   fixtureAgentID,
				Name: "auth",
				Cwd:  "/worktrees/a1",
			}, workflowstore.Config{
				Name:     "backend",
				Cwd:      "/repo",
				Worktree: workflowstore.WorktreeConfig{Enabled: true, Dir: tt.dir},
			})

			runner := &recordingRunner{}
			service := NewScriptService(NewScriptServiceParams{
				Agents:     st.Agents,
				Workflows:  st.Workflows,
				Tmux:       fakePaneOwner{},
				Runner:     runner,
				BuiltinDir: builtinDir,
				Load: func() (*config.Config, error) {
					cfg := config.Defaults()
					cfg.Worktrees.Dir = configured

					return cfg, nil
				},
			})

			require.NoError(t, fireScript(t.Context(), t, service))

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
// something to open.
func TestARefusedHookLeavesARunLogSayingWhy(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "install-deps"), []byte("#!/bin/sh\nexit 0\n"), 0o755))

	logsDir := t.TempDir()
	svc := scriptServiceWithLogsDir(t, dir, scripts.NewRunner(), logsDir)

	err := svc.RunHook(t.Context(), agentstore.Agent{ID: fixtureAgentID, Cwd: "/definitely/not/here"}, "install-deps", workflowstore.HookPostRestore)
	require.ErrorIs(t, err, ErrHookFailed)

	runs := config.ListRunLogs(logsDir, fixtureAgentID)
	require.Len(t, runs, 1)
	assert.Equal(t, workflowstore.HookPostRestore, runs[0].Trigger)
	assert.Equal(t, "install-deps", runs[0].Script)

	logged, readErr := os.ReadFile(runs[0].Path)
	require.NoError(t, readErr)
	assert.Contains(t, string(logged), "/definitely/not/here")
}

// TestAHookThatCannotStartLeavesARunLogSayingWhy pins the other refusal, for a hook deleted out
// from under a workflow.
func TestAHookThatCannotStartLeavesARunLogSayingWhy(t *testing.T) {
	logsDir := t.TempDir()
	svc := scriptServiceWithLogsDir(t, t.TempDir(), scripts.NewRunner(), logsDir)

	err := svc.RunHook(t.Context(), agentstore.Agent{ID: fixtureAgentID, Cwd: t.TempDir()}, "install-deps", workflowstore.HookPostLaunch)
	require.ErrorIs(t, err, ErrHookFailed)

	runs := config.ListRunLogs(logsDir, fixtureAgentID)
	require.Len(t, runs, 1)

	logged, readErr := os.ReadFile(runs[0].Path)
	require.NoError(t, readErr)
	assert.Contains(t, string(logged), "install-deps")
}

// TestSilentAsyncFailureSaysWhyInItsLog covers the trigger nothing waits on, where a script
// that exits non-zero having printed nothing raises the flag on the agent's row and the run
// that flag points at is the only place a person can read why.
func TestSilentAsyncFailureSaysWhyInItsLog(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "teardown"), []byte("#!/bin/sh\n"), 0o755))

	logsDir := t.TempDir()
	runner := &recordingRunner{results: map[string]runResult{
		"teardown": {exitCode: 7, err: scripts.ErrFailed},
	}}
	svc, st := scriptServiceRecording(t, dir, runner, logsDir)

	require.NoError(t, svc.RunDeprovision(t.Context(), agentstore.Agent{ID: fixtureAgentID}, "teardown", nil))

	runs := listRuns(t, st)
	require.Len(t, runs, 1)
	require.NotEmpty(t, runs[0].Failure, "the row is a failure, so it wears the flag")

	said, err := os.ReadFile(onlyRunLog(t, logsDir))
	require.NoError(t, err)
	require.NotEmpty(t, said, "the run the flag points at should say why rather than opening an empty pager")
	assert.Contains(t, string(said), "teardown", "and it names the script that failed")
	assert.Contains(t, string(said), "exited non-zero", "and says what became of it")
}
