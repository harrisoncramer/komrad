// Package komradtest runs a whole komrad in a test: its own database, its own tmux server, and a
// service over both, with the loops driven by hand rather than by the clock.
//
// The behaviour worth testing here is the wiring between config, the service, tmux and a
// spawned CLI, and that is exactly what a unit test with fakes on both sides cannot reach. An
// instance lets a test launch an agent for real, make its harness look busy, run one pass of
// the reconciler, and then ask what was written and what was typed into the pane.
//
// Two things in here are about a pane outliving the test that opened it. A CLI coming up in one
// writes into its harness root for the first time whenever it gets there, which can be after the
// test has ended, so the root is a directory of komradtest's own rather than a t.TempDir, removed
// with tolerance for a write that lands mid-walk. And the temp directories are claimed before the
// tmux server, so the panes are dead before the tree beneath them goes. Both surface as
// "TempDir RemoveAll cleanup: directory not empty" against a test whose own assertions all passed.
package komradtest

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

	"komrad/config"
	"komrad/internal/agents"
	"komrad/internal/caller"
	"komrad/internal/doctor"
	"komrad/internal/forge"
	"komrad/internal/harness"
	"komrad/internal/harnesstest"
	"komrad/internal/jobs/internaljobs"
	"komrad/internal/logging"
	"komrad/internal/mail"
	"komrad/internal/server"
	agentstore "komrad/internal/store/agent"
	"komrad/internal/storetest"
	"komrad/internal/tmux"
	"komrad/internal/tmuxtest"
	"komrad/lib/xhttp"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	"komrad/proto/gen/config/v1/configv1connect"
	"komrad/proto/gen/doctor/v1/doctorv1connect"
	"komrad/proto/gen/forge/v1/forgev1connect"
	"komrad/proto/gen/hook/v1/hookv1connect"
	"komrad/proto/gen/log/v1/logv1connect"
	"komrad/proto/gen/pullrequest/v1/pullrequestv1connect"
	"komrad/proto/gen/schedule/v1/schedulev1connect"
	"komrad/proto/gen/terminal/v1/terminalv1connect"
	"komrad/proto/gen/watch/v1/watchv1connect"
	"komrad/proto/gen/workflow/v1/workflowv1connect"
	"komrad/proto/gen/worktree/v1/worktreev1connect"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
)

const settleTimeout = 10 * time.Second

type Instance struct {
	t            *testing.T
	Agents       agentv1connect.AgentServiceClient
	Workflows    workflowv1connect.WorkflowServiceClient
	Schedules    schedulev1connect.ScheduleServiceClient
	Hooks        hookv1connect.HookServiceClient
	Logs         logv1connect.LogServiceClient
	Doctor       doctorv1connect.DoctorServiceClient
	Forges       forgev1connect.ForgeServiceClient
	PullRequests pullrequestv1connect.PullRequestServiceClient
	Worktrees    worktreev1connect.WorktreeServiceClient
	Config       configv1connect.ConfigServiceClient
	Watches      watchv1connect.WatchServiceClient
	Terminals    terminalv1connect.TerminalServiceClient
	Harness      *harnesstest.Harness
	Tmux         *tmuxtest.Server
	Stores       *storetest.Stores
	// HooksDir is where a test writes the hooks a workflow can hang on a moment.
	HooksDir string
	// BuiltinDir is where the server installed the scripts komrad ships, which a test overwrites
	// when it needs one of them to behave differently.
	BuiltinDir string
	// WorktreesDir is the directory the instance makes its agents' worktrees in.
	WorktreesDir string
	URL          string
	server       *server.Server
	working      bool
}

type Params struct {
	MaxAgents        int
	DeliveryDeadline string
	// WorktreesDir replaces the temporary directory the instance would otherwise make its
	// worktrees in.
	WorktreesDir string
	// KeepLog writes the service's log to the file it would write on a real machine, for a
	// test that reads it back.
	KeepLog bool
	// ForgeBuilders replaces the forges the instance speaks to.
	ForgeBuilders map[string]forge.Builder
}

