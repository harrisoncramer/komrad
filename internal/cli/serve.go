package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"claude-agents/internal/agents"
	"claude-agents/internal/interceptors"
	"claude-agents/internal/mcp"
	rpcagent "claude-agents/internal/rpc/agent"
	rpcworkflow "claude-agents/internal/rpc/workflow"
	"claude-agents/internal/store"
	"claude-agents/proto/gen/agent/v1/agentv1connect"
	"claude-agents/proto/gen/workflow/v1/workflowv1connect"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"
)

// reconcileInterval is how often serve re-observes every agent and writes its
// state. It is the only thing that moves an agent's state, so it stays brisk enough
// that starting or finishing work shows within a couple of seconds while the tmux
// captures stay cheap.
const reconcileInterval = 2 * time.Second

// transcriptSyncInterval is how often serve folds new transcript text into the
// search index. It runs far slower than the state reconcile. A stale search
// corpus is harmless where a stale state count is not, and each pass re-indexes
// potentially large logs, so it stays off the hot path.
const transcriptSyncInterval = 15 * time.Second

func serveCmd(state *app) *cobra.Command {
	var addr string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the agent store over ConnectRPC/HTTP",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := state.openStore(cmd.Context()); err != nil {
				return err
			}

			// Requests are checked against the constraints declared in the proto
			// before any handler sees them, so a malformed payload is rejected in
			// one place rather than at the top of each method.
			validation, err := interceptors.NewValidation()
			if err != nil {
				return err
			}

			mux := http.NewServeMux()
			path, handler := agentv1connect.NewAgentServiceHandler(
				rpcagent.NewService(state.agents, state.transcripts, state.agentService),
				connect.WithInterceptors(validation),
			)
			mux.Handle(path, handler)

			workflowPath, workflowHandler := workflowv1connect.NewWorkflowServiceHandler(
				rpcworkflow.NewService(state.workflows, state.workflowService),
				connect.WithInterceptors(validation),
			)
			mux.Handle(workflowPath, workflowHandler)

			// The MCP endpoint reads through its own read-only handle rather than
			// the store's, so nothing reachable from a model can write, whatever
			// the tools do.
			readOnly, err := store.OpenReadOnly(cmd.Context(), state.env.DatabaseURL)
			if err != nil {
				return err
			}
			defer readOnly.Close()

			// The RPC-backed tools call back into this same server, so they are
			// pointed at the address it is about to listen on.
			mcpHandler, err := mcp.NewHandler(mcp.NewHandlerParams{DB: readOnly, BaseURL: "http://" + addr})
			if err != nil {
				return err
			}

			mux.Handle("/mcp", mcpHandler)

			// The reconciler runs here rather than in the panel because serve is
			// the always-on process that owns the store. The counts must stay
			// correct even when no panel is open.
			go runReconcileLoop(cmd.Context(), agents.NewReconciler(state.tmux, state.agents), reconcileInterval)

			// The transcript syncer runs alongside the reconciler for the same
			// reason. Serve owns the store and is always on, so the search index
			// stays current whether or not a panel is open.
			go runTranscriptSyncLoop(cmd.Context(), agents.NewTranscriptSyncer(state.agents, state.transcripts, agents.DefaultTranscriptsDir()), transcriptSyncInterval)

			server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
			return server.ListenAndServe()
		},
	}

	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:8787", "listen address")

	return cmd
}

// runReconcileLoop reconciles every agent on a fixed interval until the context
// is cancelled. Errors are logged and the loop continues, since a single failed
// pass is corrected by the next one.
func runReconcileLoop(ctx context.Context, rec *agents.Reconciler, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := rec.Reconcile(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "reconcile: %v\n", err)
			}
		}
	}
}

// runTranscriptSyncLoop folds new transcript text into the search index on a
// fixed interval until the context is cancelled. Errors are logged and the loop
// continues, since a single failed pass is corrected by the next one.
func runTranscriptSyncLoop(ctx context.Context, syncer *agents.TranscriptSyncer, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := syncer.Sync(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "transcript sync: %v\n", err)
			}
		}
	}
}
