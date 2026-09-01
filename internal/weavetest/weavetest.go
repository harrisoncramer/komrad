// Package weavetest runs a whole weave in a test: its own database, its own tmux server, and a
// service over both, with the loops driven by hand rather than by the clock.
//
// The behaviour worth testing here is the wiring between config, the service, tmux and a
// spawned CLI, and that is exactly what a unit test with fakes on both sides cannot reach. An
// instance lets a test launch an agent for real, make its harness look busy, run one pass of
// the reconciler, and then ask what was written and what was typed into the pane.
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
	"weave/db"
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

const settleTimeout = 10 * time.Second

type Instance struct {
	t *testing.T

	Agents    agentv1connect.AgentServiceClient
	Workflows workflowv1connect.WorkflowServiceClient
	Schedules schedulev1connect.ScheduleServiceClient
	Actions   actionv1connect.ActionServiceClient
	Config    configv1connect.ConfigServiceClient

	Harness *harnesstest.Harness

	Tmux *tmuxtest.Server

	Stores *storetest.Stores

	GetActionsDir string

	URL string

	server  *server.Server
	working bool
}

type Params struct {
	MaxAgents int

	DeliveryDeadline string

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

	httpServer := httptest.NewUnstartedServer(nil)
	addr := httpServer.Listener.Addr().String()

	manager := tmux.NewClient(tmuxServer.Socket())

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

// WorkQueue starts the job runner and waits for the queue to drain, then leaves it running
// since a job may queue more work.
func (i *Instance) WorkQueue() {
	i.t.Helper()

	if !i.working {
		require.NoError(i.t, i.server.Jobs.Start(context.WithoutCancel(i.t.Context())))
		i.working = true
		i.t.Cleanup(func() { i.server.Stop(context.Background()) })
	}

	require.Eventually(i.t, func() bool {
		var unfinished int64
		err := i.Stores.Pool.GetRawPool().QueryRow(i.t.Context(),
			fmt.Sprintf("SELECT count(*) FROM %q.river_job WHERE finalized_at IS NULL", db.RiverSchema),
		).Scan(&unfinished)
		require.NoError(i.t, err)

		return unfinished == 0
	}, settleTimeout, 10*time.Millisecond, "the queue never drained")
}

func (i *Instance) runLoop(name string) {
	i.t.Helper()

	for _, loop := range i.server.Loops {
		if loop.Name == name {
			require.NoError(i.t, loop.Pass(i.t.Context()))

			return
		}
	}

	i.t.Fatalf("no loop named %q", name)
}

// Reconcile runs one pass of the loop that writes agent state.
func (i *Instance) Reconcile() {
	i.t.Helper()
	i.runLoop(server.ReconcileLoop)
}

// SyncTranscripts runs one pass of the loop that folds what was said into the search index.
func (i *Instance) SyncTranscripts() {
	i.t.Helper()
	i.runLoop(server.TranscriptSyncLoop)
}

// ReconcileSchedules runs one pass of the loop that makes River's registrations match the
// schedules table.
func (i *Instance) ReconcileSchedules() {
	i.t.Helper()
	i.runLoop(server.ScheduleReconcileLoop)
}

// Settle moves an agent to an activity and reconciles until the store agrees, so a test says
// where an agent ends up rather than how many passes that takes.
func (i *Instance) Settle(agentID string, activity harness.Activity, want string) {
	i.t.Helper()

	require.NoError(i.t, i.Harness.SetActivity(agentID, activity))
	i.WaitForActivity(agentID, activity)

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

// WaitForActivity waits until an agent's own pane says it is doing something, which anything
// reading a frame would otherwise race against the first render.
func (i *Instance) WaitForActivity(agentID string, activity harness.Activity) {
	i.t.Helper()

	client := i.Tmux.Client()

	deadline := time.Now().Add(settleTimeout)
	for time.Now().Before(deadline) {
		pane := i.GetOwnedPane(agentID)
		if pane != "" && i.Harness.ReadActivity(client.CapturePane(pane)) == activity {
			return
		}

		time.Sleep(50 * time.Millisecond)
	}

	i.t.Fatalf("agent %s never showed activity %v in its own pane", agentID, activity)
}

// Deliver runs one pass of the work the delivery queue does for an agent, which is what
// gets mail to somebody who has stopped and would otherwise never look.
func (i *Instance) Deliver(agentID string) agents.Delivery {
	i.t.Helper()

	delivery, err := i.server.Delivery.Deliver(i.t.Context(), agentID)
	require.NoError(i.t, err)

	return delivery
}

// EndTurn stands in for the stop hook an agent CLI runs as its turn ends, taking whatever
// mail is waiting as that agent.
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

// WaitForTyped waits for an agent to have received the given number of typed lines, since
// typing at a pane and the agent reading its stdin are two processes apart.
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

// GetOwnedPane is the pane stamped with an agent's id, which answers what is running rather
// than what the row claims.
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