// Start builds the service and returns it running, torn down when the test ends.
func Start(t *testing.T, p Params) *Instance {
	t.Helper()

	if p.MaxAgents == 0 {
		p.MaxAgents = 50
	}

	// Claimed before the tmux server.
	root := t.TempDir()
	harnessDir := harnessRoot(t)

	tmuxServer := tmuxtest.New(t)
	tmuxServer.NewSession(agents.ManagerSession)

	fake, err := harnesstest.New(harnessDir)
	require.NoError(t, err)

	harnesses, err := harness.NewRegistry(fake)
	require.NoError(t, err)

	stores := storetest.Open(t)

	cacheDir := filepath.Join(root, "cache")
	hooksDir := filepath.Join(root, "hooks")
	require.NoError(t, os.MkdirAll(hooksDir, 0o755))

	worktreesDir := p.WorktreesDir
	if worktreesDir == "" {
		worktreesDir = filepath.Join(root, "worktrees")
	}
	require.NoError(t, os.MkdirAll(worktreesDir, 0o755))

	settings := fmt.Sprintf("[hooks]\ndir = %q\n\n[worktrees]\ndir = %q\n", hooksDir, worktreesDir)
	if p.DeliveryDeadline != "" {
		settings += fmt.Sprintf("\n[mail]\ndelivery_deadline = %q\n", p.DeliveryDeadline)
	}

	configPath := filepath.Join(root, "config.toml")
	require.NoError(t, os.WriteFile(configPath, []byte(settings), 0o600))

	httpServer := httptest.NewUnstartedServer(nil)
	addr := httpServer.Listener.Addr().String()

	manager := tmux.NewClient(tmuxServer.Socket())

	env := &config.Environment{
		DatabaseURL:         stores.URL,
		ReadOnlyDatabaseURL: stores.ReadOnlyURL,
		Addr:                addr,
		TmuxSocket:          tmuxServer.Socket(),
		CacheDir:            cacheDir,
		StateDir:            filepath.Join(root, "state"),
		Shell:               "sh",
	}

	logger := logging.New(io.Discard, slog.LevelError)
	if p.KeepLog {
		require.NoError(t, os.MkdirAll(cacheDir, 0o700))
		logFile, err := os.OpenFile(env.LogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		require.NoError(t, err)
		t.Cleanup(func() { _ = logFile.Close() })
		logger = logging.New(logFile, slog.LevelInfo)
	}

	describedHarnesses, err := doctor.DescribeHarnesses(harnesses)
	require.NoError(t, err)

	built, err := server.NewServer(t.Context(), server.NewServerParams{
		Env:                env,
		ConfigStack:        config.Stack{Paths: []string{configPath}},
		Logger:             logger,
		Addr:               addr,
		Tmux:               manager,
		Harnesses:          harnesses,
		DefaultHarness:     harnesstest.ID,
		Usage:              config.Usage{MaxAgents: p.MaxAgents},
		FallbackResumeRoot: filepath.Join(root, "restored"),
		ForgeBuilders:      p.ForgeBuilders,
		Doctor: doctor.Settings{
			TmuxSocket:          env.TmuxSocket,
			ConfigStack:         config.Stack{Paths: []string{configPath}},
			Harnesses:           describedHarnesses,
			DatabaseURL:         env.DatabaseURL,
			ReadOnlyDatabaseURL: env.ReadOnlyDatabaseURL,
			Addr:                addr,
		},
	})
	require.NoError(t, err)
	t.Cleanup(built.Close)

	httpServer.Config = &http.Server{
		Handler:           built.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		Protocols:         xhttp.NewServerProtocols(),
	}
	httpServer.Start()
	t.Cleanup(httpServer.Close)

	client, url := httpServer.Client(), httpServer.URL

	return &Instance{
		t:            t,
		Agents:       agentv1connect.NewAgentServiceClient(client, url),
		Workflows:    workflowv1connect.NewWorkflowServiceClient(client, url),
		Schedules:    schedulev1connect.NewScheduleServiceClient(client, url),
		Hooks:        hookv1connect.NewHookServiceClient(client, url),
		Logs:         logv1connect.NewLogServiceClient(client, url),
		Doctor:       doctorv1connect.NewDoctorServiceClient(client, url),
		Forges:       forgev1connect.NewForgeServiceClient(client, url),
		PullRequests: pullrequestv1connect.NewPullRequestServiceClient(client, url),
		Worktrees:    worktreev1connect.NewWorktreeServiceClient(client, url),
		Config:       configv1connect.NewConfigServiceClient(client, url),
		Watches:      watchv1connect.NewWatchServiceClient(client, url),
		Terminals:    terminalv1connect.NewTerminalServiceClient(xhttp.NewClientOver(xhttp.NewStreamTransport(), 0), url),
		Harness:      fake,
		Tmux:         tmuxServer,
		Stores:       stores,
		HooksDir:     hooksDir,
		BuiltinDir:   server.BuiltinScriptsDir(cacheDir),
		WorktreesDir: worktreesDir,
		URL:          url,
		server:       built,
	}
}

// removalAttempts and removalPause are how long a stray write is waited out.
const (
	removalAttempts = 20
	removalPause    = 50 * time.Millisecond
)

// harnessRoot is a directory for the fake harness, removed with tolerance rather than by the
// test framework.
func harnessRoot(t *testing.T) string {
	t.Helper()

	//nolint:usetesting // a t.TempDir here is removed strictly, so a straggling pane's write fails the test
	dir, err := os.MkdirTemp("", "komradtest-harness-")
	require.NoError(t, err)

	t.Cleanup(func() {
		var err error
		for range removalAttempts {
			if err = os.RemoveAll(dir); err == nil {
				return
			}
			time.Sleep(removalPause)
		}

		t.Logf("gave up removing the fake harness root %s: %v", dir, err)
	})

	return dir
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
			fmt.Sprintf("SELECT count(*) FROM %q.river_job WHERE finalized_at IS NULL", i.Stores.Pool.GetRiverSchema()),
		).Scan(&unfinished)
		require.NoError(i.t, err)

		return unfinished == 0
	}, settleTimeout, 10*time.Millisecond, "the queue never drained")
}

