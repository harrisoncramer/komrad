// Package weavetest stands the whole service up inside a test.
//
// Every other test in the tree picks a seam and stubs whatever is on the far side of it,
// which leaves the behaviour that spans seams uncovered: archiving an agent kills its
// pane, archiving a workflow takes its agents with it, a launch fires the workflow's setup
// action, a firing lands an agent in the right bucket. An Instance is the same wiring the
// served process is built from, over a database and a tmux server of this test's own, with
// the agent CLI replaced by one the test drives. Nothing else is faked.
//
// The background loops are not started. A test steps them itself, so a state change is
// something it asked for rather than something it waited out.
package weavetest

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"weave/config"
	"weave/internal/agents"
	"weave/internal/caller"
	"weave/internal/harness"
	"weave/internal/harnesstest"
	"weave/internal/logging"
	"weave/internal/server"
	"weave/internal/storetest"
	"weave/internal/tmux"
	"weave/internal/tmuxtest"
	"weave/proto/gen/action/v1/actionv1connect"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"
	"weave/proto/gen/config/v1/configv1connect"
	"weave/proto/gen/schedule/v1/schedulev1connect"
	"weave/proto/gen/workflow/v1/workflowv1connect"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
)

// settleTimeout bounds waiting for something the test asked for to show up in a pane or in
// the store. It is generous because it is only ever reached when something is wrong.
const settleTimeout = 10 * time.Second

// Instance is the running service and everything a test needs to drive it.
type Instance struct {
	t *testing.T

	// The typed clients, over the wire, so every call goes through the validation
	// interceptor and the guard the served process puts in front of its handlers.
	Agents    agentv1connect.AgentServiceClient
	Workflows workflowv1connect.WorkflowServiceClient
	Schedules schedulev1connect.ScheduleServiceClient
	Actions   actionv1connect.ActionServiceClient
	Config    configv1connect.ConfigServiceClient

	// Harness is the CLI the agents run, which the test moves between states.
	Harness *harnesstest.Harness
	// Tmux is the server the agent panes are opened on.
	Tmux *tmuxtest.Server
	// Stores read the same database the service writes, for asserting on what a call left
	// behind rather than on what it returned.
	Stores *storetest.Stores
	// GetActionsDir is where the actions the config points at live, for a test that writes
	// one.
	GetActionsDir string
	// URL is where the service is listening.
	URL string

	server *server.Server
}

// Params are the few things a test may want different. Everything with a zero value here
// takes a default that suits most of them.
type Params struct {
	// MaxAgents is how many live agents the panel holds at once, defaulting to enough
	// that no test hits the limit without meaning to.
	MaxAgents int
	// DeliveryDeadline is how long mail stays deliverable, as a Go duration. It
	// defaults to the config's own, and a test sets it to something already past to watch
	// mail expire without waiting.
	DeliveryDeadline string
	// WorktreesDir is where a generated worktree setup action puts its worktrees. It has
	// to be set by any test that lets one run, since the default is under the developer's
	// own home directory.
	WorktreesDir string
}

// Start builds the service and returns it running, torn down when the test ends.
func Start(t *testing.T, p Params) *Instance {
	t.Helper()

	if p.MaxAgents == 0 {
		p.MaxAgents = 50
	}

	tmuxServer := tmuxtest.New(t)
	tmuxServer.NewSession(agents.ManagerSession)

	fake, err := harnesstest.New(t.TempDir())
	require.NoError(t, err)

	harnesses, err := harness.NewRegistry(fake)
	require.NoError(t, err)

	stores := storetest.Open(t)

	root := t.TempDir()
	actionsDir := filepath.Join(root, "actions")
	require.NoError(t, os.MkdirAll(actionsDir, 0o755))

	settings := fmt.Sprintf("[actions]\ndir = %q\n", actionsDir)
	if p.DeliveryDeadline != "" {
		settings += fmt.Sprintf("\n[mail]\ndelivery_deadline = %q\n", p.DeliveryDeadline)
	}
	if p.WorktreesDir != "" {
		settings += fmt.Sprintf("\n[worktrees]\ndir = %q\n", p.WorktreesDir)
	}

	configPath := filepath.Join(root, "config.toml")
	require.NoError(t, os.WriteFile(configPath, []byte(settings), 0o600))

	// Unstarted, so the address the agents are told is the one the service ends up on
	// rather than a port guessed ahead of it.
	httpServer := httptest.NewUnstartedServer(nil)
	addr := httpServer.Listener.Addr().String()

	manager := tmux.NewClient(tmuxServer.Socket())

	// Nothing in a test reads the log, and a failing pass writes one every time it is
	// stepped, so it goes nowhere rather than into the test's output.
	logger := logging.New(io.Discard, slog.LevelError)

	built, err := server.New(t.Context(), server.Params{
		Env: &config.Environment{
			DatabaseURL:         stores.URL,
			ReadOnlyDatabaseURL: stores.ReadOnlyURL,
			Addr:                addr,
			TmuxSocket:          tmuxServer.Socket(),
			CacheDir:            filepath.Join(root, "cache"),
		},
		ConfigPaths:        []string{configPath},
		Logger:             logger,
		Addr:               addr,
		Tmux:               tmux.NewServers(manager, tmux.NewHost(manager, "")),
		Harnesses:          harnesses,
		DefaultHarness:     harnesstest.ID,
		MaxAgents:          p.MaxAgents,
		FallbackResumeRoot: filepath.Join(root, "restored"),
	})
	require.NoError(t, err)
	t.Cleanup(built.Close)

	httpServer.Config = &http.Server{Handler: built.Handler(), ReadHeaderTimeout: 5 * time.Second}
	httpServer.Start()
	t.Cleanup(httpServer.Close)

	client, url := httpServer.Client(), httpServer.URL

	return &Instance{
		t:             t,
		Agents:        agentv1connect.NewAgentServiceClient(client, url),
		Workflows:     workflowv1connect.NewWorkflowServiceClient(client, url),
		Schedules:     schedulev1connect.NewScheduleServiceClient(client, url),
		Actions:       actionv1connect.NewActionServiceClient(client, url),
		Config:        configv1connect.NewConfigServiceClient(client, url),
		Harness:       fake,
		Tmux:          tmuxServer,
		Stores:        stores,
		GetActionsDir: actionsDir,
		URL:           url,
		server:        built,
	}
}

