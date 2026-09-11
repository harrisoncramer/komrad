// Command paneldemo runs the panel against a fake service, so a screen can be looked at without
// a database, a server, or an agent.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"

	"weave/config"
	"weave/internal/ui"
	"weave/proto/gen/agent/v1/agentv1connect"
	"weave/proto/gen/hook/v1/hookv1connect"
	"weave/proto/gen/schedule/v1/schedulev1connect"
	"weave/proto/gen/workflow/v1/workflowv1connect"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	flag.Parse()

	// The panel's stderr is the terminal it draws in, so a record written there lands on top of
	// the screen.
	slog.SetDefault(slog.New(slog.DiscardHandler))

	held := newCast()

	server := httptest.NewServer(serviceFor(held))
	defer server.Close()

	stub := noPanes{}

	deps := ui.Deps{
		PopupWindow:   ui.NewPopupWindow(stub, "paneldemo"),
		Tmux:          stub,
		HarnessFrames: noFrames{},
		Agents:        agentv1connect.NewAgentServiceClient(server.Client(), server.URL),
		Workflows:     workflowv1connect.NewWorkflowServiceClient(server.Client(), server.URL),
		Schedules:     schedulev1connect.NewScheduleServiceClient(server.Client(), server.URL),
		Hooks:         hookv1connect.NewHookServiceClient(server.Client(), server.URL),
		Keys:          config.DefaultKeybindings(),
		Layout:        config.DefaultLayout(),
		Addr:          server.URL,
		Pane:          demoPane,
	}

	return ui.Run(deps)
}

// serviceFor mounts the fake services the panel reads from.
func serviceFor(held cast) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(agentv1connect.NewAgentServiceHandler(agentService{cast: held}))
	mux.Handle(workflowv1connect.NewWorkflowServiceHandler(workflowService{cast: held}))
	mux.Handle(schedulev1connect.NewScheduleServiceHandler(scheduleService{cast: held}))
	mux.Handle(hookv1connect.NewHookServiceHandler(hookService{}))

	return mux
}
