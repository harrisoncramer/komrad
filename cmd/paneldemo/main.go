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

	"weave/cmd/paneldemo/scenario"
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
	name := flag.String("scenario", scenario.Names()[0], "the data to draw the panel against")
	list := flag.Bool("list", false, "name the scenarios and stop")
	flag.Parse()

	if *list {
		for _, held := range scenario.All() {
			fmt.Printf("%-8s %s\n", held.Name, held.Blurb)
		}

		return nil
	}

	held, err := scenario.Named(*name)
	if err != nil {
		return err
	}

	// The panel's stderr is the terminal it draws in, so a record written there lands on top of
	// the screen.
	slog.SetDefault(slog.New(slog.DiscardHandler))

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

// serviceFor mounts every service the panel is a client of, so a screen reaching one of them
// finds a fake rather than nothing.
func serviceFor(held scenario.Scenario) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(agentv1connect.NewAgentServiceHandler(agentService{held: held}))
	mux.Handle(workflowv1connect.NewWorkflowServiceHandler(workflowService{held: held}))
	mux.Handle(schedulev1connect.NewScheduleServiceHandler(scheduleService{held: held}))
	mux.Handle(hookv1connect.NewHookServiceHandler(hookService{}))

	return mux
}