// Fire queues one firing of a schedule, due at scheduledFor, and works the queue until it drains.
func (i *Instance) Fire(scheduleID string, scheduledFor time.Time) {
	i.t.Helper()

	_, err := i.server.Jobs.Insert(i.t.Context(), internaljobs.LaunchAgentArgs{
		ScheduleID:   scheduleID,
		ScheduledFor: scheduledFor,
	}, nil)
	require.NoError(i.t, err)

	i.WorkQueue()
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

// SyncCwds runs one pass of the loop that reads each agent's checkout onto its row.
func (i *Instance) SyncCwds() {
	i.t.Helper()
	i.runLoop(server.CwdSyncLoop)
}

// SyncPullRequests runs one pass of the loop that reads the forges' pull requests.
func (i *Instance) SyncPullRequests() {
	i.t.Helper()
	i.runLoop(server.PullRequestSyncLoop)
}

// SyncTranscripts runs one pass of the loop that folds what was said into the search index.
func (i *Instance) SyncTranscripts() {
	i.t.Helper()
	i.runLoop(server.TranscriptSyncLoop)
}

// MeasureWorktrees runs one pass of the loop that walks worktree directories for their size.
func (i *Instance) MeasureWorktrees() {
	i.t.Helper()
	i.runLoop(server.WorktreeSizeLoop)
}

// Settle moves an agent to an activity and reconciles until the store agrees, so a test says
// where an agent ends up rather than how many passes that takes.
func (i *Instance) Settle(agentID string, activity harness.Activity, want agentstore.State) {
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

// WaitForHarness waits until an agent's harness has drawn in its own pane.
func (i *Instance) WaitForHarness(agentID string) {
	i.t.Helper()
	i.WaitForActivity(agentID, harness.ActivityIdle)
}

// WaitForActivity waits until an agent's own pane says it is doing something, which anything
// reading a frame would otherwise race against the first render.
func (i *Instance) WaitForActivity(agentID string, activity harness.Activity) {
	i.t.Helper()

	client := i.Tmux.Client()

	deadline := time.Now().Add(settleTimeout)
	for time.Now().Before(deadline) {
		pane := i.GetOwnedPane(agentID)
		if pane != "" && i.Harness.ReadInput(client.CapturePane(pane)).Activity == activity {
			return
		}

		time.Sleep(50 * time.Millisecond)
	}

	i.t.Fatalf("agent %s never showed activity %v in its own pane", agentID, activity)
}

// Deliver runs one pass of the work the delivery queue does for an agent.
func (i *Instance) Deliver(agentID string) mail.Delivery {
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

// Typed is every line komrad has typed into an agent's pane, read back from the agent
// itself, so a test asserts on what the agent received rather than on what was sent.
func (i *Instance) Typed(agentID string) []string {
	i.t.Helper()

	typed, err := i.Harness.Typed(agentID)
	require.NoError(i.t, err)

	return typed
}

// WaitForTyped waits for an agent to have received the given number of typed lines, typing at a
// pane and the agent reading its stdin being two processes apart.
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
