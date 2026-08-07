package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"weave/config"
	"weave/internal/weavetest"
	actionv1 "weave/proto/gen/action/v1"
	agentv1 "weave/proto/gen/agent/v1"
	schedulev1 "weave/proto/gen/schedule/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// weavetestApp is an app wired against a real, running weavetest instance rather than a
// stand-in server: its actions directory is the same one the instance's own ActionService
// reads from, so an action a cookbook installs is something the real server can see and run,
// not just a file this test happens to find.
func weavetestApp(t *testing.T, instance *weavetest.Instance, cookbooksDir string) *app {
	t.Helper()

	t.Setenv("HOME", t.TempDir())

	configPath := filepath.Join(t.TempDir(), "config.toml")
	settings := fmt.Sprintf("[actions]\ndir = %q\n\n[cookbooks]\ndir = %q\n", instance.GetActionsDir, cookbooksDir)
	require.NoError(t, os.WriteFile(configPath, []byte(settings), 0o644))

	return &app{
		env:         &config.Environment{Addr: strippedHost(t, instance.URL), CacheDir: t.TempDir()},
		configPaths: []string{configPath},
	}
}

// writeUserCookbook writes a cookbook.toml holding body under cookbooksDir/name, for the
// tests below that need a hand-authored cookbook rather than one of the built-ins.
func writeUserCookbook(t *testing.T, cookbooksDir, name, body string) string {
	t.Helper()

	dir := filepath.Join(cookbooksDir, name)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cookbook.toml"), []byte(body), 0o644))

	return dir
}

// TestCookbookInstallEndToEnd installs a built-in cookbook against a real server and reads
// the result back through the real workflow and action endpoints, rather than a stand-in, so
// what is asserted on is what the panel and an agent would actually see.
func TestCookbookInstallEndToEnd(t *testing.T) {
	instance := weavetest.Start(t, weavetest.Params{})
	ctx := t.Context()
	state := weavetestApp(t, instance, t.TempDir())

	result, err := installCookbook(ctx, state, "worktree-agent", "/repo")
	require.NoError(t, err)

	workflow, err := instance.Workflows.GetWorkflow(ctx, connect.NewRequest(&workflowv1.GetWorkflowRequest{Id: result.workflowID}))
	require.NoError(t, err)
	assert.Equal(t, "worktree-agent", workflow.Msg.GetWorkflow().GetName())
	assert.Equal(t, "/repo", workflow.Msg.GetWorkflow().GetCwd())
	assert.Equal(t, "worktree-agent-setup", workflow.Msg.GetWorkflow().GetSetupAction())
	assert.Equal(t, "worktree-agent-cleanup", workflow.Msg.GetWorkflow().GetCleanupAction())

	listed, err := instance.Actions.ListActions(ctx, connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)

	byName := make(map[string]*actionv1.Action, len(listed.Msg.GetActions()))
	for _, action := range listed.Msg.GetActions() {
		byName[action.GetName()] = action
	}

	for _, name := range []string{"worktree-agent-setup", "worktree-agent-cleanup"} {
		action, ok := byName[name]
		require.True(t, ok, "%s was not visible to the same ActionService a hand-written action goes through", name)
		assert.True(t, action.GetExecutable())
		assert.True(t, action.GetText())
		assert.Empty(t, action.GetHeaderProblems())
	}
}

// TestCookbookInstallArmsScheduleEndToEnd covers a cookbook that also arms a schedule: the row
// it leaves behind is read back through the real schedule endpoint.
func TestCookbookInstallArmsScheduleEndToEnd(t *testing.T) {
	instance := weavetest.Start(t, weavetest.Params{})
	ctx := t.Context()
	state := weavetestApp(t, instance, t.TempDir())

	result, err := installCookbook(ctx, state, "triage-schedule", "/repo")
	require.NoError(t, err)

	schedules, err := instance.Schedules.ListSchedules(ctx, connect.NewRequest(&schedulev1.ListSchedulesRequest{}))
	require.NoError(t, err)

	require.Len(t, schedules.Msg.GetSchedules(), 1)
	schedule := schedules.Msg.GetSchedules()[0]
	assert.Equal(t, "triage-schedule", schedule.GetName())
	assert.Equal(t, result.workflowID, schedule.GetWorkflowId())
	assert.False(t, schedule.GetEnabled(), "installs disarmed until someone opts in")
}

