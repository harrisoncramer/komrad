package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"weave/config"
	"weave/internal/server"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testApp is an app wired for the cookbook tests: HOME points at a fresh directory so
// config.Load resolves to the defaults, and the cache directory is fresh so InstallBuiltins
// always starts from nothing.
func testApp(t *testing.T, addr string) *app {
	t.Helper()

	t.Setenv("HOME", t.TempDir())

	return &app{env: &config.Environment{Addr: addr, CacheDir: t.TempDir()}}
}

// TestListCookbooksIncludesTheBuiltins covers the base case with no user directory
// configured at all: the three this build ships are still offered.
func TestListCookbooksIncludesTheBuiltins(t *testing.T) {
	state := testApp(t, "")

	found, err := listCookbooks(state)
	require.NoError(t, err)

	names := make([]string, 0, len(found))
	for _, cookbook := range found {
		names = append(names, cookbook.Name)
	}

	assert.Contains(t, names, "worktree-agent")
	assert.Contains(t, names, "triage-schedule")
	assert.Contains(t, names, "pr-reviewer")
}

// TestListCookbooksUserOverridesBuiltin covers a user cookbook of the same name as a
// built-in taking precedence, the override List and Find both promise.
func TestListCookbooksUserOverridesBuiltin(t *testing.T) {
	state := testApp(t, "")

	cfg, err := config.Load(state.configPaths)
	require.NoError(t, err)
	userDir, err := cfg.GetCookbooksDir()
	require.NoError(t, err)

	require.NoError(t, os.MkdirAll(filepath.Join(userDir, "pr-reviewer"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(userDir, "pr-reviewer", "cookbook.toml"),
		[]byte("description = \"mine\"\n"),
		0o644,
	))

	found, err := listCookbooks(state)
	require.NoError(t, err)

	for _, cookbook := range found {
		if cookbook.Name == "pr-reviewer" {
			assert.Equal(t, "mine", cookbook.Manifest.Description)

			return
		}
	}

	t.Fatal("pr-reviewer was not found")
}

// TestCookbookCommandsBrokenByAnUnrelatedMalformedCookbook documents a gap rather than a fix:
// one malformed cookbook.toml in the user's own directory breaks list and install for every
// cookbook, including the built-ins, since List reads the user directory before the built-in
// one and a parse error there is returned immediately rather than skipped. Neither call ever
// reaches the server, so no fake or real one is needed to see it happen.
func TestCookbookCommandsBrokenByAnUnrelatedMalformedCookbook(t *testing.T) {
	state := testApp(t, "")

	cfg, err := config.Load(state.configPaths)
	require.NoError(t, err)
	userDir, err := cfg.GetCookbooksDir()
	require.NoError(t, err)

	broken := filepath.Join(userDir, "broken")
	require.NoError(t, os.MkdirAll(broken, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(broken, "cookbook.toml"), []byte("not valid toml [[[\n"), 0o644))

	_, err = listCookbooks(state)
	require.Error(t, err, "BUG: an unrelated broken cookbook should not hide the built-ins from weave cookbook list")

	_, err = installCookbook(context.Background(), state, "worktree-agent", "")
	require.Error(t, err, "BUG: installing a built-in should not be broken by an unrelated cookbook sitting in the user's own directory")
}

// existingWorkflow is one row newCookbookRPCServer's list_workflows stands in for, the shape
// requireCookbookNotInstalled reads a name and id back out of.
type existingWorkflow struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// cookbookRPCServer stands in for the service's workflow and schedule endpoints, recording
// what create_workflow and create_schedule were called with and answering the way the real
// handlers do. existingWorkflows is read fresh on every list_workflows call, so a test can
// set it before calling installCookbook to simulate a cookbook already installed.
type cookbookRPCServer struct {
	workflowRequest   map[string]any
	scheduleRequest   map[string]any
	existingWorkflows []existingWorkflow
}

func newCookbookRPCServer(t *testing.T) (*httptest.Server, *cookbookRPCServer) {
	t.Helper()

	recorder := &cookbookRPCServer{}

	listMethod, err := findRPCMethod("list_workflows")
	require.NoError(t, err)
	workflowMethod, err := findRPCMethod("create_workflow")
	require.NoError(t, err)
	scheduleMethod, err := findRPCMethod("create_schedule")
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.HandleFunc(server.HealthPath, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc(listMethod.GetPath(), func(w http.ResponseWriter, _ *http.Request) {
		payload, err := json.Marshal(map[string]any{"workflows": recorder.existingWorkflows})
		require.NoError(t, err)
		_, _ = w.Write(payload)
	})
	mux.HandleFunc(workflowMethod.GetPath(), func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&recorder.workflowRequest))
		_, _ = fmt.Fprint(w, `{"workflow":{"id":"wf_test","name":"whatever"}}`)
	})
	mux.HandleFunc(scheduleMethod.GetPath(), func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&recorder.scheduleRequest))
		_, _ = fmt.Fprint(w, `{"schedule":{"id":"sch_test","name":"whatever"}}`)
	})

	testServer := httptest.NewServer(mux)
	t.Cleanup(testServer.Close)

	return testServer, recorder
}