// Reconcile runs one pass of the loop that writes agent state.
func (i *Instance) Reconcile() {
	i.t.Helper()
	require.NoError(i.t, i.server.Reconcile.Pass(i.t.Context()))
}

// SyncTranscripts runs one pass of the loop that folds what was said into the search index.
func (i *Instance) SyncTranscripts() {
	i.t.Helper()
	require.NoError(i.t, i.server.TranscriptSync.Pass(i.t.Context()))
}

// ReconcileSchedules runs one pass of the loop that makes River's registrations match the
// schedules table.
func (i *Instance) ReconcileSchedules() {
	i.t.Helper()
	require.NoError(i.t, i.server.ScheduleReconcile.Pass(i.t.Context()))
}

// Settle moves an agent to an activity and reconciles until the store agrees. The agent
// prints its state on a tick and the reconciler needs several passes to demote one, so a
// test says where an agent should end up rather than how many passes that takes.
func (i *Instance) Settle(agentID string, activity harness.Activity, want string) {
	i.t.Helper()

	require.NoError(i.t, i.Harness.SetActivity(agentID, activity))

	deadline := time.Now().Add(settleTimeout)
	for time.Now().Before(deadline) {
		i.Reconcile()

		stored, err := i.Stores.Agents.GetByID(context.Background(), agentID)
		require.NoError(i.t, err)
		require.NotNil(i.t, stored)

		if stored.State == want {
			return
		}

		time.Sleep(50 * time.Millisecond)
	}

	i.t.Fatalf("agent %s never reached %q", agentID, want)
}

// Deliver runs one pass of the work the delivery queue does for an agent, which is what
// gets mail to somebody who has stopped and would otherwise never look.
func (i *Instance) Deliver(agentID string) agents.Delivery {
	i.t.Helper()

	delivery, err := i.server.Delivery.Deliver(i.t.Context(), agentID)
	require.NoError(i.t, err)

	return delivery
}

// EndTurn stands in for the stop hook an agent CLI runs as its turn ends: it takes whatever
// mail is waiting, as that agent, and records it as something the agent was told. The real
// hook is a subcommand of this binary calling the same endpoint, so what is exercised here
// is the panel's half of it.
func (i *Instance) EndTurn(agentID string) []*agentv1.Mail {
	i.t.Helper()

	stored, err := i.Stores.Agents.GetByID(i.t.Context(), agentID)
	require.NoError(i.t, err)
	require.NotNil(i.t, stored)

	req := connect.NewRequest(&agentv1.ReadMailRequest{})
	caller.Caller{AgentID: agentID, SessionID: stored.SessionID}.SetHeader(req.Header())

	read, err := i.Agents.ReadMail(i.t.Context(), req)
	require.NoError(i.t, err)

	for _, item := range read.Msg.GetMail() {
		require.NoError(i.t, i.Harness.Say(stored.SessionID, "user", item.GetBody()))
	}

	return read.Msg.GetMail()
}

// Typed is every line weave has typed into an agent's pane, read back from the agent
// itself, so a test asserts on what the agent received rather than on what was sent.
func (i *Instance) Typed(agentID string) []string {
	i.t.Helper()

	typed, err := i.Harness.Typed(agentID)
	require.NoError(i.t, err)

	return typed
}

// WaitForTyped waits for an agent to have received the given number of typed lines. Typing
// at a pane and the agent reading its own stdin are two processes apart, so a test says what
// it is waiting for rather than sleeping.
func (i *Instance) WaitForTyped(agentID string, lines int) []string {
	i.t.Helper()

	deadline := time.Now().Add(settleTimeout)
	for time.Now().Before(deadline) {
		typed := i.Typed(agentID)
		if len(typed) >= lines {
			return typed
		}

		time.Sleep(50 * time.Millisecond)
	}

	i.t.Fatalf("agent %s was never typed at %d time(s)", agentID, lines)

	return nil
}

// GetOwnedPane is the pane an agent is running in, empty when none on the server is stamped
// with its id. It reads the stamp rather than the agent's recorded pane, so it answers
// what is running rather than what the row claims.
func (i *Instance) GetOwnedPane(agentID string) string {
	i.t.Helper()

	panes, err := i.Tmux.Client().GetPanes()
	require.NoError(i.t, err)

	for pane, status := range panes {
		if status.Owner == agentID {
			return pane
		}
	}

	return ""
}