// TestCookbookInstallRefusesDuplicateAgainstRealServer covers the same refusal the fake-server
// tests in cookbook_test.go cover, this time against the real create_workflow and
// list_workflows endpoints and their own pagination, rather than a stand-in that answers
// whatever it is told to.
func TestCookbookInstallRefusesDuplicateAgainstRealServer(t *testing.T) {
	instance := weavetest.Start(t, weavetest.Params{})
	ctx := t.Context()
	state := weavetestApp(t, instance, t.TempDir())

	_, err := installCookbook(ctx, state, "worktree-agent", "/repo")
	require.NoError(t, err)

	_, err = installCookbook(ctx, state, "worktree-agent", "/repo")
	require.ErrorIs(t, err, ErrAlreadyInstalled)
}

// TestCookbookInstallAcceptsAMissingSetupAction documents a gap rather than a fix: a cookbook
// naming a setup or cleanup action with no matching sibling file installs without complaint.
// Nothing checks the reference against what InstallActions actually wrote, and the real
// server's own validation on setupAction only checks the string's shape (no slash, no leading
// dot), not whether it names a real, installed file. The workflow this leaves behind points at
// an action nobody installed, and the failure will not surface until somebody tries to launch
// an agent into it and setup fails to find the script.
func TestCookbookInstallAcceptsAMissingSetupAction(t *testing.T) {
	instance := weavetest.Start(t, weavetest.Params{})
	ctx := t.Context()

	cookbooksDir := t.TempDir()
	writeUserCookbook(t, cookbooksDir, "broken-setup",
		"description = \"references an action that was never written\"\n\n[workflow]\nsetup_action = \"nope\"\n")

	state := weavetestApp(t, instance, cookbooksDir)

	result, err := installCookbook(ctx, state, "broken-setup", "/repo")
	require.NoError(t, err, "BUG: this install should be refused, since setup_action names a file that does not exist")

	workflow, err := instance.Workflows.GetWorkflow(ctx, connect.NewRequest(&workflowv1.GetWorkflowRequest{Id: result.workflowID}))
	require.NoError(t, err)
	assert.Equal(t, "broken-setup-nope", workflow.Msg.GetWorkflow().GetSetupAction(), "the workflow points at this name regardless")

	listed, err := instance.Actions.ListActions(ctx, connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)
	for _, action := range listed.Msg.GetActions() {
		assert.NotEqual(t, "broken-setup-nope", action.GetName(), "BUG: nothing was ever installed under the name the workflow now names")
	}
}

// TestCookbookInstallShipsNonScriptFilesAsActions documents a gap rather than a fix: every
// file next to cookbook.toml is installed as an action, whether or not it is actually a
// script, since InstallActions no longer distinguishes them from an explicit list. A stray
// note dropped in a cookbook's own directory is chmod +x'd and offered to the panel exactly
// like a real action, and the failure only shows up once somebody actually runs it.
func TestCookbookInstallShipsNonScriptFilesAsActions(t *testing.T) {
	instance := weavetest.Start(t, weavetest.Params{})
	ctx := t.Context()

	cookbooksDir := t.TempDir()
	dir := writeUserCookbook(t, cookbooksDir, "with-notes", "description = \"has a stray note file\"\n")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("just some prose, not a script\n"), 0o644))

	state := weavetestApp(t, instance, cookbooksDir)

	result, err := installCookbook(ctx, state, "with-notes", t.TempDir())
	require.NoError(t, err)
	require.Contains(t, result.actions, "with-notes-notes.txt", "BUG: a plain-text note was installed as though it were an action")

	listed, err := instance.Actions.ListActions(ctx, connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)

	var note *actionv1.Action
	for _, action := range listed.Msg.GetActions() {
		if action.GetName() == "with-notes-notes.txt" {
			note = action
		}
	}
	require.NotNil(t, note)
	assert.True(t, note.GetExecutable(), "BUG: the executable bit was set on a file that is not a script")

	agent, err := instance.Agents.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
		Prompt:     "irrelevant to this test",
		WorkflowId: result.workflowID,
	}))
	require.NoError(t, err)

	_, err = instance.Actions.RunAction(ctx, connect.NewRequest(&actionv1.RunActionRequest{
		AgentId: agent.Msg.GetAgent().GetId(),
		Action:  "with-notes-notes.txt",
	}))
	require.Error(t, err, "the file has no shebang, so the OS refuses to exec it, exactly what a hand-written note would do too")
	assert.Contains(t, err.Error(), "could not be started")
}