// TestInstallCookbookCreatesTheWorkflowAndItsActions covers a cookbook with no schedule: its
// actions land in the actions directory and its workflow is created over the same endpoint
// create_workflow already answers, with the manifest's own fields in the request.
func TestInstallCookbookCreatesTheWorkflowAndItsActions(t *testing.T) {
	testServer, recorder := newCookbookRPCServer(t)
	state := testApp(t, strippedHost(t, testServer.URL))

	result, err := installCookbook(context.Background(), state, "worktree-agent", "")
	require.NoError(t, err)

	assert.Equal(t, "wf_test", result.workflowID)
	assert.ElementsMatch(t, []string{"worktree-agent-setup", "worktree-agent-cleanup"}, result.actions)
	assert.Empty(t, result.scheduleName, "this cookbook arms no schedule")

	assert.Equal(t, "worktree-agent", recorder.workflowRequest["name"])
	assert.Equal(t, "worktree-agent-setup", recorder.workflowRequest["setupAction"])
	assert.Equal(t, "worktree-agent-cleanup", recorder.workflowRequest["cleanupAction"])

	cfg, err := config.Load(state.configPaths)
	require.NoError(t, err)
	actionsDir, err := cfg.GetActionsDir()
	require.NoError(t, err)

	assert.FileExists(t, filepath.Join(actionsDir, "worktree-agent-setup"))
	assert.FileExists(t, filepath.Join(actionsDir, "worktree-agent-cleanup"))
}

// TestInstallCookbookArmsItsSchedule covers a cookbook that also declares a schedule: it is
// created after the workflow, pointed at the workflow that was just created.
func TestInstallCookbookArmsItsSchedule(t *testing.T) {
	testServer, recorder := newCookbookRPCServer(t)
	state := testApp(t, strippedHost(t, testServer.URL))

	result, err := installCookbook(context.Background(), state, "triage-schedule", "")
	require.NoError(t, err)

	assert.Equal(t, "triage-schedule", result.scheduleName)
	assert.Equal(t, "triage-schedule", recorder.scheduleRequest["name"])
	assert.Equal(t, "wf_test", recorder.scheduleRequest["workflowId"])
	assert.Equal(t, false, recorder.scheduleRequest["enabled"])
}

// TestInstallCookbookCwdOverride covers --cwd overriding the manifest's own, blank, working
// directory, which is how a cookbook that cannot know your repository path in advance is
// still made runnable from one command.
func TestInstallCookbookCwdOverride(t *testing.T) {
	testServer, recorder := newCookbookRPCServer(t)
	state := testApp(t, strippedHost(t, testServer.URL))

	_, err := installCookbook(context.Background(), state, "worktree-agent", "/repo")
	require.NoError(t, err)

	assert.Equal(t, "/repo", recorder.workflowRequest["cwd"])
}

// TestInstallCookbookRefusesWhenAlreadyInstalled covers a second install of the same
// cookbook: it has to be refused rather than creating a second, indistinguishable workflow,
// and nothing should be written or created on the way to that refusal.
func TestInstallCookbookRefusesWhenAlreadyInstalled(t *testing.T) {
	testServer, recorder := newCookbookRPCServer(t)
	recorder.existingWorkflows = []existingWorkflow{{ID: "wf_existing", Name: "worktree-agent"}}
	state := testApp(t, strippedHost(t, testServer.URL))

	_, err := installCookbook(context.Background(), state, "worktree-agent", "")

	require.ErrorIs(t, err, ErrAlreadyInstalled)
	assert.Contains(t, err.Error(), "wf_existing")
	assert.Nil(t, recorder.workflowRequest, "create_workflow must not be called once a match is found")

	cfg, err := config.Load(state.configPaths)
	require.NoError(t, err)
	actionsDir, err := cfg.GetActionsDir()
	require.NoError(t, err)
	assert.NoFileExists(t, filepath.Join(actionsDir, "worktree-agent-setup"), "its actions are not installed either")
}

// TestInstallCookbookRequiresServer covers the install being refused outright when nothing is
// serving, since the create_workflow call it ends with has nowhere to land.
func TestInstallCookbookRequiresServer(t *testing.T) {
	state := testApp(t, freeAddr(t))

	cmd := cookbookInstallCmd(state)
	cmd.SetArgs([]string{"worktree-agent"})
	cmd.SetContext(context.Background())

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "run weave start first")
}
